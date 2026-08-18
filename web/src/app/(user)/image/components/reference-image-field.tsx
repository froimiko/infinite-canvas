"use client";

import { ArrowLeft, ArrowRight, ClipboardPaste, Trash2, Upload } from "lucide-react";
import { Button } from "antd";

import { imageReferenceLabel } from "@/lib/image-reference-prompt";
import type { ReferenceImage } from "@/types/image";

type ReferenceImageFieldProps = {
    references: ReferenceImage[];
    onReferencesChange: (updater: (value: ReferenceImage[]) => ReferenceImage[]) => void;
    onPaste: () => void;
    onUpload: () => void;
    title?: string;
    hint?: string;
};

/**
 * 参考图区。通用生图与 NovelAI 生图共用，不要各写一份。
 *
 * NovelAI 侧的语义：有参考图 → 走 requestEdit（img2img），
 * 此时高级参数里的「附加原图（img2img）」开关才真正生效。
 */
export function ReferenceImageField({ references, onReferencesChange, onPaste, onUpload, title = "参考图", hint }: ReferenceImageFieldProps) {
    return (
        <div className="min-w-0">
            <div className="mb-2 flex items-center justify-between gap-3">
                <div className="flex min-w-0 items-center gap-2">
                    <span className="text-base font-semibold">{title}</span>
                    {hint ? <span className="truncate text-xs text-stone-500 dark:text-stone-400">{hint}</span> : null}
                </div>
                <div className="flex shrink-0 gap-2">
                    <Button size="small" icon={<ClipboardPaste className="size-3.5" />} onClick={onPaste}>
                        剪切板
                    </Button>
                    <Button size="small" icon={<Upload className="size-3.5" />} onClick={onUpload}>
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
