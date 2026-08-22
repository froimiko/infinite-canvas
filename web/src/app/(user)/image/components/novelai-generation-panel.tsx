"use client";

import { useState } from "react";
import { Button } from "antd";
import { Sparkles } from "lucide-react";

import { ModelPicker } from "@/components/model-picker";
import { NovelAIParamsPanel } from "@/components/novelai/novelai-params-panel";
import { NOVELAI_QUALITY_PRESET_OPTIONS, NOVELAI_UC_PRESET_OPTIONS, normalizeNovelAIQualityPreset, normalizeNovelAIUcPreset } from "@/components/novelai/novelai-presets";
import { PromptEditorDialog } from "@/components/prompt-editor-dialog";
import { canvasThemes } from "@/lib/canvas-theme";
import type { AiConfig } from "@/stores/use-config-store";
import { useNovelAIWorkbenchStore } from "@/stores/use-novelai-workbench-store";
import { useThemeStore } from "@/stores/use-theme-store";
import type { CanvasNodeMetadata } from "@/app/(user)/canvas/types";
import type { ReferenceImage } from "@/types/image";

import { NovelAICharacterPositionDialog } from "./novelai-character-position-dialog";
import { NovelAICharacterSection } from "./novelai-character-section";
import { ReferenceImageField } from "./reference-image-field";
import "./novelai-generation-panel.css";

type PromptField = "positive" | "negative";

const PROMPT_FIELDS: { value: PromptField; label: string }[] = [
    { value: "positive", label: "正面提示词" },
    { value: "negative", label: "负面提示词" },
];

type NovelAIGenerationPanelProps = {
    activeField: PromptField;
    onActiveFieldChange: (field: PromptField) => void;
    references: ReferenceImage[];
    onReferencesChange: (updater: (value: ReferenceImage[]) => ReferenceImage[]) => void;
    onPasteReferences: () => void;
    onUploadReferences: () => void;
    config: AiConfig;
    openConfigDialog: (shouldPromptContinue?: boolean) => void;
    running: boolean;
    canGenerate: boolean;
    onGenerate: () => void;
};

/**
 * NovelAI 生图面板。
 *
 * 三个要点：
 *  1. 提示词编辑器复用画布那套 PromptEditorDialog 的 inline 模式，
 *     token 化 / Tag 候选 / 一键翻译 全部共享，不另写一份。
 *  2. 正面/负面切换靠 key 强制 remount 编辑器 —— inline 模式不接受外部值回灌，
 *     换字段必须重建组件，否则会看到上一个字段的内容。
 *  3. 参数不走全局 config，全部存在 useNovelAIWorkbenchStore（尺寸/张数与通用生图隔离）。
 */
