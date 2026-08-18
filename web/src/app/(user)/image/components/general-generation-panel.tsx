"use client";

import { ArrowLeft, ArrowRight, BookOpen, ClipboardPaste, FolderPlus, SlidersHorizontal, Sparkles, Trash2, Upload } from "lucide-react";
import { Button } from "antd";

import { imageReferenceLabel } from "@/lib/image-reference-prompt";
import { modelOptionLabel, type AiConfig } from "@/stores/use-config-store";
import type { ReferenceImage } from "@/types/image";

import { GenerationSettings, type UpdateAiConfig } from "./generation-settings";
import { PromptTranslationField } from "./prompt-translation-field";

type GeneralGenerationPanelProps = {
    prompt: string;
    onPromptChange: (value: string) => void;
    promptTranslation: string;
    onPromptTranslationChange: (value: string) => void;
    onSwapTranslation: () => void;
    references: ReferenceImage[];
    onReferencesChange: (updater: (value: ReferenceImage[]) => ReferenceImage[]) => void;
    onPasteReferences: () => void;
    onUploadReferences: () => void;
    config: AiConfig;
    model: string;
    updateConfig: UpdateAiConfig;
    openConfigDialog: (shouldPromptContinue?: boolean) => void;
    onOpenPromptLibrary: () => void;
    onOpenAssetPicker: () => void;
    onOpenSettingsDrawer: () => void;
    running: boolean;
    canGenerate: boolean;
    onGenerate: () => void;
};

/**
 * 通用生图表单。
 *
 * 与旧版工作台的三点差异（都是需求明确要求的，改动前请确认还需不需要）：
 *  1. 提示词是**纯文本框**，不用 PromptBlockEditor —— 通用模型不需要积木块与提示词候选。
 *  2. 没有负面提示词区块。原来的位置换成「一键翻译」译文区，译文只显示、不入请求。
 *  3. 图像设置不显示 NovelAI 高级参数（`showNovelAI={false}`），且生成时强制不发 NovelAI 参数。
 */
