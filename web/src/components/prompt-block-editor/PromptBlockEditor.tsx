"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, ChangeEvent, DragEvent, KeyboardEvent, MouseEvent, PointerEvent, RefObject } from "react";

import { searchPromptTags, translatePromptTags } from "@/services/api/prompt-tags";
import type { PromptTagSearchResult } from "@/services/api/prompt-tags";
import type { PromptBlockEditorProps, PromptBlockMentionReference, PromptBlockToken } from "./prompt-block-types";
import { createPromptBlockToken, normalizePromptBlockTokens, parsePromptToTokens, promptTagSuggestionToToken, serializeTokensToPrompt, tokenNeedsTranslation } from "./prompt-block-utils";
import "./prompt-block-editor.css";

const DEFAULT_MAX_SUGGESTIONS = 12;
const CLICK_EDIT_DELAY_MS = 180;
const SEARCH_DEBOUNCE_MS = 160;
const TRANSLATE_DEBOUNCE_MS = 260;

type CurrentWord = {
    query: string;
    replaceStart: number;
    replaceEnd: number;
};

export function PromptBlockEditor({
    value,
    onChange,
    tokens,
    onTokensChange,
    mentionReferences = [],
    onSubmit,
    placeholder = "输入提示词并回车添加 tag",
    disabled = false,
    className = "",
    style,
    compact = false,
    rows = 3,
    maxSuggestions = DEFAULT_MAX_SUGGESTIONS,
    onKeyDown,
}: PromptBlockEditorProps) {
    const [internalTokens, setInternalTokens] = useState<PromptBlockToken[]>(() => normalizePromptBlockTokens(tokens?.length ? tokens : parsePromptToTokens(value)));
    const [currentWord, setCurrentWord] = useState<CurrentWord>(() => getCurrentWord(value, value.length, value.length));
    const [suggestions, setSuggestions] = useState<PromptTagSearchResult[]>([]);
    const [selectedSuggestionIndex, setSelectedSuggestionIndex] = useState(0);
    const [showSuggestions, setShowSuggestions] = useState(false);
    const [isSearching, setIsSearching] = useState(false);
    const [selectedMentionIndex, setSelectedMentionIndex] = useState(0);
    const [showMentions, setShowMentions] = useState(false);
    const [menuStyle, setMenuStyle] = useState<CSSProperties>({ left: 0, top: 0 });
    const [editingTokenId, setEditingTokenId] = useState<string | null>(null);
    const [editValue, setEditValue] = useState("");
    const [dragIndex, setDragIndex] = useState<number | null>(null);
    const [dragOverIndex, setDragOverIndex] = useState<number | null>(null);
    const textareaRef = useRef<HTMLTextAreaElement>(null);
    const textareaWrapRef = useRef<HTMLDivElement>(null);
    const suggestionsRef = useRef<HTMLDivElement>(null);
    const mentionsRef = useRef<HTMLDivElement>(null);
    const editClickTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const searchRequestRef = useRef(0);
    const lastEmittedValueRef = useRef(value);
    const isControlledTokens = tokens !== undefined;
    const activeTokens = useMemo(() => normalizePromptBlockTokens(isControlledTokens ? tokens : internalTokens), [internalTokens, isControlledTokens, tokens]);
    const serializedValue = useMemo(() => serializeTokensToPrompt(activeTokens), [activeTokens]);
    const editorMinHeight = compact ? undefined : Math.max(64, rows * 32);
    const query = currentWord.query;
    const trimmedQuery = query.trim();
    const isMentionQuery = trimmedQuery.startsWith("@");
    const mentionMatches = useMemo(() => filterMentionReferences(mentionReferences, query, maxSuggestions), [mentionReferences, maxSuggestions, query]);

    const syncMenuPosition = useCallback(() => {
        const textarea = textareaRef.current;
        const wrap = textareaWrapRef.current;
        if (!textarea || !wrap) return;
        setMenuStyle(getTextareaCursorMenuStyle(textarea, wrap));
    }, []);

    const syncCurrentWordFromTextarea = useCallback(() => {
        const textarea = textareaRef.current;
        if (!textarea) return;
        setCurrentWord(getCurrentWord(textarea.value, textarea.selectionStart, textarea.selectionEnd));
        syncMenuPosition();
    }, [syncMenuPosition]);

    const syncTokensFromValue = useCallback(
        (nextValue: string, previousTokens: PromptBlockToken[] = activeTokens, preferredToken?: PromptBlockToken, preferredTokenIndex?: number) => {
            const nextTokens = parsePromptToTokens(nextValue, previousTokens);
            if (preferredToken && preferredTokenIndex !== undefined && nextTokens[preferredTokenIndex]?.text === preferredToken.text) {
                nextTokens[preferredTokenIndex] = { ...preferredToken, disabled: nextTokens[preferredTokenIndex].disabled === true };
            }
            const normalized = normalizePromptBlockTokens(nextTokens);
            setInternalTokens(normalized);
            onTokensChange?.(normalized);
            return normalized;
        },
        [activeTokens, onTokensChange],
    );

    useEffect(() => {
        if (isControlledTokens) return;
        const isInternalValueChange = value === lastEmittedValueRef.current;
        setInternalTokens((previousTokens) => normalizePromptBlockTokens(parsePromptToTokens(value, isInternalValueChange ? previousTokens : [])));
        lastEmittedValueRef.current = value;
    }, [isControlledTokens, value]);

    useEffect(() => {
        const textarea = textareaRef.current;
        if (!textarea || document.activeElement !== textarea) return;
        setCurrentWord(getCurrentWord(value, textarea.selectionStart, textarea.selectionEnd));
        syncMenuPosition();
    }, [syncMenuPosition, value]);

    useEffect(() => {
        return () => {
            if (editClickTimerRef.current) clearTimeout(editClickTimerRef.current);
        };
    }, []);

    const applyTokens = useCallback(
        (nextTokens: PromptBlockToken[]) => {
            const normalized = normalizePromptBlockTokens(nextTokens);
            setInternalTokens(normalized);
            onTokensChange?.(normalized);
            const nextValue = serializeTokensToPrompt(normalized);
            if (nextValue !== value) {
                lastEmittedValueRef.current = nextValue;
                onChange(nextValue);
            }
            window.setTimeout(() => {
                const textarea = textareaRef.current;
                textarea?.focus();
                const caret = nextValue.length;
                textarea?.setSelectionRange(caret, caret);
                syncCurrentWordFromTextarea();
            }, 0);
        },
        [onChange, onTokensChange, syncCurrentWordFromTextarea, value],
    );

    useEffect(() => {
        const keyword = trimmedQuery;
        if (!keyword || disabled || keyword.startsWith("@")) {
            setSuggestions([]);
            setShowSuggestions(false);
            setIsSearching(false);
            searchRequestRef.current += 1;
            return;
        }
        const requestId = searchRequestRef.current + 1;
        searchRequestRef.current = requestId;
        setIsSearching(true);
        const timeout = window.setTimeout(() => {
            void searchPromptTags({ keyword, limit: maxSuggestions })
                .then((items) => {
                    if (searchRequestRef.current !== requestId) return;
                    setSuggestions(items);
                    setSelectedSuggestionIndex(0);
                    setShowSuggestions(items.length > 0);
                })
                .catch(() => {
                    if (searchRequestRef.current !== requestId) return;
                    setSuggestions([]);
                    setShowSuggestions(false);
                })
                .finally(() => {
                    if (searchRequestRef.current === requestId) setIsSearching(false);
                });
        }, SEARCH_DEBOUNCE_MS);
        return () => window.clearTimeout(timeout);
    }, [disabled, maxSuggestions, trimmedQuery]);

    useEffect(() => {
        if (!isMentionQuery || disabled) {
            setShowMentions(false);
            setSelectedMentionIndex(0);
            return;
        }
        setSuggestions([]);
        setShowSuggestions(false);
        setIsSearching(false);
        setSelectedMentionIndex(0);
        setShowMentions(mentionMatches.length > 0);
    }, [disabled, isMentionQuery, mentionMatches.length]);

    useEffect(() => {
        scrollSelectedItemIntoView(mentionsRef, showMentions, selectedMentionIndex);
    }, [selectedMentionIndex, showMentions]);

    useEffect(() => {
        const missing = activeTokens.filter(tokenNeedsTranslation);
        if (!missing.length) return;
        const uniqueTags = Array.from(new Set(missing.map((token) => token.text.trim()).filter(Boolean)));
        if (!uniqueTags.length) return;
        const timeout = window.setTimeout(() => {
            void translatePromptTags(uniqueTags)
                .then((translations) => {
                    if (!Object.keys(translations).length) return;
                    const nextTokens = activeTokens.map((token) => (token.translation?.trim() || !translations[token.text] ? token : { ...token, translation: translations[token.text] }));
                    const normalized = normalizePromptBlockTokens(nextTokens);
                    setInternalTokens(normalized);
                    onTokensChange?.(normalized);
                })
                .catch(() => undefined);
        }, TRANSLATE_DEBOUNCE_MS);
        return () => window.clearTimeout(timeout);
    }, [activeTokens, onTokensChange]);

    useEffect(() => {
        scrollSelectedItemIntoView(suggestionsRef, showSuggestions, selectedSuggestionIndex);
    }, [selectedSuggestionIndex, showSuggestions]);

    const replaceCurrentWord = useCallback(
        (text: string, preferredToken: PromptBlockToken, separator = ", ") => {
            if (disabled) return;
            const textarea = textareaRef.current;
            const word = textarea ? getCurrentWord(value, textarea.selectionStart, textarea.selectionEnd) : currentWord;
            const tokenIndex = parsePromptToTokens(value.slice(0, word.replaceStart)).length;
            const nextValue = replaceWordInValue(value, word, text, separator);
            const caret = word.replaceStart + text.length + insertedSeparatorLength(value, word, separator);
            syncTokensFromValue(nextValue, activeTokens, preferredToken, tokenIndex);
            lastEmittedValueRef.current = nextValue;
            onChange(nextValue);
            setCurrentWord(getCurrentWord(nextValue, caret, caret));
            setSuggestions([]);
            setShowSuggestions(false);
            setSelectedSuggestionIndex(0);
            setShowMentions(false);
            setSelectedMentionIndex(0);
            window.setTimeout(() => {
                textareaRef.current?.focus();
                textareaRef.current?.setSelectionRange(caret, caret);
                syncMenuPosition();
            }, 0);
        },
        [activeTokens, currentWord, disabled, onChange, syncMenuPosition, syncTokensFromValue, value],
    );

    const insertSuggestion = useCallback(
        (suggestion: PromptTagSearchResult) => {
            const token = promptTagSuggestionToToken(suggestion);
            replaceCurrentWord(token.text, token);
        },
        [replaceCurrentWord],
    );

    const insertMention = useCallback(
        (reference: PromptBlockMentionReference) => {
            const token = createPromptBlockToken(reference.label, {
                translation: reference.title,
                referenceNodeId: reference.nodeId,
                referenceKind: reference.kind,
            });
            replaceCurrentWord(token.text, token);
        },
        [replaceCurrentWord],
    );

    const removeToken = useCallback(
        (tokenId: string) => {
            if (disabled) return;
            applyTokens(activeTokens.filter((token) => token.id !== tokenId));
        },
        [activeTokens, applyTokens, disabled],
    );

    const toggleTokenDisabled = useCallback(
        (tokenId: string) => {
            if (disabled) return;
            if (editClickTimerRef.current) clearTimeout(editClickTimerRef.current);
            setEditingTokenId(null);
            applyTokens(activeTokens.map((token) => (token.id === tokenId ? { ...token, disabled: !token.disabled } : token)));
        },
        [activeTokens, applyTokens, disabled],
    );

    const startEditToken = useCallback(
        (token: PromptBlockToken) => {
            if (disabled) return;
            setEditingTokenId(token.id);
            setEditValue(token.text);
        },
        [disabled],
    );

    const scheduleEditToken = useCallback(
        (token: PromptBlockToken) => {
            if (disabled) return;
            if (editClickTimerRef.current) clearTimeout(editClickTimerRef.current);
            editClickTimerRef.current = setTimeout(() => startEditToken(token), CLICK_EDIT_DELAY_MS);
        },
        [disabled, startEditToken],
    );

    const commitEditToken = useCallback(() => {
        if (!editingTokenId) return;
        const text = editValue.trim();
        const nextTokens = text
            ? activeTokens.map((token) => {
                  if (token.id !== editingTokenId) return token;
                  if (token.text.trim() === text) return token;
                  return createPromptBlockToken(text, { id: token.id, disabled: token.disabled });
              })
            : activeTokens.filter((token) => token.id !== editingTokenId);
        setEditingTokenId(null);
        setEditValue("");
        applyTokens(nextTokens);
    }, [activeTokens, applyTokens, editValue, editingTokenId]);

    const cancelEditToken = useCallback(() => {
        setEditingTokenId(null);
        setEditValue("");
    }, []);

    const handleTextareaChange = (event: ChangeEvent<HTMLTextAreaElement>) => {
        const nextValue = event.target.value;
        const nextWord = getCurrentWord(nextValue, event.target.selectionStart, event.target.selectionEnd);
        lastEmittedValueRef.current = nextValue;
        onChange(nextValue);
        syncTokensFromValue(nextValue);
        setCurrentWord(nextWord);
        syncMenuPosition();
    };

    const handleTextareaKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
        event.stopPropagation();
        if (showMentions && mentionMatches.length) {
            if (event.key === "ArrowDown") {
                event.preventDefault();
                setSelectedMentionIndex((index) => (index + 1) % mentionMatches.length);
                return;
            }
            if (event.key === "ArrowUp") {
                event.preventDefault();
                setSelectedMentionIndex((index) => (index - 1 + mentionMatches.length) % mentionMatches.length);
                return;
            }
            if (event.key === "Enter" || event.key === "Tab") {
                event.preventDefault();
                insertMention(mentionMatches[Math.min(selectedMentionIndex, mentionMatches.length - 1)]);
                return;
            }
            if (event.key === "Escape") {
                event.preventDefault();
                setShowMentions(false);
                return;
            }
        }
        if (showSuggestions && suggestions.length && !isMentionQuery) {
            if (event.key === "ArrowDown") {
                event.preventDefault();
                setSelectedSuggestionIndex((index) => (index + 1) % suggestions.length);
                return;
            }
            if (event.key === "ArrowUp") {
                event.preventDefault();
                setSelectedSuggestionIndex((index) => (index - 1 + suggestions.length) % suggestions.length);
                return;
            }
            if (event.key === "Enter" || event.key === "Tab") {
                event.preventDefault();
                insertSuggestion(suggestions[Math.min(selectedSuggestionIndex, suggestions.length - 1)]);
                return;
            }
        }
        if (event.key === "Escape" && (showSuggestions || showMentions)) {
            event.preventDefault();
            setShowSuggestions(false);
            setShowMentions(false);
            return;
        }
        if (event.key === "Enter" && !event.shiftKey && !event.metaKey && !event.ctrlKey && !trimmedQuery && !showSuggestions && !showMentions && onSubmit) {
            event.preventDefault();
            onSubmit();
            return;
        }
        onKeyDown?.(event);
        window.requestAnimationFrame(syncCurrentWordFromTextarea);
    };

    const handleEditKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
        event.stopPropagation();
        if (event.key === "Enter") {
            event.preventDefault();
            commitEditToken();
        }
        if (event.key === "Escape") {
            event.preventDefault();
            cancelEditToken();
        }
    };

    const handleDragStart = (index: number, event: DragEvent<HTMLDivElement>) => {
        if (disabled) return;
        setDragIndex(index);
        event.dataTransfer.effectAllowed = "move";
        event.dataTransfer.setData("text/plain", activeTokens[index]?.text || "");
    };

    const handleDragOver = (index: number, event: DragEvent<HTMLDivElement>) => {
        if (disabled || dragIndex === null) return;
        event.preventDefault();
        setDragOverIndex(index);
    };

    const handleDrop = (index: number, event: DragEvent<HTMLDivElement>) => {
        event.preventDefault();
        if (disabled || dragIndex === null || dragIndex === index) {
            setDragIndex(null);
            setDragOverIndex(null);
            return;
        }
        const rect = event.currentTarget.getBoundingClientRect();
        const insertAfter = event.clientX > rect.left + rect.width / 2;
        let targetIndex = index + (insertAfter ? 1 : 0);
        if (dragIndex < targetIndex) targetIndex -= 1;
        const nextTokens = [...activeTokens];
        const [dragged] = nextTokens.splice(dragIndex, 1);
        nextTokens.splice(Math.max(0, Math.min(targetIndex, nextTokens.length)), 0, dragged);
        applyTokens(nextTokens);
        setDragIndex(null);
        setDragOverIndex(null);
    };

    const handleDragEnd = () => {
        setDragIndex(null);
        setDragOverIndex(null);
    };

    const stopInteraction = (event: MouseEvent | PointerEvent) => event.stopPropagation();

    return (
        <div
            className={`prompt-block-editor ${compact ? "is-compact" : ""} ${disabled ? "is-disabled" : ""} ${className}`}
            style={{ ...style, minHeight: editorMinHeight }}
            onMouseDown={stopInteraction}
            onPointerDown={stopInteraction}
            onWheel={(event) => event.stopPropagation()}
        >
            <div ref={textareaWrapRef} className="prompt-block-editor__textarea-wrap">
                <textarea
                    ref={textareaRef}
                    value={value}
                    disabled={disabled}
                    rows={rows}
                    className="prompt-block-editor__textarea"
                    placeholder={placeholder}
                    onChange={handleTextareaChange}
                    onKeyDown={handleTextareaKeyDown}
                    onFocus={syncCurrentWordFromTextarea}
                    onClick={syncCurrentWordFromTextarea}
                    onSelect={syncCurrentWordFromTextarea}
                    onScroll={syncMenuPosition}
                />
                {showMentions ? <MentionMenu references={mentionMatches} selectedIndex={selectedMentionIndex} menuRef={mentionsRef} menuStyle={menuStyle} onSelect={insertMention} onHover={setSelectedMentionIndex} /> : null}
                {!isMentionQuery && (showSuggestions || isSearching) ? (
                    <SuggestionMenu suggestions={suggestions} isLoading={isSearching} selectedIndex={selectedSuggestionIndex} menuRef={suggestionsRef} menuStyle={menuStyle} onSelect={insertSuggestion} onHover={setSelectedSuggestionIndex} />
                ) : null}
            </div>
            <section className="prompt-block-editor__token-panel" aria-label="提示词块辅助操作区">
                <div className="prompt-block-editor__token-panel-header">
                    <span className="prompt-block-editor__token-panel-title">提示词块</span>
                    <span className="prompt-block-editor__token-panel-hint">拖拽排序 / 单击编辑 / 双击禁用 / 单独复制</span>
                </div>
                {activeTokens.length ? (
                    <div className="prompt-block-editor__token-list" aria-label="提示词块列表">
                        {activeTokens.map((token, index) => (
                            <PromptTokenItem
                                key={token.id}
                                token={token}
                                index={index}
                                disabled={disabled}
                                isEditing={editingTokenId === token.id}
                                editValue={editingTokenId === token.id ? editValue : ""}
                                isDragging={dragIndex === index}
                                isDragOver={dragOverIndex === index}
                                onEditValueChange={setEditValue}
                                onCommitEdit={commitEditToken}
                                onCancelEdit={cancelEditToken}
                                onEditKeyDown={handleEditKeyDown}
                                onScheduleEdit={scheduleEditToken}
                                onToggleDisabled={toggleTokenDisabled}
                                onRemove={removeToken}
                                onDragStart={handleDragStart}
                                onDragOver={handleDragOver}
                                onDrop={handleDrop}
                                onDragEnd={handleDragEnd}
                            />
                        ))}
                    </div>
                ) : (
                    <div className="prompt-block-editor__token-empty">在上方输入或粘贴提示词后，这里会生成可拖拽、可禁用、可单独复制的提示词块。</div>
                )}
            </section>
            <input type="hidden" value={value || serializedValue} readOnly />
        </div>
    );
}

