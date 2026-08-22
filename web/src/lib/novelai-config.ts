import { CHARACTER_GENDER_OPTIONS, characterPositionForIndex, clampCharacterPosition, resolveEffectiveGender, supportsMultiCharacter } from "@/components/novelai/character-position-layout";
import { DEFAULT_NOVELAI_SETTINGS, NOVELAI_AQT_PRESETS, NOVELAI_NOISE_SCHEDULES, NOVELAI_SAMPLERS, NOVELAI_UC_PRESETS } from "@/components/novelai/novelai-constants";
import { normalizePromptBlockTokens } from "@/components/prompt-block-editor/prompt-block-utils";
import { modelOptionName } from "@/stores/use-config-store";
import type { NovelAICharacterPrompt, NovelAISettings } from "@/types/image";
import { nanoid } from "nanoid";

export function isNovelAIModel(model: string) {
    const value = model.toLowerCase();
    return value.includes("nai-") || value.includes("novelai") || value.includes("nai-diffusion");
}

/**
 * 角色数量上限（归一化层）。
 *
 * 取 V5 的官方上限 32。历史上这里写死 6（V4/V4.5 的上限），结果 V5 的第 7 个及以后
 * 角色在归一化时被静默吃掉 —— 界面上还在，存档一读就没了，极难排查。
 * 真正的「按模型限制」交给 UI（resolveCharacterLimit），归一化只兜最宽的底。
 * 画布旧面板自身有 MAX_CHARACTERS = 6 的 UI 限制，放宽这里不影响画布行为。
 */
export const MAX_NOVELAI_CHARACTERS = 32;

export function normalizeNovelAICharacterPrompts(prompts: unknown): NovelAICharacterPrompt[] {
    if (!Array.isArray(prompts)) return [];
    return prompts
        .map((item, index) => {
            const value = item && typeof item === "object" ? (item as Partial<NovelAICharacterPrompt>) : {};
            const characterPrompt = String(value.characterPrompt || "").trim();
            const characterNegativePrompt = String(value.characterNegativePrompt || "").trim();
            const characterPromptTokens = normalizePromptBlockTokens(Array.isArray(value.characterPromptTokens) ? value.characterPromptTokens : []);
            const characterNegativePromptTokens = normalizePromptBlockTokens(Array.isArray(value.characterNegativePromptTokens) ? value.characterNegativePromptTokens : []);
            // 提示词全空的角色**不能丢**：用户刚点「+♀女」时正面词只有 `girl, `、负面词是空的，
            // 中途清空重打字更是常态，一旦归一化直接删掉，角色卡会在输入过程中凭空消失。
            // 与参考项目一致保留占位；真正的过滤放在 buildNovelAIRequestParameters，
            // 出请求时才剔空，避免给上游发空 char_caption。
            const coords = value.coords && typeof value.coords === "object" ? value.coords : undefined;
            const center = value.center && typeof value.center === "object" ? value.center : undefined;
            return {
                displayName: String(value.displayName || `角色${index + 1}`).slice(0, 20),
                characterPrompt,
                ...(characterPromptTokens.length ? { characterPromptTokens } : {}),
                ...(characterNegativePrompt ? { characterNegativePrompt } : {}),
                ...(characterNegativePromptTokens.length ? { characterNegativePromptTokens } : {}),
                ...(coords ? { coords: { x: clampInteger(coords.x, 0, 4, 2), y: clampInteger(coords.y, 0, 4, 2) } } : {}),
                ...(center ? { center: clampCharacterPosition(center) } : {}),
                // 老存档没有 id（这个字段是多角色工作台才加的），缺失时补一个，
                // 否则 React key 只能退化成数组下标，删除中间项会串行内容。
                id: String(value.id || "") || nanoid(),
                // gender 只是「添加时点了哪个按钮」的记录，缺失时按首个 tag 反推，
                // 这样老存档也能有个合理值。UI 配色始终实时算，不读这里。
                gender: oneOf(String(value.gender), CHARACTER_GENDER_OPTIONS, resolveEffectiveGender(characterPrompt)),
                // 默认启用：老存档一律没有这个字段，用 !== false 才不会把它们全部读成禁用。
                enabled: value.enabled !== false,
            } satisfies NovelAICharacterPrompt;
        })
        .slice(0, MAX_NOVELAI_CHARACTERS);
}

/**
 * 归一化 NovelAI 设置。
 *
 * 注意 novelAIModel 的兜底顺序：画布 NovelAI 节点创建时 novelAIModel 故意留空
 * （让 ModelPicker 显示全局默认模型），此时必须回落到 config.model / imageModel，
 * 绝不能直接落到 DEFAULT_NOVELAI_SETTINGS.novelAIModel（V3）——否则下拉里显示
 * V4.5 Full、实际却按 V3 出图，连质量词都会取成 V3 的那一套。
 */
