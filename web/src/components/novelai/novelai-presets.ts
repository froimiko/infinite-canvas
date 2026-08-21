/**
 * NovelAI 官方质量词与负面词预设，内容对齐 Aaalice_NAI_Launcher。
 * 预设不写入提示词输入框，只在生成时按当前模型注入请求。
 */

export type NovelAIQualityPreset = "nai-default" | "none";
export type NovelAIUcPreset = "heavy" | "light" | "furry" | "human" | "none";

export const NOVELAI_QUALITY_PRESET_OPTIONS: { value: NovelAIQualityPreset; label: string }[] = [
    { value: "nai-default", label: "NAI 默认" },
    { value: "none", label: "无" },
];

export const NOVELAI_UC_PRESET_OPTIONS: { value: NovelAIUcPreset; label: string }[] = [
    { value: "heavy", label: "重度" },
    { value: "light", label: "轻度" },
    { value: "furry", label: "Furry" },
    { value: "human", label: "人物" },
    { value: "none", label: "无" },
];

export const DEFAULT_NOVELAI_QUALITY_PRESET: NovelAIQualityPreset = "nai-default";
export const DEFAULT_NOVELAI_UC_PRESET: NovelAIUcPreset = "heavy";

// NovelAI 官方文档尚未公开 V5 专属质量词。先按对应版本继承 V4.5，
// 这样 V5 不会因为未知模型而静默落到 V3 质量词；官方更新后只需改这两个常量。
const V45_FULL_QUALITY_TAGS = "location, very aesthetic, masterpiece, no text";
const V45_CURATED_QUALITY_TAGS = "location, masterpiece, no text, -0.8::feet::, rating:general";

const QUALITY_TAGS: Record<string, string> = {
    "nai-diffusion-5-full": V45_FULL_QUALITY_TAGS,
    "nai-diffusion-5-curated": V45_CURATED_QUALITY_TAGS,
    "nai-diffusion-4-5-full": V45_FULL_QUALITY_TAGS,
    "nai-diffusion-4-5-curated": V45_CURATED_QUALITY_TAGS,
    "nai-diffusion-4-full": "no text, best quality, very aesthetic, absurdres",
    "nai-diffusion-4-curated-preview": "rating:general, amazing quality, very aesthetic, absurdres",
    "nai-diffusion-3": "best quality, amazing quality, very aesthetic, absurdres",
    "nai-diffusion-furry-3": "{best quality}, {amazing quality}",
};

const FURRY_UC =
    "{{worst quality}}, [displeasing], {unusual pupils}, guide lines, {{unfinished}}, {bad}, url, artist name, {{tall image}}, mosaic, {sketch page}, comic panel, impact (font), [dated], {logo}, ych, {what}, {where is your god now}, {distorted text}, repeated text, {floating head}, {1994}, {widescreen}, absolutely everyone, sequence, {compression artifacts}, hard translated, {cropped}, {commissioner name}, unknown text, high contrast";

const V45_FULL_UC_PRESETS: Record<NovelAIUcPreset, string> = {
    heavy: "lowres, artistic error, film grain, scan artifacts, worst quality, bad quality, jpeg artifacts, very displeasing, chromatic aberration, dithering, halftone, screentone, multiple views, logo, too many watermarks, negative space, blank page",
    light: "lowres, artistic error, scan artifacts, worst quality, bad quality, jpeg artifacts, multiple views, very displeasing, too many watermarks, negative space, blank page",
    furry: "{worst quality}, distracting watermark, unfinished, bad quality, {widescreen}, upscale, {sequence}, {{grandfathered content}}, blurred foreground, chromatic aberration, sketch, everyone, [sketch background], simple, [flat colors], ych (character), outline, multiple scenes, [[horror (theme)]], comic",
    human: "lowres, artistic error, film grain, scan artifacts, worst quality, bad quality, jpeg artifacts, very displeasing, chromatic aberration, dithering, halftone, screentone, multiple views, logo, too many watermarks, negative space, blank page, @_@, mismatched pupils, glowing eyes, bad anatomy",
    none: "",
};