function getCurrentWord(value: string, selectionStart: number, selectionEnd = selectionStart): CurrentWord {
    let replaceStart = Math.max(0, Math.min(selectionStart, value.length));
    let replaceEnd = Math.max(replaceStart, Math.min(selectionEnd, value.length));
    while (replaceStart > 0 && !isCurrentWordSeparator(value[replaceStart - 1])) replaceStart -= 1;
    while (replaceEnd < value.length && !isCurrentWordSeparator(value[replaceEnd])) replaceEnd += 1;
    return {
        query: value.slice(replaceStart, replaceEnd),
        replaceStart,
        replaceEnd,
    };
}

function isCurrentWordSeparator(char: string) {
    return char === "," || char === "，" || /\s/.test(char);
}

function replaceWordInValue(value: string, word: CurrentWord, text: string, separator: string) {
    const suffix = insertedSeparator(value, word, separator);
    return `${value.slice(0, word.replaceStart)}${text}${suffix}${value.slice(word.replaceEnd)}`;
}

function insertedSeparatorLength(value: string, word: CurrentWord, separator: string) {
    return insertedSeparator(value, word, separator).length;
}

function insertedSeparator(value: string, word: CurrentWord, separator: string) {
    const nextChar = value[word.replaceEnd] || "";
    if (!nextChar || !isCurrentWordSeparator(nextChar)) return separator;
    return "";
}

