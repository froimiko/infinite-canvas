import type { PromptBlockToken } from "@/components/prompt-block-editor/prompt-block-types";

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

/** 角色性别。只用于「添加角色」时决定初始提示词与默认配色，不参与请求。 */
export type NovelAICharacterGender = "female" | "male" | "other";

export type NovelAICharacterPrompt = {
    displayName: string;
    characterPrompt: string;
    characterPromptTokens?: PromptBlockToken[];
    characterNegativePrompt?: string;
    characterNegativePromptTokens?: PromptBlockToken[];
    /**
     * 历史的 5x5 网格坐标（整数 0-4），画布 NovelAI 节点的角色面板仍在用它
     * （character-prompts-panel.tsx 是两个数字输入框）。后端 mapNovelAIGridCoord
     * 会把它量化成 0.0/0.1/0.3/0.5/0.7 —— 注意最大只到 0.7，画不到右下角。
     */
    coords?: { x: number; y: number };
    /**
     * 连续坐标（0-1 浮点），对齐参考实现 nai_image_request_builder.dart:194-206 的直传语义。
     *
     * 为什么要和 coords 并存而不是替换：
     *  - 工作台的位置画布是「拖动锚点，松手即生效」，网格量化会变成吸附跳格，
     *    而且 4 档上限 0.7 到不了边缘，构图与参考项目对不上，所以必须走连续坐标；
     *  - 画布节点那套 0-4 网格 UI 和存量画布数据还在跑，直接改语义会让老节点的位置漂移。
     * 后端优先级固定为 center > coords，两边各用各的，互不影响。
     */
    center?: { x: number; y: number };
    /** 稳定标识（nanoid），用于 React key 与选中态；纯前端字段，不发给后端。 */
    id?: string;
    /** 添加时选的性别。UI 配色请用 resolveEffectiveGender 按首个 tag 实时推导，别读这里。 */
    gender?: NovelAICharacterGender;
    /** 是否参与出图。关掉的角色留在列表里但不进请求，缺省视为 true。 */
    enabled?: boolean;
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
    novelAIQualityToggle: boolean;
    novelAIAddOriginalImage: boolean;
};
