"use client";

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { LoaderCircle, Play, Settings2, SlidersHorizontal, Square } from "lucide-react";

import { ModelPicker } from "@/components/model-picker";
import { PromptEditorDialog, type PromptEditorTarget } from "@/components/prompt-editor-dialog";
import { canvasThemes } from "@/lib/canvas-theme";
import { useConfigStore, useEffectiveConfig } from "@/stores/use-config-store";
import { useThemeStore } from "@/stores/use-theme-store";
import { NovelAIParamsPanel } from "./novelai-params-panel";
import { NOVELAI_PORTAL_DROPDOWN_ATTR } from "./novelai-resolution-select";
import { NOVELAI_QUALITY_PRESET_OPTIONS, NOVELAI_UC_PRESET_OPTIONS, normalizeNovelAIQualityPreset, normalizeNovelAIUcPreset } from "./novelai-presets";
import type { CanvasNodeData, CanvasNodeMetadata } from "@/app/(user)/canvas/types";

const PANEL_WIDTH = 300;
const PANEL_GAP = 8;
const SCREEN_MARGIN = 12;

type NovelAINodePanelProps = {
    node: CanvasNodeData;
    isRunning: boolean;
    onConfigChange: (nodeId: string, patch: Partial<CanvasNodeMetadata>) => void;
    onGenerate: (nodeId: string) => void;
    onStop: (nodeId: string) => void;
};

type EditorState = { field: "positive" | "negative"; target: PromptEditorTarget } | null;