function getTextareaCursorMenuStyle(textarea: HTMLTextAreaElement, wrap: HTMLDivElement): CSSProperties {
    const position = textarea.selectionStart ?? textarea.value.length;
    const caret = measureTextareaCaret(textarea, position);
    const menuWidth = 360;
    const left = Math.max(4, Math.min(caret.left, wrap.clientWidth - menuWidth - 4));
    const top = Math.max(4, Math.min(caret.top + 6, wrap.clientHeight - 48));
    return { left, top };
}

function measureTextareaCaret(textarea: HTMLTextAreaElement, position: number) {
    const style = window.getComputedStyle(textarea);
    const mirror = document.createElement("div");
    const span = document.createElement("span");
    const properties = [
        "box-sizing",
        "width",
        "border-top-width",
        "border-right-width",
        "border-bottom-width",
        "border-left-width",
        "padding-top",
        "padding-right",
        "padding-bottom",
        "padding-left",
        "font-family",
        "font-size",
        "font-weight",
        "font-style",
        "letter-spacing",
        "line-height",
        "text-transform",
        "text-indent",
        "text-align",
        "white-space",
        "word-spacing",
    ];
    properties.forEach((property) => {
        mirror.style.setProperty(property, style.getPropertyValue(property));
    });
    mirror.style.position = "absolute";
    mirror.style.visibility = "hidden";
    mirror.style.overflow = "hidden";
    mirror.style.whiteSpace = "pre-wrap";
    mirror.style.overflowWrap = "break-word";
    mirror.style.top = "0";
    mirror.style.left = "0";
    mirror.textContent = textarea.value.slice(0, position);
    span.textContent = textarea.value.slice(position, position + 1) || ".";
    mirror.appendChild(span);
    document.body.appendChild(mirror);
    const lineHeight = Number.parseFloat(style.lineHeight) || Number.parseFloat(style.fontSize) * 1.4 || 20;
    const result = {
        left: span.offsetLeft - textarea.scrollLeft,
        top: span.offsetTop + lineHeight - textarea.scrollTop,
    };
    document.body.removeChild(mirror);
    return result;
}

