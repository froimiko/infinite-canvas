import type { NovelAISettings, NovelAIAqtPreset, NovelAINoiseSchedule, NovelAIUCPreset } from "@/types/image";

export const NOVELAI_MODELS = [
    { id: "nai-diffusion-3", name: "NovelAI Diffusion V3" },
    { id: "nai-diffusion-4-curated-preview", name: "NAI Diffusion V4 Curated" },
    { id: "nai-diffusion-4-full", name: "NAI Diffusion V4 Full" },
    { id: "nai-diffusion-4-5-curated", name: "NAI Diffusion V4.5 Curated" },
    { id: "nai-diffusion-4-5-full", name: "NAI Diffusion V4.5 Full" },
] as const;

export const NOVELAI_SAMPLERS = ["k_euler", "k_euler_ancestral", "k_dpmpp_2s_ancestral", "k_dpmpp_2m", "k_dpmpp_sde", "ddim_v3"] as const;

export const NOVELAI_UC_PRESETS: NovelAIUCPreset[] = ["Heavy", "Light", "None", "Human Focus"];
export const NOVELAI_NOISE_SCHEDULES: NovelAINoiseSchedule[] = ["native", "karras", "exponential", "polyexponential"];
export const NOVELAI_AQT_PRESETS: NovelAIAqtPreset[] = ["safe", "nai", "full", "balanced", "anime", "furry", "pony"];

export const NOVELAI_DEFAULT_NEGATIVE_PROMPT = "lowres, bad anatomy, bad hands, text, error, missing fingers, extra digit, fewer digits, cropped, worst quality, low quality, normal quality, jpeg artifacts, signature, watermark, username, blurry";

export const DEFAULT_NOVELAI_SETTINGS: NovelAISettings = {
    novelAIEnabled: false,
    novelAIModel: "nai-diffusion-3",
    novelAISampler: "k_euler",
    novelAISteps: 28,
    novelAICfgScale: 5,
    novelAISeed: -1,
    novelAIUcPreset: "Heavy",
    novelAICfgRescale: 0.18,
    novelAINoiseSchedule: "native",
    novelAISm: true,
    novelAISmDyn: true,
    novelAIDynamicThresholding: true,
    novelAIVarietyPlus: false,
    novelAIAqtPreset: "safe",
    novelAIDivideRoles: false,
    novelAIUseAutoPositioning: false,
    novelAICharacterPrompts: [],
};