export function GeneralGenerationPanel({
    prompt,
    onPromptChange,
    promptTranslation,
    onPromptTranslationChange,
    onSwapTranslation,
    references,
    onReferencesChange,
    onPasteReferences,
    onUploadReferences,
    config,
    model,
    updateConfig,
    openConfigDialog,
    onOpenPromptLibrary,
    onOpenAssetPicker,
    onOpenSettingsDrawer,
    running,
    canGenerate,
    onGenerate,
}: GeneralGenerationPanelProps) {
    return (
        <>
            <div className="mt-6 space-y-5">
                <div>
                    <div className="mb-2 flex items-center justify-between gap-3">
                        <span className="text-base font-semibold">提示词</span>
                        <div className="flex gap-2">
                            <Button size="small" icon={<BookOpen className="size-3.5" />} onClick={onOpenPromptLibrary}>
                                查看提示词库
                            </Button>
                            <Button size="small" icon={<FolderPlus className="size-3.5" />} onClick={onOpenAssetPicker}>
                                查看我的素材
                            </Button>
                        </div>
                    </div>
                    <textarea
                        className="thin-scrollbar w-full resize-y rounded-md border border-stone-300 bg-transparent px-3 py-2 text-sm leading-6 outline-none transition placeholder:text-stone-400 focus:border-blue-500 dark:border-stone-700"
                        rows={8}
                        value={prompt}
                        placeholder="描述画面主体、风格、构图、光线和用途"
                        onChange={(event) => onPromptChange(event.target.value)}
                    />
                </div>

                <PromptTranslationField promptText={prompt} value={promptTranslation} onChange={onPromptTranslationChange} onSwap={onSwapTranslation} />

                <div className="min-w-0">
                    <div className="mb-2 flex items-center justify-between gap-3">
                        <span className="text-base font-semibold">参考图</span>
                        <div className="flex gap-2">
                            <Button size="small" icon={<ClipboardPaste className="size-3.5" />} onClick={onPasteReferences}>
                                剪切板
                            </Button>
                            <Button size="small" icon={<Upload className="size-3.5" />} onClick={onUploadReferences}>
                                上传
                            </Button>
                        </div>
                    </div>
                    <div
                        className="hover-scrollbar hover-scrollbar-hint flex min-h-24 w-full min-w-0 max-w-full gap-2 overflow-x-scroll overflow-y-hidden rounded-lg border border-dashed border-stone-300 p-2 pb-3 overscroll-x-contain dark:border-stone-700"
                        onWheel={(event) => {
                            if (event.currentTarget.scrollWidth <= event.currentTarget.clientWidth) return;
                            event.preventDefault();
                            event.currentTarget.scrollLeft += event.deltaY;
                        }}
                    >
                        {references.map((item, index) => (
                            <div key={item.id} className="group relative size-20 shrink-0 overflow-hidden rounded-md border border-stone-200 dark:border-stone-800">
                                <img src={item.dataUrl} alt={item.name} className="size-full object-cover" />
                                <span className="absolute left-1 top-1 rounded bg-black/60 px-1.5 py-0.5 text-[10px] font-medium text-white">{imageReferenceLabel(index)}</span>
                                <ReferenceOrderButtons index={index} total={references.length} onMove={(offset) => onReferencesChange((value) => moveListItem(value, index, offset))} />
                                <button
                                    type="button"
                                    className="absolute right-1 top-1 hidden size-6 items-center justify-center rounded bg-black/60 text-white group-hover:flex"
                                    onClick={() => onReferencesChange((value) => value.filter((ref) => ref.id !== item.id))}
                                    aria-label="移除参考图"
                                >
                                    <Trash2 className="size-3.5" />
                                </button>
                            </div>
                        ))}
                        {!references.length ? <div className="flex min-w-full items-center justify-center text-sm text-stone-500">暂无参考图</div> : null}
                    </div>
                </div>

                <div className="flex items-center justify-between rounded-lg border border-stone-200 bg-stone-50 px-3 py-2 text-sm dark:border-stone-800 dark:bg-stone-900 sm:hidden">
                    <span className="truncate text-stone-500 dark:text-stone-400">
                        {modelOptionLabel(config, model)} · {config.size} · {config.quality}
                    </span>
                    <Button size="small" type="text" icon={<SlidersHorizontal className="size-4" />} onClick={onOpenSettingsDrawer}>
                        调整
                    </Button>
                </div>

                <div className="hidden gap-4 sm:grid sm:grid-cols-2">
                    <GenerationSettings config={config} model={model} updateConfig={updateConfig} openConfigDialog={openConfigDialog} showNovelAI={false} />
                </div>
            </div>

            <div className="mt-auto pt-6">
                <Button type="primary" size="large" block icon={<Sparkles className="size-4" />} loading={running} disabled={!canGenerate || running} onClick={onGenerate}>
                    开始生成
                </Button>
            </div>
        </>
    );
}

function ReferenceOrderButtons({ index, total, onMove }: { index: number; total: number; onMove: (offset: number) => void }) {
    if (total <= 1) return null;
    return (
        <div className="absolute inset-x-1 bottom-1 flex justify-between">
            <Button size="small" className="!h-6 !w-6 !min-w-6 !rounded-full !bg-white/85 !p-0 !shadow-sm" icon={<ArrowLeft className="size-3" />} disabled={index <= 0} onClick={() => onMove(-1)} />
            <Button size="small" className="!h-6 !w-6 !min-w-6 !rounded-full !bg-white/85 !p-0 !shadow-sm" icon={<ArrowRight className="size-3" />} disabled={index >= total - 1} onClick={() => onMove(1)} />
        </div>
    );
}

function moveListItem<T>(items: T[], index: number, offset: number) {
    const targetIndex = index + offset;
    if (targetIndex < 0 || targetIndex >= items.length) return items;
    const next = [...items];
    [next[index], next[targetIndex]] = [next[targetIndex], next[index]];
    return next;
}
