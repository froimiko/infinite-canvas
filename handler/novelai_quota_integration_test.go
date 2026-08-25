package handler

// NovelAI V5 配额守卫与生图链路的集成测试。
//
// 与 novelai_quota_test.go 的分工：那边测「判定逻辑」（纯函数 + 缓存），
// 这边测「接进真实出图链路后的行为」—— 批内复查是否真的中止、成功后是否真的递减、
// 以及非 V5 模型是否真的一次订阅查询都不发。
//
// ⚠️ 同样受包级 sync.Map 串味影响，渠道一律走 quotaTestChannel（唯一 Token）。

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/basketikun/infinite-canvas/model"
)

// fakeNovelAIImageZip 造一个「一张 PNG」的 zip，形状与 NovelAI 上游返回一致，
// 好让 extractNovelAIImageData 能正常解出图片。
func fakeNovelAIImageZip(t *testing.T) []byte {
	t.Helper()
	var pngBuf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	var zipBuf bytes.Buffer
	writer := zip.NewWriter(&zipBuf)
	file, err := writer.Create("image_0.png")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := file.Write(pngBuf.Bytes()); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return zipBuf.Bytes()
}

// novelAIStubServer 同时假扮「订阅接口」与「生图接口」。
//
// subscriptionBody 支持动态生成，好模拟「出图过程中配额被别人消耗掉」。
// 返回两个计数器：订阅查询次数、生图次数。
func novelAIStubServer(t *testing.T, subscriptionBody func() string) (*httptest.Server, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	imageZip := fakeNovelAIImageZip(t)
	var subCalls, imageCalls atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user/subscription":
			subCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(subscriptionBody()))
		case "/ai/generate-image":
			imageCalls.Add(1)
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(imageZip)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server, &subCalls, &imageCalls
}

// batchRequest 构造一个走「免费锁 + 批量拆单」路径的 OpenAI 请求。
func batchRequest(naiModel string, count int) openAIImageRequest {
	return openAIImageRequest{
		Prompt:         "1girl, solo",
		Size:           "1024x1024",
		N:              count,
		NovelAIEnabled: true,
		NovelAIModel:   naiModel,
	}
}

// TestBatchDoesNotQueryQuotaForNonV5 是接入层的「零查询」回归测试。
//
// novelai_quota_test.go 里已经验证过守卫函数本身会短路，这里再从**真实批量出图
// 链路**验证一遍：V4.5 出 4 张，订阅接口必须一次都没被碰过。
//
// 之所以要两层都测：守卫函数正确，但接入点若在调用前先自己查一次订阅
// （比如为了打日志），同样会把延迟加回来。
func TestBatchDoesNotQueryQuotaForNonV5(t *testing.T) {
	// 配额透支：一旦真去查了，V5 会被拦；V4.5 必须完全无视它。
	server, subCalls, imageCalls := novelAIStubServer(t, func() string { return opusBody(-2, true) })
	channel := quotaTestChannel(t, server.URL, guardLock())

	data, succeeded, err := requestNovelAISingleImageBatch(
		context.Background(), batchRequest("nai-diffusion-4-5-full", 4), 4, channel, "user-1", nil, nil,
	)
	if err != nil {
		t.Fatalf("V4.5 batch must not be blocked by the V5 quota guard: %v", err)
	}
	if succeeded != 4 || len(data) != 4 {
		t.Fatalf("succeeded=%d images=%d, want 4/4", succeeded, len(data))
	}
	if got := subCalls.Load(); got != 0 {
		t.Fatalf("V4.5 batch triggered %d subscription lookups, want 0", got)
	}
	if got := imageCalls.Load(); got != 4 {
		t.Fatalf("upstream image calls = %d, want 4", got)
	}
}

