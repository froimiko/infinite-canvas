import { DEFAULT_NOVELAI_SETTINGS } from "@/components/novelai/novelai-constants";
import { CanvasNodeType } from "./types";
import type { CanvasNodeMetadata } from "./types";

type CanvasNodeSpec = {
    width: number;
    height: number;
    title: string;
    metadata?: CanvasNodeMetadata;
};

export const NODE_DEFAULT_SIZE = {
    [CanvasNodeType.Image]: { width: 340, height: 240, title: "New Generation" },
    [CanvasNodeType.Text]: { width: 340, height: 240, title: "Note" },
    [CanvasNodeType.Config]: { width: 340, height: 240, title: "生成配置" },
    [CanvasNodeType.Video]: { width: 420, height: 236, title: "Video" },
    [CanvasNodeType.Audio]: { width: 340, height: 120, title: "Audio" },
    [CanvasNodeType.NovelAI]: { width: 380, height: 360, title: "NovelAI" },
} satisfies Record<CanvasNodeType, { width: number; height: number; title: string }>;

export const NODE_SPECS = {
    [CanvasNodeType.Image]: {
        ...NODE_DEFAULT_SIZE[CanvasNodeType.Image],
        metadata: { content: "", status: "idle" },
    },
    [CanvasNodeType.Text]: {
        ...NODE_DEFAULT_SIZE[CanvasNodeType.Text],
        metadata: { content: "", status: "idle", fontSize: 14 },
    },
    [CanvasNodeType.Config]: {
        ...NODE_DEFAULT_SIZE[CanvasNodeType.Config],
        metadata: { content: "", status: "idle", generationMode: "image" },
    },
    [CanvasNodeType.Video]: {
        ...NODE_DEFAULT_SIZE[CanvasNodeType.Video],
        metadata: { content: "", status: "idle" },
    },
    [CanvasNodeType.Audio]: {
        ...NODE_DEFAULT_SIZE[CanvasNodeType.Audio],
        metadata: { content: "", status: "idle" },
    },
    [CanvasNodeType.NovelAI]: {
        ...NODE_DEFAULT_SIZE[CanvasNodeType.NovelAI],
        // novelAIModel 留空，模型由 ModelPicker 从用户配置里选，避免预设名不在可选列表中。
        metadata: { ...DEFAULT_NOVELAI_SETTINGS, novelAIModel: "", novelAIEnabled: true, status: "idle", generationMode: "image", size: "832x1216", count: 1, naPositivePrompt: "", naNegativePrompt: "" },
    },
} satisfies Record<CanvasNodeType, CanvasNodeSpec>;

export function getNodeSpec(type: CanvasNodeType) {
    return NODE_SPECS[type];
}
