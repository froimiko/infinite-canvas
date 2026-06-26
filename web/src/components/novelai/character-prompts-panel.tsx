"use client";

import { Plus, Trash2 } from "lucide-react";

import { TagAutocomplete } from "@/components/tag-autocomplete";
import type { CanvasTheme } from "@/lib/canvas-theme";
import type { NovelAICharacterPrompt } from "@/types/image";

const MAX_CHARACTERS = 6;

type CharacterPromptsPanelProps = {
    value: NovelAICharacterPrompt[];
    useAutoPositioning: boolean;
    onChange: (value: NovelAICharacterPrompt[]) => void;
    theme: CanvasTheme;
};

export function CharacterPromptsPanel({ value, useAutoPositioning, onChange, theme }: CharacterPromptsPanelProps) {
    const addCharacter = () => {
        if (value.length >= MAX_CHARACTERS) return;
        onChange([...value, { displayName: `角色${value.length + 1}`, characterPrompt: "", characterNegativePrompt: "", coords: { x: 2, y: 2 } }]);
    };
    const updateCharacter = <K extends keyof NovelAICharacterPrompt>(index: number, key: K, nextValue: NovelAICharacterPrompt[K]) => {
        const next = [...value];
        next[index] = { ...next[index], [key]: nextValue };
        onChange(next);
    };
    const updateCoord = (index: number, axis: "x" | "y", nextValue: number) => {
        const coords = value[index]?.coords || { x: 2, y: 2 };
        updateCharacter(index, "coords", { ...coords, [axis]: Math.max(0, Math.min(4, Math.floor(nextValue || 0))) });
    };

    return (
        <div className="space-y-2.5">
            <div className="flex items-center justify-between gap-2">
                <span className="text-xs font-medium" style={{ color: theme.node.muted }}>
                    角色提示词（{value.length}/{MAX_CHARACTERS}）
                </span>
                <button type="button" className="inline-flex h-7 items-center gap-1 rounded-full border px-2 text-xs" style={{ borderColor: theme.node.stroke, color: theme.node.text }} disabled={value.length >= MAX_CHARACTERS} onClick={addCharacter}>
                    <Plus className="size-3" /> 添加
                </button>
            </div>
            {!value.length ? <div className="rounded-xl border border-dashed px-3 py-4 text-center text-xs" style={{ borderColor: theme.node.stroke, color: theme.node.muted }}>开启角色分离后可添加最多 6 个角色</div> : null}
            {value.map((character, index) => (
                <div key={index} className="space-y-2 rounded-xl border p-2.5" style={{ borderColor: theme.node.stroke, background: theme.node.fill }}>
                    <div className="flex items-center gap-2">
                        <input value={character.displayName} maxLength={20} onChange={(event) => updateCharacter(index, "displayName", event.target.value)} className="min-w-0 flex-1 rounded-lg border bg-transparent px-2 py-1 text-xs outline-none" style={{ borderColor: theme.node.stroke, color: theme.node.text }} placeholder={`角色${index + 1}`} />
                        <button type="button" className="rounded-md p-1 text-red-400 hover:bg-red-500/10" onClick={() => onChange(value.filter((_, itemIndex) => itemIndex !== index))} title="删除角色">
                            <Trash2 className="size-3.5" />
                        </button>
                    </div>
                    <TagAutocomplete value={character.characterPrompt} onChange={(nextValue) => updateCharacter(index, "characterPrompt", nextValue)} rows={2} className="w-full resize-none rounded-lg border bg-transparent px-2 py-1.5 text-xs outline-none" style={{ borderColor: theme.node.stroke, color: theme.node.text }} placeholder="角色外观、服装、姿态..." />
                    <TagAutocomplete value={character.characterNegativePrompt || ""} onChange={(nextValue) => updateCharacter(index, "characterNegativePrompt", nextValue)} rows={1} className="w-full resize-none rounded-lg border bg-transparent px-2 py-1.5 text-xs outline-none" style={{ borderColor: theme.node.stroke, color: theme.node.text }} placeholder="角色负面提示词（可选）" />
                    {!useAutoPositioning ? (
                        <div className="flex items-center gap-2 text-xs" style={{ color: theme.node.muted }}>
                            <span>位置</span>
                            <label className="flex items-center gap-1">X <input type="number" min={0} max={4} value={character.coords?.x ?? 2} onChange={(event) => updateCoord(index, "x", Number(event.target.value))} className="w-12 rounded border bg-transparent px-1 py-0.5 text-center outline-none" style={{ borderColor: theme.node.stroke, color: theme.node.text }} /></label>
                            <label className="flex items-center gap-1">Y <input type="number" min={0} max={4} value={character.coords?.y ?? 2} onChange={(event) => updateCoord(index, "y", Number(event.target.value))} className="w-12 rounded border bg-transparent px-1 py-0.5 text-center outline-none" style={{ borderColor: theme.node.stroke, color: theme.node.text }} /></label>
                            <span>0=左/上，4=右/下</span>
                        </div>
                    ) : null}
                </div>
            ))}
        </div>
    );
}
