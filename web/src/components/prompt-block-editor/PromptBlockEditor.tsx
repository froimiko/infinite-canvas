"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { DragEvent, KeyboardEvent, MouseEvent, PointerEvent, RefObject } from "react";

import { searchPromptTags, translatePromptTags } from "@/services/api/prompt-tags";
import type { PromptTagSearchResult } from "@/services/api/prompt-tags";
import type { PromptBlockEditorProps, PromptBlockMentionReference, PromptBlockToken } from "./prompt-block-types";
import { createPromptBlockToken, normalizePromptBlockTokens, parsePromptToTokens, promptTagSuggestionToToken, serializeTokensToPrompt, tokenNeedsTranslation } from "./prompt-block-utils";
import "./prompt-block-editor.css";

const DEFAULT_MAX_SUGGESTIONS = 12;
const CLICK_EDIT_DELAY_MS = 180;
const SEARCH_DEBOUNCE_MS = 160;
const TRANSLATE_DEBOUNCE_MS = 260;

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
    const [query, setQuery] = useState("");
    const [suggestions, setSuggestions] = useState<PromptTagSearchResult[]>([]);
    const [selectedSuggestionIndex, setSelectedSuggestionIndex] = useState(0);
    const [showSuggestions, setShowSuggestions] = useState(false);
    const [isSearching, setIsSearching] = useState(false);
    const [selectedMentionIndex, setSelectedMentionIndex] = useState(0);
    const [showMentions, setShowMentions] = useState(false);
    const [editingTokenId, setEditingTokenId] = useState<string | null>(null);
    const [editValue, setEditValue] = useState("");
    const [dragIndex, setDragIndex] = useState<number | null>(null);
    const [dragOverIndex, setDragOverIndex] = useState<number | null>(null);
    const inputRef = useRef<HTMLInputElement>(null);
    const suggestionsRef = useRef<HTMLDivElement>(null);
    const mentionsRef = useRef<HTMLDivElement>(null);
    const editClickTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const searchRequestRef = useRef(0);
    const isControlledTokens = tokens !== undefined;
    const activeTokens = useMemo(() => normalizePromptBlockTokens(isControlledTokens ? tokens : internalTokens), [internalTokens, isControlledTokens, tokens]);
    const serializedValue = useMemo(() => serializeTokensToPrompt(activeTokens), [activeTokens]);
    const editorMinHeight = compact ? undefined : Math.max(64, rows * 32);
    const isMentionQuery = query.trim().startsWith("@");
    const mentionMatches = useMemo(() => filterMentionReferences(mentionReferences, query, maxSuggestions), [mentionReferences, maxSuggestions, query]);

    useEffect(() => {
        if (isControlledTokens) return;
        const nextValue = serializeTokensToPrompt(internalTokens);
        if (nextValue !== value) {
            setInternalTokens(parsePromptToTokens(value, internalTokens));
        }
        // Intentionally depend on value only: internal updates already drive value through applyTokens.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [value, isControlledTokens]);

    useEffect(() => {
        return () => {
            if (editClickTimerRef.current) clearTimeout(editClickTimerRef.current);
        };
    }, []);

    const applyTokens = useCallback(
        (nextTokens: PromptBlockToken[]) => {
            const normalized = normalizePromptBlockTokens(nextTokens);
            if (!isControlledTokens) setInternalTokens(normalized);
            onTokensChange?.(normalized);
            const nextValue = serializeTokensToPrompt(normalized);
            if (nextValue !== value) onChange(nextValue);
        },
        [isControlledTokens, onChange, onTokensChange, value],
    );

    useEffect(() => {
        const keyword = query.trim();
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
    }, [disabled, maxSuggestions, query]);

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
                    if (nextTokens !== activeTokens) applyTokens(nextTokens);
                })
                .catch(() => undefined);
        }, TRANSLATE_DEBOUNCE_MS);
        return () => window.clearTimeout(timeout);
    }, [activeTokens, applyTokens]);

    useEffect(() => {
        scrollSelectedItemIntoView(suggestionsRef, showSuggestions, selectedSuggestionIndex);
    }, [selectedSuggestionIndex, showSuggestions]);

    const insertToken = useCallback(
        (token: PromptBlockToken) => {
            const text = token.text.trim();
            if (!text || disabled) return;
            applyTokens([...activeTokens, { ...token, text, disabled: token.disabled === true }]);
            setQuery("");
            setSuggestions([]);
            setShowSuggestions(false);
            setSelectedSuggestionIndex(0);
            setShowMentions(false);
            setSelectedMentionIndex(0);
            window.setTimeout(() => inputRef.current?.focus(), 0);
        },
        [activeTokens, applyTokens, disabled],
    );

    const insertSuggestion = useCallback((suggestion: PromptTagSearchResult) => insertToken(promptTagSuggestionToToken(suggestion)), [insertToken]);

    const insertMention = useCallback(
        (reference: PromptBlockMentionReference) => {
            insertToken(
                createPromptBlockToken(reference.label, {
                    translation: reference.title,
                    referenceNodeId: reference.nodeId,
                    referenceKind: reference.kind,
                }),
            );
        },
        [insertToken],
    );

    const insertQueryAsToken = useCallback(() => {
        const text = query.trim();
        if (!text) return false;
        insertToken(createPromptBlockToken(text));
        return true;
    }, [insertToken, query]);

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
        const nextTokens = text ? activeTokens.map((token) => (token.id === editingTokenId ? { ...token, text } : token)) : activeTokens.filter((token) => token.id !== editingTokenId);
        setEditingTokenId(null);
        setEditValue("");
        applyTokens(nextTokens);
    }, [activeTokens, applyTokens, editValue, editingTokenId]);

    const cancelEditToken = useCallback(() => {
        setEditingTokenId(null);
        setEditValue("");
    }, []);

    const handleInputKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
        event.stopPropagation();
        if (showMentions && mentionMatches.length) {
            if (event.key === "ArrowDown") {
                event.preventDefault();
                setSelectedMentionIndex((index) => Math.min(index + 1, mentionMatches.length - 1));
                return;
            }
            if (event.key === "ArrowUp") {
                event.preventDefault();
                setSelectedMentionIndex((index) => Math.max(index - 1, 0));
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
                setQuery("");
                return;
            }
        }
        if (showSuggestions && suggestions.length && !isMentionQuery) {
            if (event.key === "ArrowDown") {
                event.preventDefault();
                setSelectedSuggestionIndex((index) => Math.min(index + 1, suggestions.length - 1));
                return;
            }
            if (event.key === "ArrowUp") {
                event.preventDefault();
                setSelectedSuggestionIndex((index) => Math.max(index - 1, 0));
                return;
            }
            if (event.key === "Enter" || event.key === "Tab") {
                event.preventDefault();
                insertSuggestion(suggestions[Math.min(selectedSuggestionIndex, suggestions.length - 1)]);
                return;
            }
            if (event.key === "Escape") {
                event.preventDefault();
                setShowSuggestions(false);
                return;
            }
        }
        if (event.key === "Escape" && isMentionQuery) {
            event.preventDefault();
            setShowMentions(false);
            setQuery("");
            return;
        }
        if (event.key === "Enter" && !event.shiftKey && !event.metaKey && !event.ctrlKey && query.trim() && (!isMentionQuery || !mentionMatches.length)) {
            event.preventDefault();
            insertQueryAsToken();
            return;
        }
        if (event.key === "Enter" && !event.shiftKey && !event.metaKey && !event.ctrlKey && !query.trim() && !showSuggestions && !showMentions) {
            event.preventDefault();
            onSubmit?.();
            return;
        }
        if (event.key === "Backspace" && !query && activeTokens.length && !disabled) {
            event.preventDefault();
            removeToken(activeTokens[activeTokens.length - 1].id);
            return;
        }
        onKeyDown?.(event);
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
            <div className="prompt-block-editor__token-list" aria-label="提示词积木块列表">
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
                <div className="prompt-block-editor__input-wrap">
                    <input
                        ref={inputRef}
                        value={query}
                        disabled={disabled}
                        className="prompt-block-editor__input"
                        placeholder={activeTokens.length ? "继续输入 tag..." : placeholder}
                        onChange={(event) => setQuery(event.target.value)}
                        onKeyDown={handleInputKeyDown}
                        onFocus={() => {
                            if (query.trim().startsWith("@")) setShowMentions(mentionMatches.length > 0);
                            else setShowSuggestions(suggestions.length > 0);
                        }}
                    />
                    {showMentions ? <MentionMenu references={mentionMatches} selectedIndex={selectedMentionIndex} menuRef={mentionsRef} onSelect={insertMention} onHover={setSelectedMentionIndex} /> : null}
                    {!isMentionQuery && (showSuggestions || isSearching) ? (
                        <SuggestionMenu suggestions={suggestions} isLoading={isSearching} selectedIndex={selectedSuggestionIndex} onSelect={insertSuggestion} onHover={setSelectedSuggestionIndex} />
                    ) : null}
                </div>
            </div>
            <input type="hidden" value={serializedValue} readOnly />
        </div>
    );
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
    if (isEditing) {
        return (
            <div className="prompt-block-token is-editing">
                <input className="prompt-block-token__edit-input" value={editValue} autoFocus onChange={(event) => onEditValueChange(event.target.value)} onBlur={onCommitEdit} onKeyDown={onEditKeyDown} />
                <button type="button" className="prompt-block-token__mini-button" onMouseDown={(event) => event.preventDefault()} onClick={onCancelEdit} aria-label="取消编辑">
                    ×
                </button>
            </div>
        );
    }

    return (
        <div
            className={`prompt-block-token ${token.disabled ? "is-disabled" : ""} ${isDragging ? "is-dragging" : ""} ${isDragOver ? "is-drag-over" : ""}`}
            draggable={!disabled}
            onDragStart={(event) => onDragStart(index, event)}
            onDragOver={(event) => onDragOver(index, event)}
            onDrop={(event) => onDrop(index, event)}
            onDragEnd={onDragEnd}
            title="拖拽排序；单击编辑；双击禁用/启用"
        >
            <button type="button" className="prompt-block-token__body" disabled={disabled} onClick={() => onScheduleEdit(token)} onDoubleClick={() => onToggleDisabled(token.id)}>
                <span className="prompt-block-token__text">{token.text}</span>
                {token.translation ? <span className="prompt-block-token__translation">{token.translation}</span> : null}
            </button>
            <button type="button" className="prompt-block-token__remove" disabled={disabled} onClick={() => onRemove(token.id)} aria-label={`删除 ${token.text}`}>
                ×
            </button>
        </div>
    );
}