type PromptTokenItemProps = {
    token: PromptBlockToken;
    index: number;
    disabled: boolean;
    isEditing: boolean;
    editValue: string;
    isDragging: boolean;
    isDragOver: boolean;
    onEditValueChange: (value: string) => void;
    onCommitEdit: () => void;
    onCancelEdit: () => void;
    onEditKeyDown: (event: KeyboardEvent<HTMLInputElement>) => void;
    onScheduleEdit: (token: PromptBlockToken) => void;
    onToggleDisabled: (tokenId: string) => void;
    onRemove: (tokenId: string) => void;
    onDragStart: (index: number, event: DragEvent<HTMLDivElement>) => void;
    onDragOver: (index: number, event: DragEvent<HTMLDivElement>) => void;
    onDrop: (index: number, event: DragEvent<HTMLDivElement>) => void;
    onDragEnd: () => void;
};

function PromptTokenItem({
    token,
    index,
    disabled,
    isEditing,
    editValue,
    isDragging,
    isDragOver,
    onEditValueChange,
    onCommitEdit,
    onCancelEdit,
    onEditKeyDown,
    onScheduleEdit,
    onToggleDisabled,
    onRemove,
    onDragStart,
    onDragOver,
    onDrop,
    onDragEnd,
}: PromptTokenItemProps) {
    const kind = token.kind || "tag";
    const displayText = tokenDisplayText(token);
    const secondaryText = tokenSecondaryText(token);
    const kindLabel = tokenKindLabel(token);
    const actionLabel = tokenActionLabel(token);

    if (isEditing) {
        return (
            <div className={`prompt-block-token is-editing is-kind-${kind}`}>
                <input className="prompt-block-token__edit-input" value={editValue} autoFocus onChange={(event) => onEditValueChange(event.target.value)} onBlur={onCommitEdit} onKeyDown={onEditKeyDown} />
                <button
                    type="button"
                    className="prompt-block-token__mini-button"
                    onMouseDown={(event) => {
                        event.preventDefault();
                        event.stopPropagation();
                    }}
                    onClick={(event) => {
                        event.preventDefault();
                        event.stopPropagation();
                        onCancelEdit();
                    }}
                    aria-label="取消编辑提示词块"
                >
                    ×
                </button>
            </div>
        );
    }

    return (
        <div
            className={`prompt-block-token is-kind-${kind} ${token.disabled ? "is-disabled" : ""} ${isDragging ? "is-dragging" : ""} ${isDragOver ? "is-drag-over" : ""}`}
            draggable={!disabled}
            onDragStart={(event) => {
                event.stopPropagation();
                onDragStart(index, event);
            }}
            onDragOver={(event) => {
                event.stopPropagation();
                onDragOver(index, event);
            }}
            onDrop={(event) => {
                event.stopPropagation();
                onDrop(index, event);
            }}
            onDragEnd={(event) => {
                event.stopPropagation();
                onDragEnd();
            }}
            title={`${actionLabel}；拖拽排序；单击编辑；双击${token.disabled ? "启用" : "禁用"}；复制单个提示词块`}
        >
            <button
                type="button"
                className="prompt-block-token__body"
                disabled={disabled}
                onClick={() => onScheduleEdit(token)}
                onDoubleClick={() => onToggleDisabled(token.id)}
                aria-label={`${actionLabel}，单击编辑，双击${token.disabled ? "启用" : "禁用"}`}
            >
                {kind === "lora" ? <span className="prompt-block-token__badge">LoRA</span> : null}
                {kind === "mention" ? <span className="prompt-block-token__badge">@{token.referenceKind || "资源"}</span> : null}
                <span className="prompt-block-token__text">{displayText}</span>
                {secondaryText ? <span className="prompt-block-token__translation">{secondaryText}</span> : null}
            </button>
            <button
                type="button"
                className="prompt-block-token__copy"
                disabled={disabled}
                onMouseDown={stopTokenButtonMouseDown}
                onClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    void copyPromptTokenText(token);
                }}
                aria-label={`复制 ${kindLabel} ${displayText}`}
                title="复制单个提示词块"
            >
                ⧉
            </button>
            <button
                type="button"
                className="prompt-block-token__remove"
                disabled={disabled}
                onMouseDown={stopTokenButtonMouseDown}
                onClick={(event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    onRemove(token.id);
                }}
                aria-label={`删除 ${kindLabel} ${displayText}`}
                title="删除提示词块"
            >
                ×
            </button>
        </div>
    );
}

