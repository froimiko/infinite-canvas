"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, DragEvent, KeyboardEvent, PointerEvent as ReactPointerEvent, ReactNode } from "react";
import { createPortal } from "react-dom";
import { Eraser, Trash2, X } from "lucide-react";

import { createPromptBlockToken, normalizePromptBlockTokens, parsePromptToTokens, serializeTokensToPrompt } from "@/components/prompt-block-editor/prompt-block-utils";
import type { PromptBlockToken } from "@/components/prompt-block-editor/prompt-block-types";
import { searchTags, type TagSearchResult } from "@/services/tag-service";
import { networkTranslatePromptText } from "@/services/api/prompt-tags";
import { useUserStore } from "@/stores/use-user-store";
import { getCurrentWord, measureCaretPosition, replaceCurrentWord, type CurrentWord } from "./prompt-editor-utils";
import { TranslateIcon } from "./translate-icon";
import "./prompt-editor-dialog.css";

const SEARCH_DEBOUNCE_MS = 160;
const MAX_SUGGESTIONS = 12;
/** 单击进入编辑前的等待时间，用于让双击（禁用）先取消它。 */
const CLICK_EDIT_DELAY_MS = 180;
const MIN_WIDTH = 520;
const MIN_HEIGHT = 400;
const RESIZE_CORNERS = ["nw", "ne", "sw", "se"] as const;
/** 与后端一致的单次批量上限，超出会拆成多批。 */
const TRANSLATE_BATCH_SIZE = 50;

/** 只翻译缺译名、译名等于原文或译名仍是英文的普通 Tag。 */
function needsTranslation(token: PromptBlockToken) {
    if (token.kind === "newline" || token.kind === "lora" || token.kind === "mention") return false;
    const text = token.text.trim();
    if (!text) return false;
    const translation = token.translation?.trim();
    return !translation || translation === text || /[A-Za-z]/.test(translation);
}

type ResizeCorner = (typeof RESIZE_CORNERS)[number];

export type PromptEditorTarget = {
    title: string;
    value: string;
    tokens?: PromptBlockToken[];
};

export type PromptEditorPresetOption = { value: string; label: string };

export type PromptEditorPreset = {
    label: string;
    value: string;
    options: PromptEditorPresetOption[];
    onChange: (value: string) => void;
};

type PromptEditorDialogProps = {
    open: boolean;
    title?: string;
    target: PromptEditorTarget;
    preset?: PromptEditorPreset;
    onSubmit: (value: string, tokens: PromptBlockToken[]) => void;
    onClose: () => void;
    /**
     * 内联模式：不 portal、不 fixed、无标题栏/底部按钮/缩放角，直接嵌进页面。
     *
     * 与弹窗模式的关键差异：
     *  - 弹窗靠「应用」按钮一次性 onSubmit；内联没有应用按钮，靠 onChange 实时上报。
     *  - 内联下 target 只作为初始值读一次（见 initializedRef），绝不随外部变化重置，
     *    否则父组件把 onChange 的值回灌进 target 就会造成光标跳、token 抖动。
     *    需要切换字段（正面/负面）时，请在父组件用 key 强制 remount。
     */
    inline?: boolean;
    /** 内联模式下的实时上报。弹窗模式不使用。 */
    onChange?: (value: string, tokens: PromptBlockToken[]) => void;
    /** 是否启用 Tag 候选下拉。false 时不发搜索请求也不渲染候选层。 */
    suggestionsEnabled?: boolean;
    /** 追加到操作条尾部的自定义控件（如「tag候选」勾选框）。 */
    actionsExtra?: ReactNode;
};

type DragState = { x: number; y: number };
type ResizeState = { corner: ResizeCorner; startX: number; startY: number; startWidth: number; startHeight: number; startLeft: number; startTop: number };