type SuggestionMenuProps = {
    suggestions: PromptTagSearchResult[];
    isLoading: boolean;
    selectedIndex: number;
    onSelect: (suggestion: PromptTagSearchResult) => void;
    onHover: (index: number) => void;
};

function SuggestionMenu({ suggestions, isLoading, selectedIndex, onSelect, onHover }: SuggestionMenuProps) {
    return (
        <div className="prompt-block-suggestions" onMouseDown={(event) => event.preventDefault()}>
            {isLoading ? <div className="prompt-block-suggestions__empty">搜索中...</div> : null}
            {!isLoading && suggestions.length === 0 ? <div className="prompt-block-suggestions__empty">没有匹配的 tag，按 Enter 添加原文</div> : null}
            {!isLoading
                ? suggestions.map((suggestion, index) => (
                      <button
                          key={`${suggestion.source}-${suggestion.idIndex}-${suggestion.text}`}
                          type="button"
                          className={`prompt-block-suggestion ${index === selectedIndex ? "is-selected" : ""}`}
                          onMouseEnter={() => onHover(index)}
                          onClick={() => onSelect(suggestion)}
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
    onSelect: (reference: PromptBlockMentionReference) => void;
    onHover: (index: number) => void;
};

function MentionMenu({ references, selectedIndex, menuRef, onSelect, onHover }: MentionMenuProps) {
    return (
        <div ref={menuRef} className="prompt-block-suggestions" onMouseDown={(event) => event.preventDefault()}>
            {references.map((reference, index) => (
                <button
                    key={`${reference.nodeId}-${reference.kind}-${reference.label}`}
                    type="button"
                    className={`prompt-block-suggestion ${index === selectedIndex ? "is-selected" : ""}`}
                    onMouseEnter={() => onHover(index)}
                    onClick={() => onSelect(reference)}
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