const V45_CURATED_UC_PRESETS: Record<NovelAIUcPreset, string> = {
    heavy: "blurry, lowres, upscaled, artistic error, film grain, scan artifacts, worst quality, bad quality, jpeg artifacts, very displeasing, chromatic aberration, halftone, multiple views, logo, too many watermarks, negative space, blank page",
    light: "blurry, lowres, upscaled, artistic error, scan artifacts, jpeg artifacts, logo, too many watermarks, negative space, blank page",
    furry: "{worst quality}, distracting watermark, unfinished, bad quality, {widescreen}, upscale, {sequence}, {{grandfathered content}}, blurred foreground, chromatic aberration, sketch, everyone, [sketch background], simple, [flat colors], ych (character), outline, multiple scenes, [[horror (theme)]], comic",
    human: "blurry, lowres, upscaled, artistic error, film grain, scan artifacts, bad anatomy, bad hands, worst quality, bad quality, jpeg artifacts, very displeasing, chromatic aberration, halftone, multiple views, logo, too many watermarks, @_@, mismatched pupils, glowing eyes, negative space, blank page",
    none: "",
};

// V5 官方专属 UC 文本尚未公开，暂按对应 V4.5 版本继承；
// 显式列出 V5 key，避免未知模型落到 V3_UC_PRESET。
const UC_PRESETS: Record<string, Record<NovelAIUcPreset, string>> = {
    "nai-diffusion-5-full": V45_FULL_UC_PRESETS,
    "nai-diffusion-5-curated": V45_CURATED_UC_PRESETS,
    "nai-diffusion-4-5-full": V45_FULL_UC_PRESETS,
    "nai-diffusion-4-5-curated": V45_CURATED_UC_PRESETS,
    "nai-diffusion-4-full": {
        heavy: "blurry, lowres, error, film grain, scan artifacts, worst quality, bad quality, jpeg artifacts, very displeasing, chromatic aberration, multiple views, logo, too many watermarks",
        light: "blurry, lowres, error, worst quality, bad quality, jpeg artifacts, very displeasing",
        furry: FURRY_UC,
        human: "blurry, lowres, error, film grain, scan artifacts, worst quality, bad quality, jpeg artifacts, very displeasing, chromatic aberration, multiple views, logo, too many watermarks, bad anatomy, bad hands",
        none: "",
    },
    "nai-diffusion-4-curated-preview": {
        heavy: "blurry, lowres, error, film grain, scan artifacts, worst quality, bad quality, jpeg artifacts, very displeasing, chromatic aberration, logo, dated, signature, multiple views, gigantic breasts",
        light: "blurry, lowres, error, worst quality, bad quality, jpeg artifacts, very displeasing, logo, dated, signature",
        furry: FURRY_UC,
        human: "blurry, lowres, error, film grain, scan artifacts, worst quality, bad quality, jpeg artifacts, very displeasing, chromatic aberration, logo, dated, signature, multiple views, gigantic breasts, bad anatomy, bad hands",
        none: "",
    },
    "nai-diffusion-furry-3": {
        heavy: FURRY_UC,
        light: "{worst quality}, guide lines, unfinished, bad, url, tall image, widescreen, compression artifacts, unknown text",
        furry: FURRY_UC,
        human: FURRY_UC,
        none: "",
    },
};

const V3_UC_PRESET: Record<NovelAIUcPreset, string> = {
    heavy: "lowres, {bad}, error, fewer, extra, missing, worst quality, jpeg artifacts, bad quality, watermark, unfinished, displeasing, chromatic aberration, signature, extra digits, artistic error, username, scan, [abstract]",
    light: "lowres, jpeg artifacts, worst quality, watermark, blurry, very displeasing",
    furry: FURRY_UC,
    human: "lowres, {bad}, error, fewer, extra, missing, worst quality, jpeg artifacts, bad quality, watermark, unfinished, displeasing, chromatic aberration, signature, extra digits, artistic error, username, scan, [abstract], bad anatomy, bad hands, @_@, mismatched pupils, heart-shaped pupils, glowing eyes",
    none: "lowres",
};

