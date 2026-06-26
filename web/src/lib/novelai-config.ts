import { DEFAULT_NOVELAI_SETTINGS, NOVELAI_AQT_PRESETS, NOVELAI_NOISE_SCHEDULES, NOVELAI_SAMPLERS, NOVELAI_UC_PRESETS } from "@/components/novelai/novelai-constants";
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
            if (!characterPrompt && !characterNegativePrompt) return null;
            const coords = value.coords && typeof value.coords === "object" ? value.coords : undefined;
            return {
                displayName: String(value.displayName || `角色${index + 1}`).slice(0, 20),
                characterPrompt,
                ...(characterNegativePrompt ? { characterNegativePrompt } : {}),
                ...(coords ? { coords: { x: clampInteger(coords.x, 0, 4, 2), y: clampInteger(coords.y, 0, 4, 2) } } : {}),
            } satisfies NovelAICharacterPrompt;
        })
        .filter((item): item is NovelAICharacterPrompt => Boolean(item))
        .slice(0, 6);
}

export function normalizeNovelAISettings(config: Partial<NovelAISettings>): NovelAISettings {
    const merged = { ...DEFAULT_NOVELAI_SETTINGS, ...config };
    return {
        novelAIEnabled: Boolean(merged.novelAIEnabled),
        novelAIModel: String(merged.novelAIModel || DEFAULT_NOVELAI_SETTINGS.novelAIModel),
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
    };
}

export function buildNovelAIRequestParameters(config: Partial<NovelAISettings>): Record<string, unknown> {
    const settings = normalizeNovelAISettings(config);
    if (!settings.novelAIEnabled) return {};
    return {
        novelai_enabled: true,
        novelai_model: settings.novelAIModel,
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
        aqt_preset: settings.novelAIAqtPreset,
        divide_roles: settings.novelAIDivideRoles,
        use_auto_positioning: settings.novelAIUseAutoPositioning,
        character_prompts: settings.novelAICharacterPrompts,
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
