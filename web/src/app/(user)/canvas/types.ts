import type { PromptBlockToken } from "@/components/prompt-block-editor/prompt-block-types";
import type { NovelAIQualityPreset, NovelAIUcPreset } from "@/components/novelai/novelai-presets";
import type { NovelAISettings } from "@/types/image";

export type Position = {
    x: number;
    y: number;
};

export type ViewportTransform = {
    x: number;
    y: number;
    k: number;
};

export enum CanvasNodeType {
    Image = "image",
    Text = "text",
    Config = "config",
    Video = "video",
    Audio = "audio",
    NovelAI = "novelai",
}

export type CanvasNodeStatus = "idle" | "success" | "loading" | "error";
export type CanvasGenerationMode = "text" | "image" | "video" | "audio";
export type CanvasImageGenerationType = "generation" | "edit";

export type CanvasNodeMetadata = Partial<NovelAISettings> & {
    content?: string;
    /**
     * 文本节点的译文（一键翻译结果）。
     *
     * 只用于在节点里展示与「↑↓」和正文互换，**绝不参与任何对外传输**：
     * 生成链路（buildNodeGenerationContext / sourceTextContent）、@ 资源引用
     * （canvas-resource-references）、AI 助手画布快照 一律只读 content。
     * 新增读取点前请先确认是否会把译文带出节点。
     */
    textTranslation?: string;
    composerContent?: string;
    prompt?: string;
    promptTokens?: PromptBlockToken[];
    naPositivePrompt?: string;
    naNegativePrompt?: string;
    naPromptTokens?: PromptBlockToken[];
    naNegativePromptTokens?: PromptBlockToken[];
    naSeedLocked?: boolean;
    naQualityPreset?: NovelAIQualityPreset;
    naUcPreset?: NovelAIUcPreset;
    naQualityToggle?: boolean;
    naAddOriginalImage?: boolean;
    status?: CanvasNodeStatus;
    errorDetails?: string;
    fontSize?: number;
    generationMode?: CanvasGenerationMode;
    generationType?: CanvasImageGenerationType;
    model?: string;
    size?: string;
    quality?: string;
    count?: number;
    seconds?: string;
    vquality?: string;
    generateAudio?: string;
    watermark?: string;
    audioVoice?: string;
    audioFormat?: string;
    audioSpeed?: string;
    audioInstructions?: string;
    references?: string[];
    naturalWidth?: number;
    naturalHeight?: number;
    freeResize?: boolean;
    isBatchRoot?: boolean;
    batchRootId?: string;
    batchChildIds?: string[];
    batchUsesReferenceImages?: boolean;
    primaryImageId?: string;
    imageBatchExpanded?: boolean;
    storageKey?: string;
    mimeType?: string;
    bytes?: number;
    durationMs?: number;
};

export type CanvasNodeData = {
    id: string;
    type: CanvasNodeType;
    title: string;
    position: Position;
    width: number;
    height: number;
    metadata?: CanvasNodeMetadata;
};

export type CanvasConnection = {
    id: string;
    fromNodeId: string;
    toNodeId: string;
};

export type CanvasAssistantReference = {
    id: string;
    type: CanvasNodeType;
    title: string;
    dataUrl?: string;
    storageKey?: string;
    text?: string;
};

export type CanvasAssistantImage = {
    id: string;
    dataUrl: string;
    storageKey?: string;
    prompt: string;
};

export type CanvasAssistantMessage = {
    id: string;
    role: "user" | "assistant" | "system" | "tool" | "error";
    title?: string;
    text: string;
    meta?: string;
    detail?: unknown;
    references?: CanvasAssistantReference[];
};

export type CanvasAssistantSession = {
    id: string;
    title: string;
    messages: CanvasAssistantMessage[];
    createdAt: string;
    updatedAt: string;
};

export type ConnectionHandle = {
    nodeId: string;
    handleType: "source" | "target";
};

export type SelectionBox = {
    startWorldX: number;
    startWorldY: number;
    currentWorldX: number;
    currentWorldY: number;
    additive: boolean;
    initialSelectedNodeIds: string[];
};

export type ContextMenuState =
    | {
          type: "node";
          x: number;
          y: number;
          nodeId: string;
      }
    | {
          type: "connection";
          x: number;
          y: number;
          connectionId: string;
      };
