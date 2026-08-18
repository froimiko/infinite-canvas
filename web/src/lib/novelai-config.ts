import { DEFAULT_NOVELAI_SETTINGS, NOVELAI_AQT_PRESETS, NOVELAI_NOISE_SCHEDULES, NOVELAI_SAMPLERS, NOVELAI_UC_PRESETS } from "@/components/novelai/novelai-constants";
import { normalizePromptBlockTokens } from "@/components/prompt-block-editor/prompt-block-utils";
import { modelOptionName } from "@/stores/use-config-store";
import type { NovelAICharacterPrompt, NovelAISettings } from "@/types/image";

export function isNovelAIModel(model: string) {
    const value = model.toLowerCase();
    return value.includes("nai-") || value.includes("novelai") || value.includes("nai-diffusion");
}

export function normalizeNovelAICharacterPrompts(prompts: unknown): NovelAICharacterPrompt[] {
    if (!Array.isArray(prompts)) return [];
    return prompts
        .map((item, index) => {
            const value = item && typeof item === "object" ? (item as Partial<NovelAICharacterPrompt>) : {};
            const characterPrompt = String(value.characterPrompt || "").trim();
            const characterNegativePrompt = String(value.characterNegativePrompt || "").trim();
            const characterPromptTokens = normalizePromptBlockTokens(Array.isArray(value.characterPromptTokens) ? value.characterPromptTokens : []);
            const characterNegativePromptTokens = normalizePromptBlockTokens(Array.isArray(value.characterNegativePromptTokens) ? value.characterNegativePromptTokens : []);
            if (!characterPrompt && !characterNegativePrompt && !characterPromptTokens.length && !characterNegativePromptTokens.length) return null;
            const coords = value.coords && typeof value.coords === "object" ? value.coords : undefined;
            return {
                displayName: String(value.displayName || `角色${index + 1}`).slice(0, 20),
                characterPrompt,
                ...(characterPromptTokens.length ? { characterPromptTokens } : {}),
                ...(characterNegativePrompt ? { characterNegativePrompt } : {}),
                ...(characterNegativePromptTokens.length ? { characterNegativePromptTokens } : {}),
                ...(coords ? { coords: { x: clampInteger(coords.x, 0, 4, 2), y: clampInteger(coords.y, 0, 4, 2) } } : {}),
            } satisfies NovelAICharacterPrompt;
        })
        .filter((item): item is NovelAICharacterPrompt => Boolean(item))
        .slice(0, 6);
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
        divide_roles: settings.novelAIDivideRoles,
        use_auto_positioning: settings.novelAIUseAutoPositioning,
        character_prompts: settings.novelAICharacterPrompts.map(({ characterPromptTokens, characterNegativePromptTokens, ...prompt }) => prompt),
        quality_toggle: settings.novelAIQualityToggle,
        add_original_image: settings.novelAIAddOriginalImage,
    };
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
