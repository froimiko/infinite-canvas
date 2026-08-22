"use client";

import { useEffect, useRef, useState } from "react";
import type { CSSProperties, PointerEvent as ReactPointerEvent } from "react";
import { createPortal } from "react-dom";
import { Mars, Venus, X } from "lucide-react";

import { parseNovelAISize } from "@/components/novelai/novelai-resolutions";
import { CHARACTER_GENDER_COLORS, clampCharacterPosition, resolveEffectiveGender } from "@/components/novelai/character-position-layout";
import { resolveNovelAICharacterCenter } from "@/lib/novelai-config";
import type { CanvasTheme } from "@/lib/canvas-theme";
import { useNovelAIWorkbenchStore } from "@/stores/use-novelai-workbench-store";
import type { NovelAICharacterGender, NovelAICharacterPrompt } from "@/types/image";

import "./novelai-character-position-dialog.css";

type CharacterPosition = { x: number; y: number };

type NovelAICharacterPositionDialogProps = {
    open: boolean;
    theme: CanvasTheme;
    selectedCharacterId: string | null;
    onSelectedCharacterIdChange: (characterId: string | null) => void;
    onClose: () => void;
};

type DragState = {
    id: string;
    position: CharacterPosition;
    offset: CharacterPosition;
};

/**
 * NovelAI 角色位置浮动弹窗。
 *
 * 三个设计约束：
 *  1. 画布显示位置与请求构造共用 `center > 默认布局` 的优先级；不能只在 UI 里维护另一套坐标，否则拖完后界面和实际出图会分叉。
 *  2. 拖动帧只写组件本地 state，松手才通过 workbench store 的 patch 写入 center；store 会 persist 到 localStorage，高频写盘会让拖拽卡顿。
 *  3. 与 Flutter 参考实现唯一有意的视觉简化是没有预览底图：工作台当前没有可用的预览图，这里使用纯色空底和边框承载同样的坐标交互。
 */
