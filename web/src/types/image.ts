export type ReferenceImage = {
    id: string;
    name: string;
    type: string;
    dataUrl: string;
    url?: string;
    storageKey?: string;
};

export type NovelAIUCPreset = "Heavy" | "Light" | "None" | "Human Focus";
export type NovelAINoiseSchedule = "native" | "karras" | "exponential" | "polyexponential";
export type NovelAIAqtPreset = "safe" | "nai" | "full" | "balanced" | "anime" | "furry" | "pony";

export type NovelAICharacterPrompt = {
    displayName: string;
    characterPrompt: string;
    characterNegativePrompt?: string;
    coords?: { x: number; y: number };
};

export type NovelAISettings = {
    novelAIEnabled: boolean;
    novelAIModel: string;
    novelAISampler: string;
    novelAISteps: number;
    novelAICfgScale: number;
    novelAISeed: number;
    novelAIUcPreset: NovelAIUCPreset;
    novelAICfgRescale: number;
    novelAINoiseSchedule: NovelAINoiseSchedule;
    novelAISm: boolean;
    novelAISmDyn: boolean;
    novelAIDynamicThresholding: boolean;
    novelAIVarietyPlus: boolean;
    novelAIAqtPreset: NovelAIAqtPreset;
    novelAIDivideRoles: boolean;
    novelAIUseAutoPositioning: boolean;
    novelAICharacterPrompts: NovelAICharacterPrompt[];
};
