import type { NovelAISettings, NovelAIAqtPreset, NovelAINoiseSchedule, NovelAIUCPreset } from "@/types/image";

export const NOVELAI_MODELS = [
    { id: "nai-diffusion-5-full", name: "NAI Diffusion V5 Full" },
    { id: "nai-diffusion-5-curated", name: "NAI Diffusion V5 Curated" },
    { id: "nai-diffusion-4-5-full", name: "NAI Diffusion V4.5 Full" },
    { id: "nai-diffusion-4-5-curated", name: "NAI Diffusion V4.5 Curated" },
    { id: "nai-diffusion-4-full", name: "NAI Diffusion V4 Full" },
    { id: "nai-diffusion-4-curated-preview", name: "NAI Diffusion V4 Curated" },
    { id: "nai-diffusion-3", name: "NovelAI Diffusion V3" },
] as const;

export const NOVELAI_SAMPLERS = ["k_euler", "k_euler_ancestral", "k_dpmpp_2s_ancestral", "k_dpmpp_2m", "k_dpmpp_sde", "ddim_v3"] as const;

export const NOVELAI_UC_PRESETS: NovelAIUCPreset[] = ["Heavy", "Light", "None", "Human Focus"];
export const NOVELAI_NOISE_SCHEDULES: NovelAINoiseSchedule[] = ["native", "karras", "exponential", "polyexponential"];
export const NOVELAI_AQT_PRESETS: NovelAIAqtPreset[] = ["safe", "nai", "full", "balanced", "anime", "furry", "pony"];

export const NOVELAI_DEFAULT_NEGATIVE_PROMPT = "lowres, bad anatomy, bad hands, text, error, missing fingers, extra digit, fewer digits, cropped, worst quality, low quality, normal quality, jpeg artifacts, signature, watermark, username, blurry";

// 默认值逐项对齐 Aaalice_NAI_Launcher 的 ImageParams（lib/data/models/image/image_params.dart）。
// 这些默认值直接决定出图观感，改动前请先核对参考实现，不要凭直觉给"看起来更好"的值：
// sampler=k_euler_ancestral、cfg_rescale=0、noise_schedule=karras、smea/smea_dyn/decrisp=false。
// 历史上这里把 sampler 写成 k_euler、cfg_rescale 写成 0.18、noise_schedule 写成 native、
// decrisp/smea 写成 true，出图会明显发软发平，像"步数没跑完"。
export const DEFAULT_NOVELAI_SETTINGS: NovelAISettings = {
    novelAIEnabled: false,
    novelAIModel: "nai-diffusion-3",
    novelAISampler: "k_euler_ancestral",
    novelAISteps: 28,
    novelAICfgScale: 5,
    novelAISeed: -1,
    novelAIUcPreset: "Heavy",
    novelAICfgRescale: 0,
    novelAINoiseSchedule: "karras",
    novelAISm: false,
    novelAISmDyn: false,
    novelAIDynamicThresholding: false,
    novelAIVarietyPlus: false,
    novelAIAqtPreset: "safe",
    novelAIDivideRoles: false,
    novelAIUseAutoPositioning: false,
    novelAICharacterPrompts: [],
    // 质量词增强（quality_toggle）默认关闭：本项目已在前端按模型注入质量词
    // （applyNovelAIQualityTags），再让上游自己追加一遍就是双份质量词。
    // 需要交给上游注入时，在参数面板里手动打开。
    novelAIQualityToggle: false,
    // 附加原图（add_original_image）默认关闭：只有 img2img 才有意义，
    // 纯文生图开着它没用，而画布/工作台大多是文生图。
    novelAIAddOriginalImage: false,
};