// TestBatchStopsMidwayWhenQuotaRunsOut 验证批量函数**内部**的逐张门禁。
//
// ⚠️ 本用例刻意直接调 requestNovelAISingleImageBatch，**绕过入口守卫** ——
// 因为按这组参数走真实入口（proxyNovelAIImageRequest）的话，入口会按整批
// 5 张 + 1 保留 = 6 份来算，手上只有 3 份，整批就被拦掉了，批内循环一次都进不去。
//
// 批内复查在生产中真正生效的场景是「入口按当时余量放行后，配额被**其他并发请求**
// 吃掉」（同一个 Token 是共享配额池）。那种时序不好稳定复现，所以这里退一步：
// 直接验证「批量函数在配额见底时会停在当前张，而不是把剩下的都白扣成 Anlas」。
//
// 期望：配额够 3 份、保留 1 张 → 只出 2 张，第 3 张被拦，返回部分成功。
func TestBatchStopsMidwayWhenQuotaRunsOut(t *testing.T) {
	// 够 3 份 = 「2 张 + 1 张保留」。+epsilon 避开浮点刀锋：
	// 第 2 张的判定恰好是「等于阈值」，靠这点余量才稳定放行。
	server, _, imageCalls := novelAIStubServer(t, func() string {
		return opusBody(quotaFor(3)+quotaEpsilon, false)
	})

	lock := guardLock()
	lock.V5QuotaCacheSeconds = 3600 // 锁住 TTL：余量变化只来自本地递减
	channel := quotaTestChannel(t, server.URL, lock)

	data, succeeded, err := requestNovelAISingleImageBatch(
		context.Background(), batchRequest("nai-diffusion-5-full", 5), 5, channel, "user-1", nil, nil,
	)
	// 部分成功不算整体失败：已出的图要能返回给用户。
	if err != nil {
		t.Fatalf("partial success must not be a hard error: %v", err)
	}
	if succeeded != 2 {
		t.Fatalf("succeeded = %d, want 2 (quota covers 2 images plus a 1-image reserve)", succeeded)
	}
	if len(data) != 2 {
		t.Fatalf("returned %d images, want 2", len(data))
	}
	// 最关键的一条：没被守卫拦住的话这里会是 5。
	if got := imageCalls.Load(); got != 2 {
		t.Fatalf("upstream image calls = %d, want 2 (guard must stop the batch mid-way)", got)
	}
}