export function NovelAINodePanel({ node, isRunning, onConfigChange, onGenerate, onStop }: NovelAINodePanelProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const globalConfig = useEffectiveConfig();
    const openConfigDialog = useConfigStore((state) => state.openConfigDialog);
    const metadata = node.metadata || {};
    const [paramsOpen, setParamsOpen] = useState(false);
    const [paramsRect, setParamsRect] = useState<DOMRect | null>(null);
    const [editor, setEditor] = useState<EditorState>(null);
    const paramsButtonRef = useRef<HTMLButtonElement>(null);
    const paramsPanelRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        if (!paramsOpen) return;
        const syncPosition = () => setParamsRect(paramsButtonRef.current?.getBoundingClientRect() || null);
        const closeOnOutside = (event: PointerEvent) => {
            const target = event.target;
            if (!(target instanceof Node)) return;
            if (paramsButtonRef.current?.contains(target) || paramsPanelRef.current?.contains(target)) return;
            // 面板内的自定义下拉（如图像尺寸）被 portal 到 body，不在 paramsPanelRef 子树里。
            // 这里必须放行，否则 pointerdown 阶段就把面板关掉、下拉随之卸载，
            // click 永远不会触发 —— 表现为"选了尺寸没反应，面板还自己关了"。
            const element = target instanceof Element ? target : target.parentElement;
            if (element?.closest(`[${NOVELAI_PORTAL_DROPDOWN_ATTR}]`)) return;
            setParamsOpen(false);
        };
        syncPosition();
        window.addEventListener("resize", syncPosition);
        window.addEventListener("scroll", syncPosition, true);
        window.addEventListener("pointerdown", closeOnOutside, true);
        return () => {
            window.removeEventListener("resize", syncPosition);
            window.removeEventListener("scroll", syncPosition, true);
            window.removeEventListener("pointerdown", closeOnOutside, true);
        };
    }, [paramsOpen]);

    const fieldStyle = { background: theme.node.fill, borderColor: theme.node.stroke, color: theme.node.text };

    return (
        <div className="flex h-full w-full cursor-move flex-col gap-2 px-3 pb-3 pt-7 text-sm" style={{ color: theme.node.text }} onWheel={(event) => event.stopPropagation()}>
            <div className="flex items-center justify-between gap-2">
                <span className="text-sm font-semibold">NovelAI</span>
                <span className="text-[11px]" style={{ color: theme.node.muted }}>
                    {metadata.size || "832x1216"}
                </span>
            </div>

            <label className="flex min-h-0 flex-1 flex-col gap-1">
                <span className="text-[11px]" style={{ color: theme.node.muted }}>
                    正面提示词
                </span>
                <textarea
                    className="thin-scrollbar min-h-0 flex-1 cursor-text resize-none rounded-lg border px-2 py-1.5 text-xs outline-none"
                    style={fieldStyle}
                    value={metadata.naPositivePrompt || ""}
                    placeholder="描述画面主体、风格、构图"
                    onMouseDown={(event) => event.stopPropagation()}
                    onChange={(event) => onConfigChange(node.id, { naPositivePrompt: event.target.value })}
                />
            </label>

            <label className="flex min-h-0 flex-1 flex-col gap-1">
                <span className="text-[11px]" style={{ color: theme.node.muted }}>
                    负面提示词
                </span>
                <textarea
                    className="thin-scrollbar min-h-0 flex-1 cursor-text resize-none rounded-lg border px-2 py-1.5 text-xs outline-none"
                    style={fieldStyle}
                    value={metadata.naNegativePrompt || ""}
                    placeholder="留空使用默认负面提示词"
                    onMouseDown={(event) => event.stopPropagation()}
                    onChange={(event) => onConfigChange(node.id, { naNegativePrompt: event.target.value })}
                />
            </label>

            <div className="flex min-w-0 items-center gap-2" onMouseDown={(event) => event.stopPropagation()}>
                <button
                    type="button"
                    className="flex h-8 min-w-0 flex-1 cursor-pointer items-center justify-center gap-1.5 rounded-lg border px-2 text-xs"
                    style={fieldStyle}
                    onClick={() => setEditor({ field: "positive", target: { title: "正面提示词", value: metadata.naPositivePrompt || "", tokens: metadata.naPromptTokens } })}
                >
                    <SlidersHorizontal className="size-3.5 shrink-0" />
                    <span className="truncate">正面提示词编辑器</span>
                </button>
                <button
                    type="button"
                    className="flex h-8 min-w-0 flex-1 cursor-pointer items-center justify-center gap-1.5 rounded-lg border px-2 text-xs"
                    style={fieldStyle}
                    onClick={() => setEditor({ field: "negative", target: { title: "负面提示词", value: metadata.naNegativePrompt || "", tokens: metadata.naNegativePromptTokens } })}
                >
                    <SlidersHorizontal className="size-3.5 shrink-0" />
                    <span className="truncate">负面提示词编辑器</span>
                </button>
            </div>

            <div className="flex min-w-0 items-center gap-2" onMouseDown={(event) => event.stopPropagation()}>
                <ModelPicker
                    className="canvas-compact-control h-9 min-w-0 flex-1"
                    config={globalConfig}
                    value={metadata.model || globalConfig.imageModel}
                    capability="image"
                    onChange={(model) => onConfigChange(node.id, { novelAIModel: model, model })}
                    onMissingConfig={() => openConfigDialog(true)}
                    fullWidth
                />
                <button ref={paramsButtonRef} type="button" className="inline-flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-lg border px-2.5 text-xs" style={fieldStyle} onClick={() => setParamsOpen((current) => !current)}>
                    <Settings2 className="size-3.5" />
                    高级参数
                </button>
                <button
                    type="button"
                    className="inline-flex h-9 shrink-0 cursor-pointer items-center gap-1.5 rounded-lg px-3 text-xs font-medium"
                    style={{ background: isRunning ? "#dc2626" : theme.node.activeStroke, color: theme.node.panel }}
                    onClick={() => (isRunning ? onStop(node.id) : onGenerate(node.id))}
                >
                    {isRunning ? (
                        <>
                            <LoaderCircle className="size-3.5 animate-spin" />
                            <Square className="size-3 fill-current" />
                        </>
                    ) : (
                        <>
                            <Play className="size-3.5" />
                            生成
                        </>
                    )}
                </button>
            </div>

            {paramsOpen && paramsRect
                ? createPortal(
                      <div ref={paramsPanelRef} style={paramsPanelStyle(paramsRect)}>
                          <NovelAIParamsPanel metadata={metadata} theme={theme} fallbackModel={globalConfig.imageModel || globalConfig.model} onChange={(patch) => onConfigChange(node.id, patch)} />
                      </div>,
                      document.body,
                  )
                : null}

            {editor ? (
                <PromptEditorDialog
                    open
                    target={editor.target}
                    preset={
                        editor.field === "positive"
                            ? {
                                  label: "质量词",
                                  value: normalizeNovelAIQualityPreset(metadata.naQualityPreset),
                                  options: NOVELAI_QUALITY_PRESET_OPTIONS,
                                  onChange: (value) => onConfigChange(node.id, { naQualityPreset: normalizeNovelAIQualityPreset(value) }),
                              }
                            : {
                                  label: "负面质量词",
                                  value: normalizeNovelAIUcPreset(metadata.naUcPreset),
                                  options: NOVELAI_UC_PRESET_OPTIONS,
                                  onChange: (value) => onConfigChange(node.id, { naUcPreset: normalizeNovelAIUcPreset(value) }),
                              }
                    }
                    onClose={() => setEditor(null)}
                    onSubmit={(value, tokens) => onConfigChange(node.id, editor.field === "positive" ? { naPositivePrompt: value, naPromptTokens: tokens } : { naNegativePrompt: value, naNegativePromptTokens: tokens })}
                />
            ) : null}
        </div>
    );
}

/** 依据按钮位置选择上/下展开方向，并把可视高度限制在剩余空间内，避免超出屏幕。 */
function paramsPanelStyle(rect: DOMRect) {
    const spaceBelow = window.innerHeight - rect.bottom - PANEL_GAP - SCREEN_MARGIN;
    const spaceAbove = rect.top - PANEL_GAP - SCREEN_MARGIN;
    const placeAbove = spaceBelow < 320 && spaceAbove > spaceBelow;
    const left = Math.max(SCREEN_MARGIN, Math.min(window.innerWidth - PANEL_WIDTH - SCREEN_MARGIN, rect.left));
    return {
        position: "fixed",
        zIndex: 1300,
        left,
        maxHeight: Math.max(240, placeAbove ? spaceAbove : spaceBelow),
        overflowY: "auto",
        borderRadius: 14,
        ...(placeAbove ? { bottom: window.innerHeight - rect.top + PANEL_GAP } : { top: rect.bottom + PANEL_GAP }),
    } as const;
}