export function normalizeNovelAISettings(config: Partial<NovelAISettings> & { model?: string; imageModel?: string }): NovelAISettings {
    const merged = { ...DEFAULT_NOVELAI_SETTINGS, ...config };
    return {
        novelAIEnabled: Boolean(merged.novelAIEnabled),
        novelAIModel: String(merged.novelAIModel?.trim() || config.model?.trim() || config.imageModel?.trim() || DEFAULT_NOVELAI_SETTINGS.novelAIModel),
        novelAISampler: oneOf(String(merged.novelAISampler), NOVELAI_SAMPLERS, DEFAULT_NOVELAI_SETTINGS.novelAISampler),
        novelAISteps: clampInteger(merged.novelAISteps, 1, 50, DEFAULT_NOVELAI_SETTINGS.novelAISteps),
        novelAICfgScale: clampNumber(merged.novelAICfgScale, 1, 25, DEFAULT_NOVELAI_SETTINGS.novelAICfgScale),
        novelAISeed: normalizeSeed(merged.novelAISeed),
        novelAIUcPreset: oneOf(String(merged.novelAIUcPreset), NOVELAI_UC_PRESETS, DEFAULT_NOVELAI_SETTINGS.novelAIUcPreset),
        novelAICfgRescale: clampNumber(merged.novelAICfgRescale, 0, 1, DEFAULT_NOVELAI_SETTINGS.novelAICfgRescale),
        novelAINoiseSchedule: oneOf(String(merged.novelAINoiseSchedule), NOVELAI_NOISE_SCHEDULES, DEFAULT_NOVELAI_SETTINGS.novelAINoiseSchedule),
        novelAISm: Boolean(merged.novelAISm),
        novelAISmDyn: Boolean(merged.novelAISmDyn),
        novelAIDynamicThresholding: Boolean(merged.novelAIDynamicThresholding),
        novelAIVarietyPlus: Boolean(merged.novelAIVarietyPlus),
        novelAIAqtPreset: oneOf(String(merged.novelAIAqtPreset), NOVELAI_AQT_PRESETS, DEFAULT_NOVELAI_SETTINGS.novelAIAqtPreset),
        novelAIDivideRoles: Boolean(merged.novelAIDivideRoles),
        novelAIUseAutoPositioning: Boolean(merged.novelAIUseAutoPositioning),
        novelAICharacterPrompts: normalizeNovelAICharacterPrompts(merged.novelAICharacterPrompts),
        // 这两个开关默认关闭，所以必须用 === true 判定。
        // 写成 !== false 会让 undefined 变成 true，等于默认永远开着，
        // 与 DEFAULT_NOVELAI_SETTINGS 对不上（历史上就是这么写错的）。
        novelAIQualityToggle: merged.novelAIQualityToggle === true,
        novelAIAddOriginalImage: merged.novelAIAddOriginalImage === true,
    };
}

export function buildNovelAIRequestParameters(config: Partial<NovelAISettings>): Record<string, unknown> {
    const settings = normalizeNovelAISettings(config);
    if (!settings.novelAIEnabled) return {};
    return {
        novelai_enabled: true,
        // 必须用 modelOptionName 去掉 `渠道id::` 前缀。
        // 云端模型的值形如 `__cloud__::nai-diffusion-4-5-full`，整串发给后端会让
        // resolveNovelAIModel 只能走模糊匹配（甚至匹配失败回落到 V3），
        // 反复切换模型还会叠成 `__cloud__::__cloud__::...` 导致上游 502。
        novelai_model: modelOptionName(settings.novelAIModel),
        sampler: settings.novelAISampler,
        steps: settings.novelAISteps,
        cfg_scale: settings.novelAICfgScale,
        seed: settings.novelAISeed,
        uc_preset: settings.novelAIUcPreset,
        cfg_rescale: settings.novelAICfgRescale,
        noise_schedule: settings.novelAINoiseSchedule,
        sm: settings.novelAISm,
        sm_dyn: settings.novelAISmDyn,
        dynamic_thresholding: settings.novelAIDynamicThresholding,
        variety_plus: settings.novelAIVarietyPlus,
        // V3 没有 v4_prompt / char_captions 协议，三个多角色字段必须整组省略，
        // 不能只发 false/[]：那仍然是向不支持的模型发送无意义参数。
        // supportsMultiCharacter 会先 resolveNovelAIModelId，可处理 `渠道id::` 前缀。
        ...(supportsMultiCharacter(settings.novelAIModel)
            ? {
                  divide_roles: settings.novelAIDivideRoles,
                  use_auto_positioning: settings.novelAIUseAutoPositioning,
                  character_prompts: buildNovelAICharacterPromptPayload(settings.novelAICharacterPrompts),
              }
            : {}),
        quality_toggle: settings.novelAIQualityToggle,
        add_original_image: settings.novelAIAddOriginalImage,
    };
}

