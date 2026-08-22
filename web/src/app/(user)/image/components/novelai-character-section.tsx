"use client";

import { useState } from "react";
import type { CSSProperties, MouseEvent, ReactNode } from "react";
import { App, theme as antdTheme } from "antd";
import { ArrowDown, ArrowUp, CheckCircle2, Circle, Crosshair, ListX, Mars, SquarePlus, Transgender, Trash2, Users, Venus } from "lucide-react";

import { CHARACTER_GENDER_COLORS, characterPositionForIndex, createCharacterPrompt, resolveCharacterLimit, resolveEffectiveGender, supportsMultiCharacter } from "@/components/novelai/character-position-layout";
import { PromptEditorDialog } from "@/components/prompt-editor-dialog";
import type { CanvasTheme } from "@/lib/canvas-theme";
import { useNovelAIWorkbenchStore } from "@/stores/use-novelai-workbench-store";
import type { NovelAICharacterGender, NovelAICharacterPrompt } from "@/types/image";

import "./novelai-character-section.css";

type CharacterPromptField = "positive" | "negative";

type NovelAICharacterSectionProps = {
    model: string;
    theme: CanvasTheme;
    selectedCharacterId: string | null;
    onSelectedCharacterIdChange: (characterId: string | null) => void;
    onOpenPositionDialog: () => void;
};

const CHARACTER_PROMPT_FIELDS: { value: CharacterPromptField; label: string }[] = [
    { value: "positive", label: "正向提示词" },
    { value: "negative", label: "负向提示词" },
];

const GENDER_BUTTONS: { gender: NovelAICharacterGender; label: string; icon: typeof Venus }[] = [
    { gender: "female", label: "女", icon: Venus },
    { gender: "male", label: "男", icon: Mars },
    { gender: "other", label: "其他", icon: Transgender },
];

/**
 * NovelAI 多角色区块。
 *
 * 三个设计约束：
 *  1. 角色数据全部落在 workbench store，但当前选中卡属于 UI 瞬态，刷新后不恢复，也不触发 persist 高频写盘。
 *  2. 卡内 PromptEditorDialog 的 key 必须同时带角色 id 与字段名；inline 模式历史上只读取一次 target，
 *     切角色或正负字段若不 remount，会把上一块编辑器的内部内容串进来。
 *  3. 位置模式只在这里提供入口和全局 AI/自定义开关，连续坐标画布由阶段 3 的外部弹窗承载。
 */