export function PromptEditorDialog({ open, title = "NovelAI 提示词编辑器", target, preset, onSubmit, onClose, inline = false, onChange, suggestionsEnabled = true, actionsExtra }: PromptEditorDialogProps) {
    const [tokens, setTokens] = useState<PromptBlockToken[]>([]);
    const [draft, setDraft] = useState("");
    const [suggestions, setSuggestions] = useState<TagSearchResult[]>([]);
    const [showSuggestions, setShowSuggestions] = useState(false);
    const [selectedIndex, setSelectedIndex] = useState(0);
    const [menuStyle, setMenuStyle] = useState<CSSProperties>({ left: 0, top: 0 });
    const [showDeleteButtons, setShowDeleteButtons] = useState(true);
    const [position, setPosition] = useState({ x: 0, y: 0 });
    const [size, setSize] = useState({ width: 960, height: 560 });
    const [dragging, setDragging] = useState(false);
    const [resizing, setResizing] = useState(false);
    const [dragIndex, setDragIndex] = useState<number | null>(null);
    const [editingTokenId, setEditingTokenId] = useState<string | null>(null);
    const [editValue, setEditValue] = useState("");
    const [translatingTokenIds, setTranslatingTokenIds] = useState<string[]>([]);
    const [isTranslatingAll, setIsTranslatingAll] = useState(false);
    const [translateError, setTranslateError] = useState("");
    const authToken = useUserStore((state) => state.token);
    const translateRequestRef = useRef(0);
    const textareaRef = useRef<HTMLTextAreaElement>(null);
    const wrapRef = useRef<HTMLDivElement>(null);
    const dragOffsetRef = useRef<DragState | null>(null);
    const resizeRef = useRef<ResizeState | null>(null);
    const currentWordRef = useRef<CurrentWord>({ query: "", replaceStart: 0, replaceEnd: 0 });
    const searchIdRef = useRef(0);
    const editTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    // 内联模式的初始化守卫：只在首次挂载时读 target，之后一律不再重置内部状态。
    const initializedRef = useRef(false);
    // 上报门闩必须是 state 而不是 ref：初始化 effect 里的 setTokens 要到下一次渲染才生效，
    // 若用 ref 判断，上报 effect 会在首帧带着空 tokens 先跑一次，把父组件的值冲成空串。
    const [inlineReady, setInlineReady] = useState(false);
    // onChange 放进 ref，避免父组件每次渲染换新函数时触发初始化 effect。
    const onChangeRef = useRef(onChange);
    onChangeRef.current = onChange;

    const tokenCount = useMemo(() => tokens.filter((token) => !token.disabled && token.kind !== "newline").length, [tokens]);

    useEffect(() => {
        if (!open) return;
        // 内联模式下 target 只是初始值：外部值变化不能重置内部状态，
        // 否则父组件回灌 onChange 的结果会让光标跳到末尾、token 闪烁。
        if (inline && initializedRef.current) return;
        initializedRef.current = true;
        const initial = target.tokens?.length ? normalizePromptBlockTokens(target.tokens) : normalizePromptBlockTokens(parsePromptToTokens(target.value || ""));
        setTokens(initial);
        setDraft(serializeTokensToPrompt(initial));
        setShowSuggestions(false);
        setEditingTokenId(null);
        if (inline) {
            setInlineReady(true);
            return;
        }
        const width = Math.min(960, window.innerWidth - 48);
        const height = Math.min(560, window.innerHeight - 48);
        setSize({ width, height });
        setPosition({ x: Math.max(24, (window.innerWidth - width) / 2), y: Math.max(24, (window.innerHeight - height) / 2) });
    }, [inline, open, target.tokens, target.value]);

    useEffect(() => {
        return () => {
            if (editTimerRef.current) clearTimeout(editTimerRef.current);
        };
    }, []);

    // 内联模式：token 任何变化都实时上报。
    // 用 effect 统一上报而不是在每个修改点手动调 onChange —— 打字、插入候选、拖拽排序、
    // 双击禁用、清空、翻译回填全都会走 tokens，逐点调用必然漏掉某条路径。
    useEffect(() => {
        if (!inline || !inlineReady) return;
        onChangeRef.current?.(serializeTokensToPrompt(tokens), tokens);
    }, [inline, inlineReady, tokens]);

    const syncCaretMenu = useCallback(() => {
        const textarea = textareaRef.current;
        const wrap = wrapRef.current;
        if (!textarea || !wrap) return;
        setMenuStyle(measureCaretPosition(textarea, wrap));
    }, []);

    useEffect(() => {
        if (!open) return;
        if (!suggestionsEnabled) {
            setShowSuggestions(false);
            return;
        }
        const keyword = currentWordRef.current.query.trim();
        if (!keyword) {
            setShowSuggestions(false);
            return;
        }
        const requestId = searchIdRef.current + 1;
        searchIdRef.current = requestId;
        const timeout = window.setTimeout(() => {
            void searchTags(keyword, MAX_SUGGESTIONS)
                .then((results) => {
                    if (searchIdRef.current !== requestId) return;
                    setSuggestions(results);
                    setSelectedIndex(0);
                    setShowSuggestions(results.length > 0);
                })
                .catch(() => setShowSuggestions(false));
        }, SEARCH_DEBOUNCE_MS);
        return () => window.clearTimeout(timeout);
    }, [draft, open, suggestionsEnabled]);

    useEffect(() => {
        if (!dragging) return;
        const move = (event: PointerEvent) => {
            const offset = dragOffsetRef.current;
            if (!offset) return;
            setPosition({ x: event.clientX - offset.x, y: event.clientY - offset.y });
        };
        const stop = () => {
            dragOffsetRef.current = null;
            setDragging(false);
        };
        window.addEventListener("pointermove", move);
        window.addEventListener("pointerup", stop);
        return () => {
            window.removeEventListener("pointermove", move);
            window.removeEventListener("pointerup", stop);
        };
    }, [dragging]);

    useEffect(() => {
        if (!resizing) return;
        const move = (event: PointerEvent) => {
            const state = resizeRef.current;
            if (!state) return;
            const deltaX = event.clientX - state.startX;
            const deltaY = event.clientY - state.startY;
            const pullLeft = state.corner === "nw" || state.corner === "sw";
            const pullTop = state.corner === "nw" || state.corner === "ne";
            const width = Math.max(MIN_WIDTH, state.startWidth + (pullLeft ? -deltaX : deltaX));
            const height = Math.max(MIN_HEIGHT, state.startHeight + (pullTop ? -deltaY : deltaY));
            setSize({ width, height });
            setPosition({
                x: pullLeft ? state.startLeft + (state.startWidth - width) : state.startLeft,
                y: pullTop ? state.startTop + (state.startHeight - height) : state.startTop,
            });
        };
        const stop = () => {
            resizeRef.current = null;
            setResizing(false);
        };
        window.addEventListener("pointermove", move);
        window.addEventListener("pointerup", stop);
        return () => {
            window.removeEventListener("pointermove", move);
            window.removeEventListener("pointerup", stop);
        };
    }, [resizing]);

    if (!open || typeof document === "undefined") return null;

    const commitTokens = (next: PromptBlockToken[]) => {
        const normalized = normalizePromptBlockTokens(next);
        setTokens(normalized);
        setDraft(serializeTokensToPrompt(normalized));
    };

    /** 按 token id 回填译名，源文本被改过的 Tag 会被跳过，避免异步结果错位。 */
    const applyTranslations = (results: Map<string, { text: string; translation: string }>) => {
        setTokens((current) => {
            const next = current.map((item) => {
                const result = results.get(item.id);
                return result && result.text === item.text ? createPromptBlockToken(item.text, { ...item, translation: result.translation }) : item;
            });
            return normalizePromptBlockTokens(next);
        });
    };

    const translateToken = async (target: PromptBlockToken) => {
        if (!authToken || translatingTokenIds.includes(target.id)) return;
        const requestId = (translateRequestRef.current += 1);
        setTranslatingTokenIds((current) => [...current, target.id]);
        setTranslateError("");
        try {
            const translation = await networkTranslatePromptText(target.text, authToken);
            if (translateRequestRef.current !== requestId) return;
            if (translation.trim()) applyTranslations(new Map([[target.id, { text: target.text, translation: translation.trim() }]]));
        } catch (error) {
            setTranslateError(error instanceof Error ? error.message : "翻译失败");
        } finally {
            setTranslatingTokenIds((current) => current.filter((id) => id !== target.id));
        }
    };

    const translateAllTokens = async () => {
        if (!authToken || isTranslatingAll) return;
        const pending = tokens.filter(needsTranslation);
        if (!pending.length) return;
        const requestId = (translateRequestRef.current += 1);
        setIsTranslatingAll(true);
        setTranslateError("");
        const results = new Map<string, { text: string; translation: string }>();
        let lastError = "";
        try {
            for (let start = 0; start < pending.length; start += TRANSLATE_BATCH_SIZE) {
                const batch = pending.slice(start, start + TRANSLATE_BATCH_SIZE);
                let lines: string[] = [];
                try {
                    lines = (await networkTranslatePromptText(batch.map((item) => item.text).join("\n"), authToken)).split("\n");
                } catch (error) {
                    lastError = error instanceof Error ? error.message : "翻译失败";
                }
                if (translateRequestRef.current !== requestId) return;
                if (lines.length === batch.length) {
                    batch.forEach((item, index) => {
                        const translation = lines[index]?.trim();
                        if (translation) results.set(item.id, { text: item.text, translation });
                    });
                    continue;
                }
                // 行数对不上或整批失败时逐条重试，已成功的结果不受影响。
                for (const item of batch) {
                    try {
                        const translation = (await networkTranslatePromptText(item.text, authToken)).trim();
                        if (translateRequestRef.current !== requestId) return;
                        if (translation) results.set(item.id, { text: item.text, translation });
                    } catch (error) {
                        lastError = error instanceof Error ? error.message : "翻译失败";
                    }
                }
            }
            if (results.size) applyTranslations(results);
            if (lastError) setTranslateError(results.size ? `${lastError}（部分 Tag 未翻译）` : lastError);
        } finally {
            if (translateRequestRef.current === requestId) setIsTranslatingAll(false);
        }
    };

    const handleDraftChange = (value: string, caret: number) => {
        setDraft(value);
        currentWordRef.current = getCurrentWord(value, caret);
        setTokens(normalizePromptBlockTokens(parsePromptToTokens(value, tokens)));
        window.requestAnimationFrame(syncCaretMenu);
    };

    const insertSuggestion = (suggestion: TagSearchResult) => {
        const word = currentWordRef.current;
        const { value, caret } = replaceCurrentWord(draft, word, suggestion.name);
        const nextTokens = parsePromptToTokens(value, tokens).map((token) =>
            token.text === suggestion.name && !token.translation ? createPromptBlockToken(token.text, { ...token, kind: "tag", translation: suggestion.zhName, category: suggestion.category }) : token,
        );
        setDraft(value);
        commitTokens(nextTokens);
        setShowSuggestions(false);
        currentWordRef.current = getCurrentWord(value, caret);
        window.setTimeout(() => {
            textareaRef.current?.focus();
            textareaRef.current?.setSelectionRange(caret, caret);
        }, 0);
    };

    const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
        event.stopPropagation();
        if (showSuggestions && suggestions.length) {
            if (event.key === "ArrowDown") {
                event.preventDefault();
                setSelectedIndex((index) => (index + 1) % suggestions.length);
                return;
            }
            if (event.key === "ArrowUp") {
                event.preventDefault();
                setSelectedIndex((index) => (index - 1 + suggestions.length) % suggestions.length);
                return;
            }
            if (event.key === "Enter" || event.key === "Tab") {
                event.preventDefault();
                insertSuggestion(suggestions[Math.min(selectedIndex, suggestions.length - 1)]);
                return;
            }
            if (event.key === "Escape") {
                event.preventDefault();
                setShowSuggestions(false);
            }
        }
    };

    const handleDrop = (index: number, event: DragEvent<HTMLDivElement>) => {
        event.preventDefault();
        if (dragIndex === null || dragIndex === index) {
            setDragIndex(null);
            return;
        }
        const rect = event.currentTarget.getBoundingClientRect();
        const insertAfter = event.clientX > rect.left + rect.width / 2;
        let targetIndex = index + (insertAfter ? 1 : 0);
        if (dragIndex < targetIndex) targetIndex -= 1;
        const next = [...tokens];
        const [dragged] = next.splice(dragIndex, 1);
        next.splice(Math.max(0, Math.min(targetIndex, next.length)), 0, dragged);
        commitTokens(next);
        setDragIndex(null);
    };

    /** 单击 tag 文本延迟进入编辑，双击会先清掉定时器改为切换禁用。 */
    const scheduleEditToken = (token: PromptBlockToken) => {
        if (token.kind === "newline") return;
        if (editTimerRef.current) clearTimeout(editTimerRef.current);
        editTimerRef.current = setTimeout(() => {
            setEditingTokenId(token.id);
            setEditValue(token.text);
        }, CLICK_EDIT_DELAY_MS);
    };

    const toggleTokenDisabled = (index: number) => {
        if (editTimerRef.current) clearTimeout(editTimerRef.current);
        setEditingTokenId(null);
        commitTokens(tokens.map((item, itemIndex) => (itemIndex === index ? { ...item, disabled: !item.disabled } : item)));
    };

    const finishEditToken = () => {
        if (!editingTokenId) return;
        const text = editValue.trim();
        const next = text ? tokens.map((token) => (token.id === editingTokenId && token.text.trim() !== text ? createPromptBlockToken(text, { id: token.id, disabled: token.disabled }) : token)) : tokens.filter((token) => token.id !== editingTokenId);
        setEditingTokenId(null);
        setEditValue("");
        commitTokens(next);
    };

    const startWindowDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
        dragOffsetRef.current = { x: event.clientX - position.x, y: event.clientY - position.y };
        setDragging(true);
    };

    const startResize = (corner: ResizeCorner, event: ReactPointerEvent<HTMLDivElement>) => {
        event.stopPropagation();
        resizeRef.current = { corner, startX: event.clientX, startY: event.clientY, startWidth: size.width, startHeight: size.height, startLeft: position.x, startTop: position.y };
        setResizing(true);
    };

    const dialog = (
        <div
            className={inline ? "pe-dialog pe-dialog--inline" : "pe-dialog"}
            style={inline ? undefined : { left: position.x, top: position.y, width: size.width, height: size.height }}
            onMouseDown={(event) => event.stopPropagation()}
            onPointerDown={(event) => event.stopPropagation()}
            onWheel={(event) => event.stopPropagation()}
        >
            {inline ? null : (
                <div className="pe-dialog__header" onPointerDown={startWindowDrag}>
                    <span className="pe-dialog__title">
                        {title} · {target.title}
                    </span>
                    <button type="button" className="pe-dialog__close" onClick={onClose} aria-label="关闭">
                        <X className="size-4" />
                    </button>
                </div>
            )}

            <div ref={wrapRef} className="pe-dialog__editor">
                <textarea
                    ref={textareaRef}
                    className="pe-dialog__textarea"
                    value={draft}
                    placeholder="输入提示词，使用逗号分隔"
                    onChange={(event) => handleDraftChange(event.target.value, event.target.selectionStart)}
                    onKeyDown={handleKeyDown}
                    onClick={(event) => {
                        currentWordRef.current = getCurrentWord(event.currentTarget.value, event.currentTarget.selectionStart);
                        syncCaretMenu();
                    }}
                    onBlur={() => commitTokens(parsePromptToTokens(draft, tokens))}
                />
                <span className="pe-dialog__counter">{tokenCount} tokens</span>
                {showSuggestions ? (
                    <div className="pe-suggestions" style={menuStyle} onMouseDown={(event) => event.preventDefault()}>
                        {suggestions.map((suggestion, index) => (
                            <button key={`${suggestion.name}-${index}`} type="button" className={`pe-suggestion ${index === selectedIndex ? "is-selected" : ""}`} onMouseEnter={() => setSelectedIndex(index)} onClick={() => insertSuggestion(suggestion)}>
                                <span className="pe-suggestion__text">{suggestion.name}</span>
                                <span className="pe-suggestion__desc">{suggestion.zhName}</span>
                            </button>
                        ))}
                    </div>
                ) : null}
            </div>

            <div className="pe-dialog__actions">
                <button type="button" className="pe-button" onClick={() => commitTokens(tokens.filter((token) => !token.disabled))}>
                    <Trash2 className="size-3.5" /> 一键清空禁用
                </button>
                <button type="button" className="pe-button" onClick={() => commitTokens([])}>
                    <Eraser className="size-3.5" /> 一键清空所有
                </button>
                <button type="button" className="pe-button" onClick={() => setShowDeleteButtons((current) => !current)}>
                    <X className="size-3.5" /> {showDeleteButtons ? "隐藏删除按钮" : "显示删除按钮"}
                </button>
                {preset ? (
                    <label className="pe-preset" title="生成时按当前模型注入，不会写入输入框">
                        <span className="pe-preset__label">{preset.label}</span>
                        <select className="pe-preset__select" value={preset.value} onChange={(event) => preset.onChange(event.target.value)}>
                            {preset.options.map((option) => (
                                <option key={option.value} value={option.value}>
                                    {option.label}
                                </option>
                            ))}
                        </select>
                    </label>
                ) : null}
                <button type="button" className="pe-button" disabled={!authToken || isTranslatingAll} title={authToken ? "使用后台配置的网络翻译" : "请先登录"} onClick={() => void translateAllTokens()}>
                    <TranslateIcon size={14} /> {isTranslatingAll ? "翻译中…" : "一键翻译Tag"}
                </button>
                {actionsExtra}
                <span className="pe-dialog__hint">单击 Tag 文字编辑 · 双击屏蔽 · 拖动排序</span>
                {translateError ? <span className="pe-dialog__error">{translateError}</span> : null}
            </div>

            <div className="pe-tokens">
                {tokens.length ? (
                    tokens.map((token, index) => (
                        <div
                            key={token.id}
                            className={`pe-token ${token.disabled ? "is-disabled" : ""} ${dragIndex === index ? "is-dragging" : ""} ${editingTokenId === token.id ? "is-editing" : ""}`}
                            draggable={editingTokenId !== token.id}
                            onDragStart={() => setDragIndex(index)}
                            onDragOver={(event) => event.preventDefault()}
                            onDrop={(event) => handleDrop(index, event)}
                            onDragEnd={() => setDragIndex(null)}
                            onDoubleClick={() => toggleTokenDisabled(index)}
                        >
                            <div className="pe-token__head">
                                {editingTokenId === token.id ? (
                                    <input
                                        className="pe-token__edit"
                                        value={editValue}
                                        autoFocus
                                        onChange={(event) => setEditValue(event.target.value)}
                                        onBlur={finishEditToken}
                                        onKeyDown={(event) => {
                                            event.stopPropagation();
                                            if (event.key === "Enter") finishEditToken();
                                            if (event.key === "Escape") {
                                                setEditingTokenId(null);
                                                setEditValue("");
                                            }
                                        }}
                                    />
                                ) : (
                                    <button type="button" className="pe-token__text" title="单击编辑，双击屏蔽" onClick={() => scheduleEditToken(token)}>
                                        {token.kind === "newline" ? "↵" : token.text}
                                    </button>
                                )}
                                {showDeleteButtons && editingTokenId !== token.id ? (
                                    <button type="button" className="pe-token__remove" onClick={() => commitTokens(tokens.filter((_, itemIndex) => itemIndex !== index))} aria-label={`删除 ${token.text}`}>
                                        ×
                                    </button>
                                ) : null}
                            </div>
                            {token.kind === "newline" ? null : (
                                <div className="pe-token__translation">
                                    <button
                                        type="button"
                                        className="pe-token__translate-icon"
                                        title={authToken ? "翻译该 Tag" : "请先登录"}
                                        aria-label="翻译"
                                        disabled={!authToken || translatingTokenIds.includes(token.id)}
                                        onClick={() => void translateToken(token)}
                                    >
                                        <TranslateIcon />
                                    </button>
                                    <span className="pe-token__translation-text">{translatingTokenIds.includes(token.id) ? "翻译中…" : token.translation || ""}</span>
                                </div>
                            )}
                        </div>
                    ))
                ) : (
                    <div className="pe-tokens__empty">在上方输入提示词后，这里会生成可拖拽、可禁用的提示词块。</div>
                )}
            </div>

            {inline ? null : (
                <div className="pe-dialog__footer">
                    <button type="button" className="pe-button" onClick={onClose}>
                        取消
                    </button>
                    <button
                        type="button"
                        className="pe-button is-primary"
                        onClick={() => {
                            const normalized = normalizePromptBlockTokens(parsePromptToTokens(draft, tokens));
                            onSubmit(serializeTokensToPrompt(normalized), normalized);
                            onClose();
                        }}
                    >
                        应用
                    </button>
                </div>
            )}

            {inline
                ? null
                : RESIZE_CORNERS.map((corner) => (
                      <div key={corner} className={`pe-resize pe-resize--${corner}`} onPointerDown={(event) => startResize(corner, event)} />
                  ))}
        </div>
    );

    // 内联模式直接就地渲染；弹窗模式仍旧 portal 到 body（画布节点依赖这个行为）。
    return inline ? dialog : createPortal(dialog, document.body);
}