export function NovelAIGenerationPanel({ activeField, onActiveFieldChange, references, onReferencesChange, onPasteReferences, onUploadReferences, config, openConfigDialog, running, canGenerate, onGenerate }: NovelAIGenerationPanelProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const state = useNovelAIWorkbenchStore();
    const patch = useNovelAIWorkbenchStore((store) => store.patch);
    const [positionDialogOpen, setPositionDialogOpen] = useState(false);
    const [selectedCharacterId, setSelectedCharacterId] = useState<string | null>(null);

    const isPositive = activeField === "positive";
    const model = state.model || config.imageModel || config.model;

    // NovelAIParamsPanel 吃的是 CanvasNodeMetadata 形状，这里按字段名直接映射。
    // store 的字段名与 metadata 完全一致，所以不需要任何转换逻辑。
    const paramsMetadata: CanvasNodeMetadata = {
        model,
        novelAIModel: model,
        size: state.size,
        count: state.count,
        novelAISampler: state.novelAISampler,
        novelAISteps: state.novelAISteps,
        novelAICfgScale: state.novelAICfgScale,
        novelAICfgRescale: state.novelAICfgRescale,
        novelAINoiseSchedule: state.novelAINoiseSchedule,
        novelAISeed: state.novelAISeed,
        novelAIVarietyPlus: state.novelAIVarietyPlus,
        naSeedLocked: state.naSeedLocked,
        naQualityToggle: state.naQualityToggle,
        naAddOriginalImage: state.naAddOriginalImage,
    };

    return (
        <>
            <div className="mt-4 space-y-4">
                <div className="nai-field-switch">
                    {PROMPT_FIELDS.map((field) => (
                        <button key={field.value} type="button" className={`nai-field-switch__item ${activeField === field.value ? "is-active" : ""}`} onClick={() => onActiveFieldChange(field.value)}>
                            {field.label}
                        </button>
                    ))}
                </div>

                <PromptEditorDialog
                    // key 必须带字段：inline 模式不随 target 变化重置内部状态，
                    // 换字段只能靠 remount。去掉 key 会导致切到负面仍显示正面的内容。
                    key={activeField}
                    open
                    inline
                    target={isPositive ? { title: "正面提示词", value: state.positivePrompt, tokens: state.positiveTokens } : { title: "负面提示词", value: state.negativePrompt, tokens: state.negativeTokens }}
                    preset={
                        isPositive
                            ? {
                                  label: "质量词",
                                  value: state.naQualityPreset,
                                  options: NOVELAI_QUALITY_PRESET_OPTIONS,
                                  onChange: (value) => patch({ naQualityPreset: normalizeNovelAIQualityPreset(value) }),
                              }
                            : {
                                  label: "负面质量词",
                                  value: state.naUcPreset,
                                  options: NOVELAI_UC_PRESET_OPTIONS,
                                  onChange: (value) => patch({ naUcPreset: normalizeNovelAIUcPreset(value) }),
                              }
                    }
                    suggestionsEnabled={state.suggestionsEnabled}
                    actionsExtra={
                        <label className="pe-checkbox" title="关闭后输入时不再弹出 Tag 候选">
                            <input type="checkbox" checked={state.suggestionsEnabled} onChange={(event) => patch({ suggestionsEnabled: event.target.checked })} />
                            tag候选
                        </label>
                    }
                    onChange={(value, tokens) => (isPositive ? patch({ positivePrompt: value, positiveTokens: tokens }) : patch({ negativePrompt: value, negativeTokens: tokens }))}
                    // inline 模式没有「应用/取消」，这两个回调用不到，给空实现满足必填签名。
                    onSubmit={() => {}}
                    onClose={() => {}}
                />

                <ReferenceImageField references={references} onReferencesChange={onReferencesChange} onPaste={onPasteReferences} onUpload={onUploadReferences} hint={references.length ? "已启用图生图（img2img）" : "留空则为文生图"} />

                <div>
                    <div className="mb-2 text-base font-semibold">模型</div>
                    <ModelPicker config={config} value={model} capability="image" fullWidth onChange={(value) => patch({ model: value })} onMissingConfig={() => openConfigDialog(false)} />
                </div>

                <NovelAICharacterSection model={model} theme={theme} selectedCharacterId={selectedCharacterId} onSelectedCharacterIdChange={setSelectedCharacterId} onOpenPositionDialog={() => setPositionDialogOpen(true)} />

                <div className="nai-params">
                    <NovelAIParamsPanel
                        metadata={paramsMetadata}
                        theme={theme}
                        variant="inline"
                        onChange={(nextPatch) => {
                            // 面板给的是 CanvasNodeMetadata 补丁，逐字段落到 store。
                            // 只接受面板真正会发的字段，避免把 metadata 里的无关键写进 store。
                            patch({
                                ...(nextPatch.size !== undefined ? { size: String(nextPatch.size) } : {}),
                                ...(nextPatch.count !== undefined ? { count: Number(nextPatch.count) } : {}),
                                ...(nextPatch.novelAISampler !== undefined ? { novelAISampler: String(nextPatch.novelAISampler) } : {}),
                                ...(nextPatch.novelAISteps !== undefined ? { novelAISteps: Number(nextPatch.novelAISteps) } : {}),
                                ...(nextPatch.novelAICfgScale !== undefined ? { novelAICfgScale: Number(nextPatch.novelAICfgScale) } : {}),
                                ...(nextPatch.novelAICfgRescale !== undefined ? { novelAICfgRescale: Number(nextPatch.novelAICfgRescale) } : {}),
                                ...(nextPatch.novelAINoiseSchedule !== undefined ? { novelAINoiseSchedule: nextPatch.novelAINoiseSchedule } : {}),
                                ...(nextPatch.novelAISeed !== undefined ? { novelAISeed: Number(nextPatch.novelAISeed) } : {}),
                                ...(nextPatch.novelAIVarietyPlus !== undefined ? { novelAIVarietyPlus: Boolean(nextPatch.novelAIVarietyPlus) } : {}),
                                ...(nextPatch.naSeedLocked !== undefined ? { naSeedLocked: Boolean(nextPatch.naSeedLocked) } : {}),
                                ...(nextPatch.naQualityToggle !== undefined ? { naQualityToggle: Boolean(nextPatch.naQualityToggle) } : {}),
                                ...(nextPatch.naAddOriginalImage !== undefined ? { naAddOriginalImage: Boolean(nextPatch.naAddOriginalImage) } : {}),
                            });
                        }}
                    />
                </div>
            </div>

            <div className="mt-auto pt-6">
                <Button type="primary" size="large" block icon={<Sparkles className="size-4" />} loading={running} disabled={!canGenerate || running} onClick={onGenerate}>
                    开始生成
                </Button>
            </div>
            <NovelAICharacterPositionDialog open={positionDialogOpen} theme={theme} selectedCharacterId={selectedCharacterId} onSelectedCharacterIdChange={setSelectedCharacterId} onClose={() => setPositionDialogOpen(false)} />
        </>
    );
}

export type { PromptField as NovelAIPromptField };