// TestBatchConsumesQuotaOnlyOnSuccess 验证「失败的那张不扣配额」。
//
// 上游第 2 张返回 500：它不该消耗配额，因此后续张数仍可继续出。
func TestBatchConsumesQuotaOnlyOnSuccess(t *testing.T) {
	imageZip := fakeNovelAIImageZip(t)
	var imageCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user/subscription":
			w.Header().Set("Content-Type", "application/json")
			// 给足量，确保本用例只检验「失败张不扣配额」这一件事。
			_, _ = w.Write([]byte(opusBody(90, false)))
		case "/ai/generate-image":
			if imageCalls.Add(1) == 2 {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"boom"}`))
				return
			}
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(imageZip)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	lock := guardLock()
	lock.V5QuotaCacheSeconds = 3600
	channel := quotaTestChannel(t, server.URL, lock)

	data, succeeded, err := requestNovelAISingleImageBatch(
		context.Background(), batchRequest("nai-diffusion-5-full", 3), 3, channel, "user-1", nil, nil,
	)
	if err != nil {
		t.Fatalf("single-slot failure must not abort the batch: %v", err)
	}
	// 3 张里第 2 张失败：应出 2 张，且第 3 张仍被尝试（说明失败没白扣配额把后面卡死）。
	if succeeded != 2 || len(data) != 2 {
		t.Fatalf("succeeded=%d images=%d, want 2/2", succeeded, len(data))
	}
	if got := imageCalls.Load(); got != 3 {
		t.Fatalf("upstream image calls = %d, want 3 (all slots attempted)", got)
	}
}

// TestBatchBlockedUpFrontWhenAlreadyExhausted 覆盖「一开始就透支」：
// 一张都不该出。
func TestBatchBlockedUpFrontWhenAlreadyExhausted(t *testing.T) {
	server, _, imageCalls := novelAIStubServer(t, func() string { return opusBody(-2, true) })
	channel := quotaTestChannel(t, server.URL, guardLock())

	_, succeeded, err := requestNovelAISingleImageBatch(
		context.Background(), batchRequest("nai-diffusion-5-full", 3), 3, channel, "user-1", nil, nil,
	)
	if err == nil {
		t.Fatal("fully exhausted quota must fail the whole batch")
	}
	if succeeded != 0 {
		t.Fatalf("succeeded = %d, want 0", succeeded)
	}
	if got := imageCalls.Load(); got != 0 {
		t.Fatalf("upstream image calls = %d, want 0 (must not spend Anlas at all)", got)
	}
	if !strings.Contains(err.Error(), "耗尽") {
		t.Fatalf("error should explain exhaustion, got: %v", err)
	}
}

// TestSingleImagePathConsumesQuota 验证单张路径（非批量）也会递减配额。
//
// 漏掉这处递减的后果：用户一张一张点，每次都读到同一个缓存快照，
// 保留位在单张模式下完全失效。
func TestSingleImagePathConsumesQuota(t *testing.T) {
	// 够 3 份：单张 + 1 保留 = 2 份，出一张后剩 2 份，再出一张后剩 1 份 → 第三次应拒。
	server, _, _ := novelAIStubServer(t, func() string {
		return opusBody(quotaFor(3)+quotaEpsilon, false)
	})

	lock := guardLock()
	lock.V5QuotaCacheSeconds = 3600
	channel := quotaTestChannel(t, server.URL, lock)

	naiReq := mustConvertToNovelAIRequest(t, batchRequest("nai-diffusion-5-full", 1))

	// 第一张：够。
	if err := ensureNovelAIV5Quota(context.Background(), channel, naiReq.Model, naiReq.Parameters.Width, naiReq.Parameters.Height, 1); err != nil {
		t.Fatalf("first single image blocked: %v", err)
	}
	if _, err := requestNovelAIImageData(context.Background(), channel, naiReq); err != nil {
		t.Fatalf("first single image failed: %v", err)
	}

	// 第二张：剩 2 份，1 张 + 1 保留 = 2 份，仍够。
	if err := ensureNovelAIV5Quota(context.Background(), channel, naiReq.Model, naiReq.Parameters.Width, naiReq.Parameters.Height, 1); err != nil {
		t.Fatalf("second single image blocked: %v", err)
	}
	if _, err := requestNovelAIImageData(context.Background(), channel, naiReq); err != nil {
		t.Fatalf("second single image failed: %v", err)
	}

	// 第三张：只剩约 1 份，不够「1 张 + 1 保留」→ 必须拒。
	// 这一步能过说明单张路径确实在递减配额。
	if err := ensureNovelAIV5Quota(context.Background(), channel, naiReq.Model, naiReq.Parameters.Width, naiReq.Parameters.Height, 1); err == nil {
		t.Fatal("third single image must be blocked: the single-image path has to consume quota too")
	}
}

// TestGuardUsesRequestDimensionsNotDefaults 验证守卫吃的是真实出图尺寸。
//
// 大图一张扣多份配额，若接入点误传 0 或写死 1024×1024，大图就会被低估、
// 从而放行本该拦下的请求。
func TestGuardUsesRequestDimensionsNotDefaults(t *testing.T) {
	// 给 3 份的量。
	server, _, _ := novelAIStubServer(t, func() string {
		return opusBody(quotaFor(3)+quotaEpsilon, false)
	})
	channel := quotaTestChannel(t, server.URL, guardLock())

	// 1216×1216 ≈ 1.48MP → 2 份/张。1 张需 2 份 + 保留 2 份 = 4 份 > 3 份，应拒。
	large := mustConvertToNovelAIRequest(t, openAIImageRequest{
		Prompt:         "1girl",
		Size:           "1216x1216",
		N:              1,
		NovelAIEnabled: true,
		NovelAIModel:   "nai-diffusion-5-full",
	})
	if got := large.Parameters.Width * large.Parameters.Height; novelAIQuotaUnitsForPixels(got) != 2 {
		t.Fatalf("expected the large request to cost 2 quota units, pixels=%d units=%d", got, novelAIQuotaUnitsForPixels(got))
	}
	if err := ensureNovelAIV5Quota(context.Background(), channel, large.Model, large.Parameters.Width, large.Parameters.Height, 1); err == nil {
		t.Fatal("large image must be blocked: it consumes 2 quota units per image")
	}
}

// TestGuardAppliesToResolvedModelFromShorthand 确保「前端传简写」也能被正确门禁。
//
// 前端可能传 "v5" / "NAI Diffusion V5 Full" / "__cloud__::nai-diffusion-5-full"，
// 接入点用的是 convertToNovelAIRequest 之后的 Model，所以这些都应被守卫覆盖。
func TestGuardAppliesToResolvedModelFromShorthand(t *testing.T) {
	for _, input := range []string{"v5", "NAI Diffusion V5 Full", "__cloud__::nai-diffusion-5-curated"} {
		server, subCalls, imageCalls := novelAIStubServer(t, func() string { return opusBody(-2, true) })
		channel := quotaTestChannel(t, server.URL, guardLock())

		naiReq := mustConvertToNovelAIRequest(t, batchRequest(input, 1))
		err := ensureNovelAIV5Quota(context.Background(), channel, naiReq.Model, naiReq.Parameters.Width, naiReq.Parameters.Height, 1)
		if err == nil {
			t.Fatalf("model shorthand %q resolved to %q must be guarded", input, naiReq.Model)
		}
		if got := subCalls.Load(); got != 1 {
			t.Fatalf("model %q: subscription lookups = %d, want 1", input, got)
		}
		if got := imageCalls.Load(); got != 0 {
			t.Fatalf("model %q: upstream image calls = %d, want 0", input, got)
		}
	}
}

// TestBatchQuotaGuardReusesCacheAcrossSlots 确认批内复查不会每张都打一次订阅接口。
//
// 若每张都查，出 10 张就要额外 10 次上游往返 —— 既慢又可能触发限流。
func TestBatchQuotaGuardReusesCacheAcrossSlots(t *testing.T) {
	server, subCalls, imageCalls := novelAIStubServer(t, func() string { return opusBody(90, false) })

	lock := guardLock()
	lock.V5QuotaCacheSeconds = 3600
	channel := quotaTestChannel(t, server.URL, lock)

	_, succeeded, err := requestNovelAISingleImageBatch(
		context.Background(), batchRequest("nai-diffusion-5-full", 6), 6, channel, "user-1", nil, nil,
	)
	if err != nil {
		t.Fatalf("batch failed: %v", err)
	}
	if succeeded != 6 {
		t.Fatalf("succeeded = %d, want 6", succeeded)
	}
	if got := imageCalls.Load(); got != 6 {
		t.Fatalf("upstream image calls = %d, want 6", got)
	}
	// 6 张只查 1 次订阅（TTL 内复用）。
	if got := subCalls.Load(); got != 1 {
		t.Fatalf("subscription lookups = %d, want 1 (per-slot recheck must reuse the cache)", got)
	}
}

// TestQuotaGuardDisabledLeavesBatchUntouched 确认守卫没勾时行为与改动前完全一致。
// 这是「可配置回退」的保证：出问题时后台取消勾选即可恢复原状。
func TestQuotaGuardDisabledLeavesBatchUntouched(t *testing.T) {
	server, subCalls, imageCalls := novelAIStubServer(t, func() string { return opusBody(-2, true) })

	guardOff := guardLock()
	guardOff.V5QuotaGuardEnabled = false
	channel := quotaTestChannel(t, server.URL, guardOff)

	_, succeeded, err := requestNovelAISingleImageBatch(
		context.Background(), batchRequest("nai-diffusion-5-full", 3), 3, channel, "user-1", nil, nil,
	)
	if err != nil {
		t.Fatalf("guard disabled must not change behaviour: %v", err)
	}
	if succeeded != 3 {
		t.Fatalf("succeeded = %d, want 3", succeeded)
	}
	if got := subCalls.Load(); got != 0 {
		t.Fatalf("guard disabled triggered %d subscription lookups, want 0", got)
	}
	if got := imageCalls.Load(); got != 3 {
		t.Fatalf("upstream image calls = %d, want 3", got)
	}
}

// TestQuotaGuardFailOpenStillGenerates 验证 fail-open 下上游订阅挂了也能出图。
func TestQuotaGuardFailOpenStillGenerates(t *testing.T) {
	imageZip := fakeNovelAIImageZip(t)
	var imageCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user/subscription":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/ai/generate-image":
			imageCalls.Add(1)
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(imageZip)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	lock := guardLock()
	lock.V5QuotaAllowOnLookupFailure = true
	channel := quotaTestChannel(t, server.URL, lock)

	_, succeeded, err := requestNovelAISingleImageBatch(
		context.Background(), batchRequest("nai-diffusion-5-full", 2), 2, channel, "user-1", nil, nil,
	)
	if err != nil {
		t.Fatalf("fail-open must keep generating: %v", err)
	}
	if succeeded != 2 {
		t.Fatalf("succeeded = %d, want 2", succeeded)
	}
	if got := imageCalls.Load(); got != 2 {
		t.Fatalf("upstream image calls = %d, want 2", got)
	}
}

// TestQuotaGuardHonoursChannelIsolation 确认不同 Token 是独立的配额池。
//
// 一个账号透支不该影响另一个账号 —— 配额缓存 key 含 Token 哈希正是为此。
func TestQuotaGuardHonoursChannelIsolation(t *testing.T) {
	exhausted, _, _ := novelAIStubServer(t, func() string { return opusBody(-2, true) })
	healthy, _, _ := novelAIStubServer(t, func() string { return opusBody(90, false) })

	channelExhausted := quotaTestChannel(t, exhausted.URL, guardLock())
	channelHealthy := quotaTestChannel(t, healthy.URL, guardLock())

	if err := ensureNovelAIV5Quota(context.Background(), channelExhausted, "nai-diffusion-5-full", 1024, 1024, 1); err == nil {
		t.Fatal("exhausted channel must be blocked")
	}
	if err := ensureNovelAIV5Quota(context.Background(), channelHealthy, "nai-diffusion-5-full", 1024, 1024, 1); err != nil {
		t.Fatalf("healthy channel must be unaffected by another token's quota: %v", err)
	}
}

// TestQuotaSettingsSurviveJSONRoundTrip 确认新字段能正确读写配置。
//
// 老配置（JSON 里没有这些字段）必须零迁移可用：守卫默认关闭、参数走安全默认值。
func TestQuotaSettingsSurviveJSONRoundTrip(t *testing.T) {
	legacyJSON := `{"enabled":true,"maxPixels":1048576,"maxSteps":28,"forceCountOne":true,"disableImg2Img":true}`
	var legacy model.FreeGenerationLock
	if err := json.Unmarshal([]byte(legacyJSON), &legacy); err != nil {
		t.Fatalf("legacy config must still parse: %v", err)
	}
	if legacy.V5QuotaGuardEnabled {
		t.Fatal("legacy config must leave the guard disabled")
	}
	settings := novelAIQuotaSettingsFrom(&legacy)
	if settings.ReserveImages != defaultNovelAIV5QuotaReserveImages {
		t.Fatalf("legacy ReserveImages = %d, want default %d", settings.ReserveImages, defaultNovelAIV5QuotaReserveImages)
	}

	fullJSON := `{"enabled":true,"v5QuotaGuardEnabled":true,"v5QuotaReserveImages":3,"v5QuotaAllowOnLookupFailure":true,"v5QuotaCacheSeconds":45}`
	var full model.FreeGenerationLock
	if err := json.Unmarshal([]byte(fullJSON), &full); err != nil {
		t.Fatalf("new config must parse: %v", err)
	}
	got := novelAIQuotaSettingsFrom(&full)
	if !got.GuardEnabled || !got.AllowOnLookupFailure || got.ReserveImages != 3 || got.CacheTTL.Seconds() != 45 {
		t.Fatalf("new config not applied: %+v", got)
	}

	// 显式 0 走完整 JSON 往返后仍必须是 0（不能被当成「未设置」回落到 1）。
	// 这是 *int 设计的端到端验证：前端 InputNumber 填 0 → JSON 里 `"...":0` → 后端 0。
	zeroJSON := `{"enabled":true,"v5QuotaGuardEnabled":true,"v5QuotaReserveImages":0}`
	var zero model.FreeGenerationLock
	if err := json.Unmarshal([]byte(zeroJSON), &zero); err != nil {
		t.Fatalf("zero-reserve config must parse: %v", err)
	}
	if zero.V5QuotaReserveImages == nil {
		t.Fatal("explicit 0 in JSON must produce a non-nil pointer")
	}
	if settings := novelAIQuotaSettingsFrom(&zero); settings.ReserveImages != 0 {
		t.Fatalf("explicit 0 survived JSON but became %d after parsing", settings.ReserveImages)
	}
}

// TestBatchStopsWhenQuotaConsumedConcurrently 复现「入口放行后，配额被别人吃掉」。
//
// 这是批内复查在生产中**真正**生效的场景：同一个 Token 的配额池是全渠道共享的，
// 入口按当时余量放行 3 张，但出第 1 张的过程中另一个用户（这里用 onImageDone
// 回调模拟）把配额消耗掉了，剩下两张就必须被拦住，而不是白扣 Anlas。
func TestBatchStopsWhenQuotaConsumedConcurrently(t *testing.T) {
	// 给足量（够 10 张），确保入口与首张都能放行。
	server, _, imageCalls := novelAIStubServer(t, func() string {
		return opusBody(quotaFor(10), false)
	})

	lock := guardLock()
	lock.V5QuotaCacheSeconds = 3600
	channel := quotaTestChannel(t, server.URL, lock)

	// 入口按 3 张放行（3 + 1 保留 = 4 份 ≤ 10 份）。
	naiReq := mustConvertToNovelAIRequest(t, batchRequest("nai-diffusion-5-full", 1))
	if err := ensureNovelAIV5Quota(context.Background(), channel, naiReq.Model, naiReq.Parameters.Width, naiReq.Parameters.Height, 3); err != nil {
		t.Fatalf("entry guard should allow 3 images at this level: %v", err)
	}

	// 第 1 张出完后，模拟「别人」把剩余配额几乎吃光（吃掉 9 张的量）。
	onImageDone := func(current, total int) {
		if current == 1 {
			consumeNovelAIV5Quota(channel, "nai-diffusion-5-full", 1024, 1024, 9)
		}
	}

	_, succeeded, err := requestNovelAISingleImageBatch(
		context.Background(), batchRequest("nai-diffusion-5-full", 3), 3, channel, "user-1", nil, onImageDone,
	)
	if err != nil {
		t.Fatalf("partial success must not be a hard error: %v", err)
	}
	// 第 1 张出了；随后配额被吃光，第 2、3 张必须被拦。
	if succeeded != 1 {
		t.Fatalf("succeeded = %d, want 1 (quota was drained by a concurrent consumer after the first image)", succeeded)
	}
	if got := imageCalls.Load(); got != 1 {
		t.Fatalf("upstream image calls = %d, want 1 (remaining slots must not spend Anlas)", got)
	}
}
