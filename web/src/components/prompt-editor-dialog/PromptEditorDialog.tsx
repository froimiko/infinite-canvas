"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, DragEvent, KeyboardEvent, PointerEvent as ReactPointerEvent } from "react";
import { createPortal } from "react-dom";
import { Eraser, Trash2, X } from "lucide-react";

import { createPromptBlockToken, normalizePromptBlockTokens, parsePromptToTokens, serializeTokensToPrompt } from "@/components/prompt-block-editor/prompt-block-utils";
import type { PromptBlockToken } from "@/components/prompt-block-editor/prompt-block-types";
import { searchTags, type TagSearchResult } from "@/services/tag-service";
import { getCurrentWord, measureCaretPosition, replaceCurrentWord, type CurrentWord } from "./prompt-editor-utils";
import { TranslateIcon } from "./translate-icon";
import "./prompt-editor-dialog.css";

const SEARCH_DEBOUNCE_MS = 160;
const MAX_SUGGESTIONS = 12;

export type PromptEditorTarget = {
    title: string;
    value: string;
    tokens?: PromptBlockToken[];
};

type PromptEditorDialogProps = {
    open: boolean;
    title?: string;
    target: PromptEditorTarget;
    onSubmit: (value: string, tokens: PromptBlockToken[]) => void;
    onClose: () => void;
};

export function PromptEditorDialog({ open, title = "NovelAI 提示词编辑器", target, onSubmit, onClose }: PromptEditorDialogProps) {
    const [tokens, setTokens] = useState<PromptBlockToken[]>([]);
    const [draft, setDraft] = useState("");
    const [suggestions, setSuggestions] = useState<TagSearchResult[]>([]);
    const [showSuggestions, setShowSuggestions] = useState(false);
    const [selectedIndex, setSelectedIndex] = useState(0);
    const [menuStyle, setMenuStyle] = useState<CSSProperties>({ left: 0, top: 0 });
    const [showDeleteButtons, setShowDeleteButtons] = useState(true);
    const [position, setPosition] = useState({ x: 0, y: 0 });
    const [dragging, setDragging] = useState(false);
    const [dragIndex, setDragIndex] = useState<number | null>(null);
    const textareaRef = useRef<HTMLTextAreaElement>(null);
    const wrapRef = useRef<HTMLDivElement>(null);
    const dragOffsetRef = useRef<{ x: number; y: number } | null>(null);
    const currentWordRef = useRef<CurrentWord>({ query: "", replaceStart: 0, replaceEnd: 0 });
    const searchIdRef = useRef(0);

    const tokenCount = useMemo(() => tokens.filter((token) => !token.disabled && token.kind !== "newline").length, [tokens]);

    useEffect(() => {
        if (!open) return;
        const initial = target.tokens?.length ? normalizePromptBlockTokens(target.tokens) : normalizePromptBlockTokens(parsePromptToTokens(target.value || ""));
        setTokens(initial);
        setDraft(serializeTokensToPrompt(initial));
        setShowSuggestions(false);
        setPosition({ x: Math.max(24, window.innerWidth / 2 - 480), y: Math.max(24, window.innerHeight / 2 - 260) });
    }, [open, target.tokens, target.value]);

    const syncCaretMenu = useCallback(() => {
        const textarea = textareaRef.current;
        const wrap = wrapRef.current;
        if (!textarea || !wrap) return;
        setMenuStyle(measureCaretPosition(textarea, wrap));
    }, []);

    useEffect(() => {
        if (!open) return;
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
    }, [draft, open]);

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

    if (!open || typeof document === "undefined") return null;

    const commitTokens = (next: PromptBlockToken[]) => {
        const normalized = normalizePromptBlockTokens(next);
        setTokens(normalized);
        setDraft(serializeTokensToPrompt(normalized));
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

    const startWindowDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
        dragOffsetRef.current = { x: event.clientX - position.x, y: event.clientY - position.y };
        setDragging(true);
    };

    return createPortal(
        <div className="pe-dialog" style={{ left: position.x, top: position.y }} onMouseDown={(event) => event.stopPropagation()} onPointerDown={(event) => event.stopPropagation()} onWheel={(event) => event.stopPropagation()}>
            <div className="pe-dialog__header" onPointerDown={startWindowDrag}>
                <span className="pe-dialog__title">
                    {title} · {target.title}
                </span>
                <button type="button" className="pe-dialog__close" onClick={onClose} aria-label="关闭">
                    <X className="size-4" />
                </button>
            </div>

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
                <button type="button" className="pe-button" disabled title="翻译功能待重做">
                    <TranslateIcon size={14} /> 一键翻译Tag
                </button>
            </div>

            <div className="pe-tokens">
                {tokens.length ? (
                    tokens.map((token, index) => (
                        <div
                            key={token.id}
                            className={`pe-token ${token.disabled ? "is-disabled" : ""} ${dragIndex === index ? "is-dragging" : ""}`}
                            draggable
                            onDragStart={() => setDragIndex(index)}
                            onDragOver={(event) => event.preventDefault()}
                            onDrop={(event) => handleDrop(index, event)}
                            onDragEnd={() => setDragIndex(null)}
                            onDoubleClick={() => commitTokens(tokens.map((item, itemIndex) => (itemIndex === index ? { ...item, disabled: !item.disabled } : item)))}
                            title="双击禁用/启用，拖拽排序"
                        >
                            <div className="pe-token__head">
                                <span className="pe-token__text">{token.kind === "newline" ? "↵" : token.text}</span>
                                {showDeleteButtons ? (
                                    <button type="button" className="pe-token__remove" onClick={() => commitTokens(tokens.filter((_, itemIndex) => itemIndex !== index))} aria-label={`删除 ${token.text}`}>
                                        ×
                                    </button>
                                ) : null}
                            </div>
                            {token.kind === "newline" ? null : (
                                <div className="pe-token__translation">
                                    {/* TODO: 翻译功能待重做，此处仅保留入口与占位 */}
                                    <button type="button" className="pe-token__translate-icon" title="翻译（待重做）" aria-label="翻译">
                                        <TranslateIcon />
                                    </button>
                                    <span className="pe-token__translation-text">{token.translation || ""}</span>
                                </div>
                            )}
                        </div>
                    ))
                ) : (
                    <div className="pe-tokens__empty">在上方输入提示词后，这里会生成可拖拽、可禁用的提示词块。</div>
                )}
            </div>

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
        </div>,
        document.body,
    );
}