/** 把用户配置的模型名归一到官方模型 ID，规则与后端 resolveNovelAIModel 保持一致。 */
export function resolveNovelAIModelId(model: string | undefined) {
    const value = (model || "").toLowerCase().trim().replace(/_/g, "-");
    if (!value) return "nai-diffusion-3";
    if (QUALITY_TAGS[value]) return value;
    // 使用词边界匹配，防止 "anime-v3-style" 之类的子串误匹配。
    // 顺序必须从新到旧：先 V5，再 V4.5/V4；V5 简写默认 Full，含 curated 才归 Curated。
    // 裸 "5" 只能完全相等，不能用词边界匹配："v4.5" 里的小数点也是分隔符，会把末尾 5 误判成 V5。
    if (value === "5" || hasModelKeyword(value, "v5") || hasModelKeyword(value, "nai-diffusion-5")) return value.includes("curated") ? "nai-diffusion-5-curated" : "nai-diffusion-5-full";
    if (hasModelKeyword(value, "4.5") || hasModelKeyword(value, "v4.5") || hasModelKeyword(value, "4-5")) return value.includes("curated") ? "nai-diffusion-4-5-curated" : "nai-diffusion-4-5-full";
    if (hasModelKeyword(value, "nai-diffusion-4") || hasModelKeyword(value, "v4")) return value.includes("full") ? "nai-diffusion-4-full" : "nai-diffusion-4-curated-preview";
    if (hasModelKeyword(value, "nai-diffusion-3") || hasModelKeyword(value, "v3")) return "nai-diffusion-3";
    if (hasModelKeyword(value, "furry")) return "nai-diffusion-furry-3";
    return "nai-diffusion-3";
}

/**
 * 检查 keyword 是否在 model 中以词边界出现。
 * 词边界为：字符串首尾、连字符、空格、点号。
 * 例如 "nai-diffusion-v4-5" 中 "v4" 前后都是连字符 → 匹配。
 * "xv4y" 中 "v4" 前后都是字母 → 不匹配。
 */
function hasModelKeyword(model: string, keyword: string): boolean {
    let idx = model.indexOf(keyword);
    while (idx >= 0) {
        const beforeOk = idx === 0 || !isModelNameChar(model.charCodeAt(idx - 1));
        const afterIdx = idx + keyword.length;
        const afterOk = afterIdx >= model.length || !isModelNameChar(model.charCodeAt(afterIdx));
        if (beforeOk && afterOk) return true;
        idx = model.indexOf(keyword, idx + 1);
    }
    return false;
}

function isModelNameChar(c: number): boolean {
    return (c >= 97 && c <= 122) || (c >= 48 && c <= 57); // a-z or 0-9
}

export function normalizeNovelAIQualityPreset(value: unknown): NovelAIQualityPreset {
    return value === "none" ? "none" : DEFAULT_NOVELAI_QUALITY_PRESET;
}

export function normalizeNovelAIUcPreset(value: unknown): NovelAIUcPreset {
    return NOVELAI_UC_PRESET_OPTIONS.some((item) => item.value === value) ? (value as NovelAIUcPreset) : DEFAULT_NOVELAI_UC_PRESET;
}

/** 取模型对应的质量词；预设为「无」时返回空串。 */
export function novelAIQualityTags(model: string, preset: NovelAIQualityPreset) {
    return preset === "none" ? "" : QUALITY_TAGS[model] || "";
}

/** 取模型对应的负面预设词；未知模型回落到 V3 预设，与参考实现一致。 */
export function novelAIUcPresetTags(model: string, preset: NovelAIUcPreset) {
    return (UC_PRESETS[model] || V3_UC_PRESET)[preset] || "";
}

/** 质量词拼在正面提示词末尾。 */
export function applyNovelAIQualityTags(prompt: string, model: string, preset: NovelAIQualityPreset) {
    const tags = novelAIQualityTags(model, preset);
    const trimmed = prompt.trim();
    if (!tags) return trimmed;
    if (!trimmed) return tags;
    return trimmed.endsWith(",") ? `${trimmed} ${tags}` : `${trimmed}, ${tags}`;
}

/** 负面预设词拼在用户负面提示词前面。 */
export function applyNovelAIUcPreset(negativePrompt: string, model: string, preset: NovelAIUcPreset) {
    const tags = novelAIUcPresetTags(model, preset);
    const trimmed = negativePrompt.trim();
    if (!tags) return trimmed;
    if (!trimmed) return tags;
    return `${tags}, ${trimmed}`;
}

/**
 * 把编辑器里的负面质量词下拉映射为后端 uc_preset 名称。
 * 后端会再转成 NovelAI 官方整数（Heavy=0/Light=1/Human Focus=2/None=3）。
 * Furry 在官方 API 里没有对应整数，预设词已在本地注入，因此按「无预设」上报，
 * 避免发送非法值让上游回落到默认 Heavy 预设、叠加出双份负面词。
 */
export function novelAIUcPresetApiName(preset: NovelAIUcPreset): "Heavy" | "Light" | "Human Focus" | "None" {
    switch (preset) {
        case "heavy":
            return "Heavy";
        case "light":
            return "Light";
        case "human":
            return "Human Focus";
        default:
            return "None";
    }
}