function stopTokenButtonMouseDown(event: MouseEvent<HTMLButtonElement>) {
    event.preventDefault();
    event.stopPropagation();
}

function tokenKindLabel(token: PromptBlockToken) {
    if (token.kind === "text") return "自然语言";
    if (token.kind === "mention") return "资源引用";
    if (token.kind === "newline") return "换行";
    if (token.kind === "lora") return "LoRA";
    return "Tag";
}

function tokenDisplayText(token: PromptBlockToken) {
    if (token.kind === "newline") return "↵ 换行";
    if (token.kind === "mention") return token.text.startsWith("@") ? token.text : `@${token.text}`;
    return token.text;
}

function tokenSecondaryText(token: PromptBlockToken) {
    if (token.kind === "newline") return "";
    if (token.kind === "text") return token.translation?.trim() || "";
    if (token.kind === "mention") return [token.referenceKind, token.translation].filter(Boolean).join(" · ");
    return token.translation?.trim() || "";
}

function tokenActionLabel(token: PromptBlockToken) {
    const kind = tokenKindLabel(token);
    const text = tokenDisplayText(token);
    return `${kind} ${text}`;
}

async function copyPromptTokenText(token: PromptBlockToken) {
    try {
        await navigator.clipboard?.writeText(token.kind === "newline" ? "\n" : token.text);
    } catch {
        // Clipboard failures are intentionally silent: token copy must not interrupt editing.
    }
}