/**
 * 判断角色是否会进入 NovelAI 的 character_prompts 载荷。
 *
 * 两道判定（enabled === false、正负提示词都为空）是「有效角色」的唯一口径：
 *  - 请求构造（buildNovelAICharacterPromptPayload）按它过滤；
 *  - 快照的 divide_roles 判定（page.tsx 里用 countEffectiveNovelAICharacters）按它计数。
 * 两处若各写一份规则，迟早漂移出「说开启分离、payload 却为空」的自相矛盾请求
 * （后端虽有 len(charCaptions) > 0 兜底，但前端不该发这种参数）。
 * 归一化层刻意不丢角色（空提示词占位 / 临时禁用都要留在界面上供编辑），过滤只在这里做。
 */
function isEffectiveNovelAICharacterPrompt(prompt: NovelAICharacterPrompt) {
    // count helper 会直接吃工作台 store 的原始值，而 payload 构造前会先经过 normalize（含 trim）。
    // 这里也 trim，避免纯空白在计数时被当成有效、到 payload 时又被归一化成空串。
    return prompt.enabled !== false && Boolean(String(prompt.characterPrompt || "").trim() || String(prompt.characterNegativePrompt || "").trim());
}
/** 有效角色数（启用且正负提示词非空）。divide_roles 的判定必须用它，与请求载荷口径一致。 */
export function countEffectiveNovelAICharacters(prompts: NovelAICharacterPrompt[]) {
    return prompts.filter(isEffectiveNovelAICharacterPrompt).length;
}

/**
 * 解析角色最终生效的连续坐标（center）。
 *
 * 位置画布的锚点显示与请求载荷**必须都调用它**，否则会分叉成两套坐标 ——
 * 参考项目 character_prompt.dart 第 55-58 行专门写了这条警告，
 * 它的 resolvePosition（第 349-363 行）就是这个函数的原型。
 *
 * 为什么兜底要按「有效角色」的 index/total 而不是整个数组：
 * 载荷只发有效角色，后端 char_captions 的下标是过滤后的下标。
 * 若按整个数组算兜底，禁用/空占位角色会占掉编号，画布显示的位置
 * 和后端收到的坐标就对不上。
 *
 * 历史坑：这里曾经只在画布侧算兜底、载荷侧不带 center，导致没拖过的角色
 * 在弹窗里分散显示（0.2/0.5/0.8），实际却全被后端兜底成 0.5/0.5 挤在画面中央 ——
 * 界面看着分散、出图却全叠在一起，且不报错。
 */
export function resolveNovelAICharacterCenter(character: NovelAICharacterPrompt, prompts: NovelAICharacterPrompt[]) {
    if (character.center) return clampCharacterPosition(character.center);
    const effective = prompts.filter(isEffectiveNovelAICharacterPrompt);
    const matches = (item: NovelAICharacterPrompt) => (item.id && character.id ? item.id === character.id : item === character);
    const effectiveIndex = effective.findIndex(matches);
    // 找不到说明这个角色本身不会进载荷（被禁用或提示词为空），
    // 退回整表下标只为让它在画布上有个稳定的显示位置。
    if (effectiveIndex < 0) return characterPositionForIndex(prompts.findIndex(matches), prompts.length);
    return characterPositionForIndex(effectiveIndex, effective.length);
}

/**
 * 把角色列表裁成后端认识的 character_prompts 载荷。
 *
 * 输出同时带 center（连续坐标）与 coords（网格，画布链路兼容），
 * 后端按 center > coords 取。id / gender / enabled 是纯前端状态，
 * 后端结构体里没有对应字段，发过去只会被忽略，没必要占体积。
 *
 * center 一律用 resolveNovelAICharacterCenter 显式补齐，**不能留空让后端兜底**：
 * 后端对缺失坐标只会统一回落 {0.5, 0.5}，多个没拖过的角色会全叠在画面中央，
 * 而位置画布上它们是按默认布局分散显示的 —— 界面与出图直接分叉。
 */
function buildNovelAICharacterPromptPayload(prompts: NovelAICharacterPrompt[]) {
    return prompts.filter(isEffectiveNovelAICharacterPrompt).map(({ characterPromptTokens, characterNegativePromptTokens, id, gender, enabled, ...prompt }) => ({ ...prompt, center: resolveNovelAICharacterCenter({ ...prompt, id, enabled }, prompts) }));
}

function oneOf<T extends readonly string[] | string[]>(value: string, options: T, fallback: T[number]): T[number] {
    return options.includes(value) ? value : fallback;
}

function normalizeSeed(value: unknown) {
    const seed = Math.floor(Number(value));
    if (!Number.isFinite(seed) || seed < 0) return -1;
    return seed;
}

function clampInteger(value: unknown, min: number, max: number, fallback: number) {
    return Math.max(min, Math.min(max, Math.floor(Number(value) || fallback)));
}

function clampNumber(value: unknown, min: number, max: number, fallback: number) {
    const number = Number(value);
    return Math.max(min, Math.min(max, Number.isFinite(number) ? number : fallback));
}