export function NovelAICharacterSection({ model, theme, selectedCharacterId, onSelectedCharacterIdChange, onOpenPositionDialog }: NovelAICharacterSectionProps) {
    const { modal } = App.useApp();
    // danger 色跟着 antd token 走，避免在亮色算法下手写红过曝、暗色下发闷。
    const dangerColor = antdTheme.useToken().token.colorError;
    const characters = useNovelAIWorkbenchStore((state) => state.characters);
    const charactersAiChoice = useNovelAIWorkbenchStore((state) => state.charactersAiChoice);
    const suggestionsEnabled = useNovelAIWorkbenchStore((state) => state.suggestionsEnabled);
    const patch = useNovelAIWorkbenchStore((state) => state.patch);
    // 选中态提升到了 novelai-generation-panel：位置弹窗的芯片条要与角色卡编辑态共用同一个 id，
    // 内部再持一份 useState 会变成两个互不同步的选中源。父组件持有的是普通 UI 瞬态，不进 persist。
    const setSelectedCharacterId = onSelectedCharacterIdChange;
    const [activePromptField, setActivePromptField] = useState<CharacterPromptField>("positive");

    if (!supportsMultiCharacter(model)) return null;

    const characterLimit = resolveCharacterLimit(model);
    const hasCharacters = characters.length > 0;
    const reachedLimit = characters.length >= characterLimit;
    const limitTitle = reachedLimit ? `已达当前模型上限（${characterLimit}）` : undefined;

    const updateCharacter = (index: number, partial: Partial<NovelAICharacterPrompt>) => {
        patch({ characters: characters.map((character, itemIndex) => (itemIndex === index ? { ...character, ...partial } : character)) });
    };

    const toggleEditing = (characterId: string) => {
        if (selectedCharacterId === characterId) {
            setSelectedCharacterId(null);
            return;
        }
        setActivePromptField("positive");
        setSelectedCharacterId(characterId);
    };

    const stopHeaderToggle = (event: MouseEvent<HTMLElement>) => event.stopPropagation();

    const moveCharacter = (index: number, offset: -1 | 1) => {
        const targetIndex = index + offset;
        if (targetIndex < 0 || targetIndex >= characters.length) return;
        const next = [...characters];
        [next[index], next[targetIndex]] = [next[targetIndex], next[index]];
        patch({ characters: next });
    };

    const deleteCharacter = (index: number, characterId: string) => {
        // 参考实现刻意直接删除单张卡；只有「清空全部」才需要二次确认。
        patch({ characters: characters.filter((_, itemIndex) => itemIndex !== index) });
        if (selectedCharacterId === characterId) setSelectedCharacterId(null);
    };

    const addCharacter = (gender: NovelAICharacterGender) => {
        if (reachedLimit) return;
        const newIndex = characters.length;
        const newTotal = newIndex + 1;
        const character = createCharacterPrompt(gender, newIndex, characterPositionForIndex(newIndex, newTotal));
        patch({ characters: [...characters, character] });
        // 新建后直接进入编辑，和参考项目的 _selectLast 一样省掉一次额外点击。
        setActivePromptField("positive");
        setSelectedCharacterId(character.id || null);
    };

    const clearAllCharacters = () => {
        modal.confirm({
            title: "清空全部角色？",
            content: `将删除当前的 ${characters.length} 个角色，此操作无法撤销。`,
            okText: "清空全部",
            cancelText: "取消",
            okButtonProps: { danger: true },
            onOk: () => {
                patch({ characters: [] });
                setSelectedCharacterId(null);
            },
        });
    };

    return (
        <section
            className="nai-character-section"
            style={
                {
                    "--nai-character-panel": theme.node.panel,
                    "--nai-character-fill": theme.node.fill,
                    "--nai-character-border": theme.node.stroke,
                    "--nai-character-text": theme.node.text,
                    "--nai-character-muted": theme.node.muted,
                    "--nai-character-faint": theme.node.faint,
                    "--nai-character-accent": theme.node.activeStroke,
                    "--nai-character-toolbar-hover": theme.toolbar.itemHover,
                    "--nai-character-danger": dangerColor,
                } as CSSProperties
            }
        >
            <div className="nai-character-section__header">
                <div className={`nai-character-section__title ${hasCharacters ? "is-populated" : ""}`}>
                    <Users className="size-4" />
                    <span>角色</span>
                    {hasCharacters ? <span className="nai-character-section__count">{characters.length}</span> : null}
                </div>

                {hasCharacters ? (
                    <div className="nai-character-section__header-actions">
                        <div className="nai-character-mode" aria-label="角色位置模式">
                            <button type="button" className={`nai-character-mode__segment nai-character-mode__segment--first ${charactersAiChoice ? "is-active" : ""}`} onClick={() => patch({ charactersAiChoice: true })}>
                                AI 选择
                            </button>
                            <button type="button" className={`nai-character-mode__segment ${!charactersAiChoice ? "is-active" : ""}`} onClick={() => patch({ charactersAiChoice: false })}>
                                自定义
                            </button>
                            <button type="button" className="nai-character-mode__segment nai-character-mode__segment--icon nai-character-mode__segment--last" title="编辑角色位置" aria-label="编辑角色位置" onClick={onOpenPositionDialog}>
                                <Crosshair className="size-3.5" />
                            </button>
                        </div>
                        <button type="button" className="nai-character-section__clear" title="清空全部角色" aria-label="清空全部角色" onClick={clearAllCharacters}>
                            <ListX className="size-4" />
                        </button>
                    </div>
                ) : null}
            </div>

            <div className="nai-character-section__list">
                {characters.map((character, index) => {
                    const characterId = character.id || `character-${index}`;
                    const isSelected = selectedCharacterId === characterId;
                    const isEnabled = character.enabled !== false;
                    const effectiveGender = resolveEffectiveGender(character.characterPrompt);
                    const genderColor = CHARACTER_GENDER_COLORS[effectiveGender];
                    const promptValue = activePromptField === "positive" ? character.characterPrompt : character.characterNegativePrompt || "";
                    const promptTokens = activePromptField === "positive" ? character.characterPromptTokens : character.characterNegativePromptTokens;

                    return (
                        <article key={characterId} className={`nai-character-card ${isSelected ? "is-selected" : ""} ${isEnabled ? "" : "is-disabled"}`}>
                            <div
                                className="nai-character-card__header"
                                role="button"
                                tabIndex={0}
                                onClick={() => toggleEditing(characterId)}
                                onKeyDown={(event) => {
                                    if (event.key === "Enter" || event.key === " ") {
                                        event.preventDefault();
                                        toggleEditing(characterId);
                                    }
                                }}
                            >
                                <span className="nai-character-card__gender-dot" style={{ backgroundColor: genderColor }} />
                                <div className="nai-character-card__name-wrap">
                                    {isSelected ? (
                                        <input
                                            className="nai-character-card__name-input"
                                            value={character.displayName}
                                            maxLength={20}
                                            aria-label={`角色 ${index + 1} 名称`}
                                            onClick={stopHeaderToggle}
                                            onChange={(event) => updateCharacter(index, { displayName: event.target.value.slice(0, 20) })}
                                            onKeyDown={(event) => {
                                                event.stopPropagation();
                                                if (event.key === "Enter") event.currentTarget.blur();
                                            }}
                                        />
                                    ) : (
                                        <span className="nai-character-card__name">{character.displayName}</span>
                                    )}
                                </div>

                                <div className="nai-character-card__actions" onClick={stopHeaderToggle}>
                                    <IconButton label="上移" disabled={index === 0} onClick={() => moveCharacter(index, -1)}>
                                        <ArrowUp className="size-3.5" />
                                    </IconButton>
                                    <IconButton label="下移" disabled={index === characters.length - 1} onClick={() => moveCharacter(index, 1)}>
                                        <ArrowDown className="size-3.5" />
                                    </IconButton>
                                    <IconButton label="加入词库（暂未接入）" disabled>
                                        <SquarePlus className="size-3.5" />
                                    </IconButton>
                                    <IconButton label={isEnabled ? "禁用角色" : "启用角色"} active={isEnabled} onClick={() => updateCharacter(index, { enabled: !isEnabled })}>
                                        {isEnabled ? <CheckCircle2 className="size-3.5" /> : <Circle className="size-3.5" />}
                                    </IconButton>
                                    <IconButton label="删除角色" danger onClick={() => deleteCharacter(index, characterId)}>
                                        <Trash2 className="size-3.5" />
                                    </IconButton>
                                </div>
                            </div>

                            {isSelected ? (
                                <div className="nai-character-card__editor">
                                    <div className="nai-character-card__tabs">
                                        {CHARACTER_PROMPT_FIELDS.map((field) => (
                                            <button key={field.value} type="button" className={`nai-character-card__tab ${activePromptField === field.value ? "is-active" : ""}`} onClick={() => setActivePromptField(field.value)}>
                                                {field.label}
                                            </button>
                                        ))}
                                    </div>
                                    <PromptEditorDialog
                                        // inline 编辑器只在首次挂载时读取 target；角色 id 与字段名缺一都会发生内容串线。
                                        key={`${characterId}-${activePromptField}`}
                                        open
                                        inline
                                        target={{
                                            title: activePromptField === "positive" ? "正向提示词" : "负向提示词",
                                            value: promptValue,
                                            tokens: promptTokens,
                                        }}
                                        suggestionsEnabled={suggestionsEnabled}
                                        actionsExtra={
                                            <label className="pe-checkbox" title="关闭后输入时不再弹出 Tag 候选">
                                                <input type="checkbox" checked={suggestionsEnabled} onChange={(event) => patch({ suggestionsEnabled: event.target.checked })} />
                                                tag候选
                                            </label>
                                        }
                                        onChange={(value, tokens) =>
                                            activePromptField === "positive"
                                                ? updateCharacter(index, { characterPrompt: value, characterPromptTokens: tokens })
                                                : updateCharacter(index, { characterNegativePrompt: value, characterNegativePromptTokens: tokens })
                                        }
                                        onSubmit={() => {}}
                                        onClose={() => {}}
                                    />
                                </div>
                            ) : (
                                <button type="button" className={`nai-character-card__preview ${character.characterPrompt.trim() ? "" : "is-empty"}`} onClick={() => toggleEditing(characterId)}>
                                    {character.characterPrompt.trim() || "输入角色提示词，描述外观、服装与动作"}
                                </button>
                            )}
                        </article>
                    );
                })}
            </div>

            <div className="nai-character-section__add-row">
                {GENDER_BUTTONS.map(({ gender, label, icon: GenderIcon }) => (
                    <button key={gender} type="button" className="nai-character-add" style={{ "--nai-character-gender": CHARACTER_GENDER_COLORS[gender] } as CSSProperties} disabled={reachedLimit} title={limitTitle} onClick={() => addCharacter(gender)}>
                        <span className="nai-character-add__plus">+</span>
                        <GenderIcon className="size-4" />
                        <span>{label}</span>
                    </button>
                ))}
                <button type="button" className="nai-character-add nai-character-add--library" disabled title="暂未接入">
                    <SquarePlus className="size-4" />
                    <span>词库</span>
                </button>
            </div>
        </section>
    );
}

type IconButtonProps = {
    label: string;
    disabled?: boolean;
    active?: boolean;
    danger?: boolean;
    onClick?: () => void;
    children: ReactNode;
};

function IconButton({ label, disabled = false, active = false, danger = false, onClick, children }: IconButtonProps) {
    return (
        <button type="button" className={`nai-character-card__icon-button ${active ? "is-active" : ""} ${danger ? "is-danger" : ""}`} title={label} aria-label={label} disabled={disabled} onClick={onClick}>
            {children}
        </button>
    );
}