type SuggestionMenuProps = {
    suggestions: PromptTagSearchResult[];
    isLoading: boolean;
    selectedIndex: number;
    menuRef: RefObject<HTMLDivElement | null>;
    menuStyle: CSSProperties;
    onSelect: (suggestion: PromptTagSearchResult) => void;
    onHover: (index: number) => void;
};

function SuggestionMenu({ suggestions, isLoading, selectedIndex, menuRef, menuStyle, onSelect, onHover }: SuggestionMenuProps) {
    return (
        <div ref={menuRef} className="prompt-block-suggestions" style={menuStyle} onMouseDown={stopSuggestionMenuMouseDown} onPointerDown={(event) => event.stopPropagation()}>
            {isLoading ? <div className="prompt-block-suggestions__empty">搜索中...</div> : null}
            {!isLoading && suggestions.length === 0 ? <div className="prompt-block-suggestions__empty">没有匹配的 tag</div> : null}
            {!isLoading
                ? suggestions.map((suggestion, index) => (
                      <button
                          key={`${suggestion.source}-${suggestion.idIndex}-${suggestion.text}`}
                          type="button"
                          className={`prompt-block-suggestion ${index === selectedIndex ? "is-selected" : ""}`}
                          onMouseEnter={() => onHover(index)}
                          onClick={(event) => {
                              event.preventDefault();
                              event.stopPropagation();
                              onSelect(suggestion);
                          }}
                      >
                          <span className="prompt-block-suggestion__main">
                              <span className="prompt-block-suggestion__text">{suggestion.text}</span>
                              {suggestion.translation ? <span className="prompt-block-suggestion__translation">{suggestion.translation}</span> : null}
                          </span>
                          <span className="prompt-block-suggestion__meta">
                              {suggestion.source}
                              {suggestion.count ? ` · ${formatCount(suggestion.count)}` : ""}
                          </span>
                      </button>
                  ))
                : null}
        </div>
    );
}