export function NovelAICharacterPositionDialog({ open, theme, selectedCharacterId, onSelectedCharacterIdChange, onClose }: NovelAICharacterPositionDialogProps) {
    const characters = useNovelAIWorkbenchStore((state) => state.characters);
    const charactersAiChoice = useNovelAIWorkbenchStore((state) => state.charactersAiChoice);
    const size = useNovelAIWorkbenchStore((state) => state.size);
    const patch = useNovelAIWorkbenchStore((state) => state.patch);
    const [draftPositions, setDraftPositions] = useState<Record<string, CharacterPosition>>({});
    const dragRef = useRef<DragState | null>(null);

    useEffect(() => {
        if (!open) return;
        const handleKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape") onClose();
        };
        document.addEventListener("keydown", handleKeyDown);
        return () => document.removeEventListener("keydown", handleKeyDown);
    }, [onClose, open]);

    useEffect(() => {
        const validIds = new Set(characters.map((character, index) => getCharacterId(character, index)));
        setDraftPositions((current) => Object.fromEntries(Object.entries(current).filter(([id]) => validIds.has(id))));
        if (selectedCharacterId && !validIds.has(selectedCharacterId)) onSelectedCharacterIdChange(null);
    }, [characters, onSelectedCharacterIdChange, selectedCharacterId]);

    if (!open || typeof document === "undefined") return null;

    const parsedSize = parseNovelAISize(size);
    // 兜底坐标必须走 novelai-config 的 resolveNovelAICharacterCenter —— 请求载荷用的是同一个函数。
    // 这里若自己按 characters.length 算兜底，禁用/空占位角色会占掉编号，
    // 就会出现「画布上分散显示、后端收到的却是另一套坐标」的分叉。
    const getPosition = (character: NovelAICharacterPrompt, index: number) => draftPositions[getCharacterId(character, index)] || resolveNovelAICharacterCenter(character, characters);

    const startDrag = (character: NovelAICharacterPrompt, index: number, event: ReactPointerEvent<HTMLButtonElement>) => {
        if (charactersAiChoice) return;
        const canvas = event.currentTarget.closest(".nai-position-dialog__canvas");
        if (!(canvas instanceof HTMLElement)) return;
        const rect = canvas.getBoundingClientRect();
        const id = getCharacterId(character, index);
        const position = getPosition(character, index);
        const pointerPosition = clampCharacterPosition({ x: (event.clientX - rect.left) / rect.width, y: (event.clientY - rect.top) / rect.height });
        dragRef.current = { id, position, offset: { x: pointerPosition.x - position.x, y: pointerPosition.y - position.y } };
        setDraftPositions((current) => ({ ...current, [id]: position }));
        onSelectedCharacterIdChange(id);
        event.currentTarget.setPointerCapture(event.pointerId);
    };

    const moveDrag = (event: ReactPointerEvent<HTMLButtonElement>) => {
        const drag = dragRef.current;
        if (!drag) return;
        const canvas = event.currentTarget.closest(".nai-position-dialog__canvas");
        if (!(canvas instanceof HTMLElement)) return;
        const rect = canvas.getBoundingClientRect();
        const position = clampCharacterPosition({ x: (event.clientX - rect.left) / rect.width - drag.offset.x, y: (event.clientY - rect.top) / rect.height - drag.offset.y });
        dragRef.current = { ...drag, position };
        setDraftPositions((current) => ({ ...current, [drag.id]: position }));
    };

    const finishDrag = (event: ReactPointerEvent<HTMLButtonElement>) => {
        const drag = dragRef.current;
        if (!drag) return;
        if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
        dragRef.current = null;
        setDraftPositions((current) => {
            const next = { ...current };
            delete next[drag.id];
            return next;
        });
        patch({ characters: characters.map((character, index) => (getCharacterId(character, index) === drag.id ? { ...character, center: clampCharacterPosition(drag.position) } : character)) });
    };

    const rootStyle = {
        "--nai-position-panel": theme.node.panel,
        "--nai-position-fill": theme.node.fill,
        "--nai-position-border": theme.node.stroke,
        "--nai-position-text": theme.node.text,
        "--nai-position-muted": theme.node.muted,
        "--nai-position-accent": theme.node.activeStroke,
    } as CSSProperties;

    const dialog = (
        <div className="nai-position-dialog__backdrop" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
            <section className="nai-position-dialog" style={rootStyle} role="dialog" aria-modal="true" aria-labelledby="nai-position-dialog-title" onMouseDown={(event) => event.stopPropagation()}>
                <header className="nai-position-dialog__header">
                    <h2 id="nai-position-dialog-title" className="nai-position-dialog__title">
                        ✥ 角色位置
                    </h2>
                    <button type="button" className="nai-position-dialog__close" aria-label="关闭角色位置" onClick={onClose}>
                        <X className="size-4" />
                    </button>
                </header>
                <div className="nai-position-dialog__chips" role="tablist" aria-label="选择角色">
                    {characters.map((character, index) => {
                        const id = getCharacterId(character, index);
                        const display = resolveCharacterChipDisplay(character, index);
                        const Icon = display.gender === "female" ? Venus : display.gender === "male" ? Mars : null;
                        return (
                            <button key={id} type="button" role="tab" aria-selected={selectedCharacterId === id} className={`nai-position-dialog__chip ${selectedCharacterId === id ? "is-selected" : ""}`} onClick={() => onSelectedCharacterIdChange(id)}>
                                <span>{index + 1}</span>
                                {Icon ? <Icon className="size-3.5" /> : null}
                                <span className="nai-position-dialog__chip-label">{display.label}</span>
                            </button>
                        );
                    })}
                </div>
                <div className="nai-position-dialog__canvas-wrap">
                    <div className="nai-position-dialog__canvas" style={{ aspectRatio: `${parsedSize.width} / ${parsedSize.height}` }}>
                        {charactersAiChoice ? (
                            <div className="nai-position-dialog__ai-hint">AI 选择模式下，NovelAI 会自动安排角色位置</div>
                        ) : (
                            characters.map((character, index) => {
                                const id = getCharacterId(character, index);
                                const position = getPosition(character, index);
                                const isSelected = selectedCharacterId === id;
                                const isDragging = dragRef.current?.id === id;
                                const diameter = isDragging ? 40 : isSelected ? 36 : 32;
                                const gender = resolveEffectiveGender(character.characterPrompt);
                                return (
                                    <button
                                        key={id}
                                        type="button"
                                        className={`nai-position-dialog__anchor ${isSelected ? "is-selected" : ""} ${isDragging ? "is-dragging" : ""}`}
                                        style={{ left: `${position.x * 100}%`, top: `${position.y * 100}%`, width: diameter, height: diameter, backgroundColor: CHARACTER_GENDER_COLORS[gender], opacity: character.enabled === false ? 0.45 : 1 }}
                                        aria-label={`移动角色 ${index + 1}`}
                                        onPointerDown={(event) => startDrag(character, index, event)}
                                        onPointerMove={moveDrag}
                                        onPointerUp={finishDrag}
                                        onPointerCancel={finishDrag}
                                    >
                                        <span>{index + 1}</span>
                                    </button>
                                );
                            })
                        )}
                        {dragRef.current ? (
                            <div className="nai-position-dialog__drag-label" style={{ left: `${dragRef.current.position.x * 100}%`, top: `${dragRef.current.position.y * 100}%` }}>
                                {Math.round(dragRef.current.position.x * 100)}% , {Math.round(dragRef.current.position.y * 100)}%
                            </div>
                        ) : null}
                    </div>
                </div>
                <p className="nai-position-dialog__hint">{charactersAiChoice ? "AI 将根据角色提示词自动选择构图位置" : "拖动锚点设置角色位置，松开即生效"}</p>
            </section>
        </div>
    );

    return createPortal(dialog, document.body);
}

function getCharacterId(character: NovelAICharacterPrompt, index: number) {
    return character.id || `character-${index}`;
}

function resolveCharacterChipDisplay(character: NovelAICharacterPrompt, index: number): { gender: NovelAICharacterGender; label: string } {
    const tags = character.characterPrompt
        .split(",")
        .map((tag) => tag.trim())
        .filter(Boolean);
    const gender = resolveEffectiveGender(character.characterPrompt);
    const firstTag = tags[0]?.toLowerCase();
    const isGenderTag = firstTag === "girl" || firstTag === "1girl" || firstTag === "boy" || firstTag === "1boy";
    const promptLabel = isGenderTag ? tags[1] : tags[0];
    const hasCustomName = !/^Character \d+$/.test(character.displayName.trim());
    return { gender, label: (hasCustomName ? character.displayName.trim() : promptLabel) || `角色 ${index + 1}` };
}