type MentionMenuProps = {
    references: PromptBlockMentionReference[];
    selectedIndex: number;
    menuRef: RefObject<HTMLDivElement | null>;
    menuStyle: CSSProperties;
    onSelect: (reference: PromptBlockMentionReference) => void;
    onHover: (index: number) => void;
};

function MentionMenu({ references, selectedIndex, menuRef, menuStyle, onSelect, onHover }: MentionMenuProps) {
    return (
        <div ref={menuRef} className="prompt-block-suggestions" style={menuStyle} onMouseDown={stopSuggestionMenuMouseDown} onPointerDown={(event) => event.stopPropagation()}>
            {references.map((reference, index) => (
                <button
                    key={`${reference.nodeId}-${reference.kind}-${reference.label}`}
                    type="button"
                    className={`prompt-block-suggestion ${index === selectedIndex ? "is-selected" : ""}`}
                    onMouseEnter={() => onHover(index)}
                    onClick={(event) => {
                        event.preventDefault();
                        event.stopPropagation();
                        onSelect(reference);
                    }}
                >
                    <span className="prompt-block-suggestion__main">
                        <span className="prompt-block-suggestion__text">{reference.label}</span>
                        <span className="prompt-block-suggestion__translation">{reference.title}</span>
                    </span>
                    <span className="prompt-block-suggestion__meta">@{reference.kind}</span>
                </button>
            ))}
        </div>
    );
}

function stopSuggestionMenuMouseDown(event: MouseEvent<HTMLDivElement>) {
    event.preventDefault();
    event.stopPropagation();
}

function filterMentionReferences(references: PromptBlockMentionReference[], query: string, limit: number) {
    const keyword = query.trim().replace(/^@/, "").toLowerCase();
    return references
        .filter((reference) => reference.active !== false)
        .filter((reference) => {
            if (!keyword) return true;
            return [reference.label, reference.title, reference.kind, reference.text].filter(Boolean).some((value) => String(value).toLowerCase().includes(keyword));
        })
        .slice(0, limit);
}

function scrollSelectedItemIntoView(ref: RefObject<HTMLDivElement | null>, show: boolean, selectedIndex: number) {
    if (!ref.current || !show) return;
    ref.current.querySelectorAll(".prompt-block-suggestion")[selectedIndex]?.scrollIntoView({ block: "nearest" });
}

function formatCount(count: number) {
    if (count >= 1000000) return `${(count / 1000000).toFixed(1)}M`;
    if (count >= 1000) return `${Math.floor(count / 1000)}K`;
    return String(count);
}
