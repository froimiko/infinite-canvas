"use client";

import { forwardRef, useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, MouseEvent, PointerEvent, TextareaHTMLAttributes } from "react";
import { createPortal } from "react-dom";
import { FileText, Image as ImageIcon, Music2, Video } from "lucide-react";

import { canvasThemes } from "@/lib/canvas-theme";
import { searchTags, preloadTagData, TAG_CATEGORY_COLORS, type TagSearchResult } from "@/services/tag-service";
import { useThemeStore } from "@/stores/use-theme-store";
import type { CanvasResourceReference } from "../utils/canvas-resource-references";

type MentionState = {
    start: number;
    query: string;
};

type CurrentWord = { word: string; start: number; end: number };

type Props = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "onChange" | "value"> & {
    value: string;
    references: CanvasResourceReference[];
    onChange: (value: string) => void;
    onSubmit?: () => void;
    containerClassName?: string;
    highlightLabels?: boolean;
    enableTagAutocomplete?: boolean;
};

export const CanvasResourceMentionTextarea = forwardRef<HTMLTextAreaElement, Props>(function CanvasResourceMentionTextarea(
    { value, references, onChange, onSubmit, onKeyDown, className, containerClassName, style, highlightLabels = true, enableTagAutocomplete = false, ...props },
    forwardedRef,
) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const textareaRef = useRef<HTMLTextAreaElement | null>(null);
    const overlayRef = useRef<HTMLDivElement | null>(null);
    const [mention, setMention] = useState<MentionState | null>(null);
    const [activeIndex, setActiveIndex] = useState(0);
    const [hasSelection, setHasSelection] = useState(false);
    const [tagSuggestions, setTagSuggestions] = useState<TagSearchResult[]>([]);
    const [showTagSuggestions, setShowTagSuggestions] = useState(false);
    const [tagSelectedIndex, setTagSelectedIndex] = useState(0);
    const [tagCurrentWord, setTagCurrentWord] = useState<CurrentWord>({ word: "", start: 0, end: 0 });
    const [tagHasNavigated, setTagHasNavigated] = useState(false);
    const tagSearchTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const candidates = useMemo(() => {
        if (!mention) return [];
        const query = mention.query.trim().toLowerCase();
        const activeReferences = references.filter((item) => item.active);
        if (!query) return activeReferences;
        return activeReferences.filter((item) => `${item.label} ${item.title} ${item.kind} ${item.text || ""}`.toLowerCase().includes(query));
    }, [mention, references]);
    const activeLabels = useMemo(() => (highlightLabels ? Array.from(new Set(references.filter((item) => item.active).map((item) => item.label))).sort((a, b) => b.length - a.length) : []), [highlightLabels, references]);

    useEffect(() => {
        if (enableTagAutocomplete) preloadTagData();
    }, [enableTagAutocomplete]);

    useEffect(() => {
        return () => {
            if (tagSearchTimeoutRef.current) clearTimeout(tagSearchTimeoutRef.current);
        };
    }, []);

    const updateValue = (next: string, selectionStart?: number) => {
        onChange(next);
        if (typeof selectionStart !== "number") return;
        requestAnimationFrame(() => {
            textareaRef.current?.focus();
            textareaRef.current?.setSelectionRange(selectionStart, selectionStart);
        });
    };

    const closeTagSuggestions = () => {
        setShowTagSuggestions(false);
        setTagSuggestions([]);
        setTagSelectedIndex(0);
        setTagHasNavigated(false);
        if (tagSearchTimeoutRef.current) {
            clearTimeout(tagSearchTimeoutRef.current);
            tagSearchTimeoutRef.current = null;
        }
    };

    const closeMention = () => {
        setMention(null);
        setActiveIndex(0);
    };

    const searchTagSuggestions = async (query: string) => {
        if (!query.trim()) {
            closeTagSuggestions();
            return;
        }
        const results = await searchTags(query, 15);
        setTagSuggestions(results);
        setShowTagSuggestions(results.length > 0);
        setTagSelectedIndex(0);
        setTagHasNavigated(false);
    };

    const scheduleTagSearch = (wordInfo: CurrentWord) => {
        setTagCurrentWord(wordInfo);
        if (!enableTagAutocomplete || !wordInfo.word.trim()) {
            closeTagSuggestions();
            return;
        }
        if (tagSearchTimeoutRef.current) clearTimeout(tagSearchTimeoutRef.current);
        tagSearchTimeoutRef.current = setTimeout(() => void searchTagSuggestions(wordInfo.word), 150);
    };

    const syncMention = (nextValue: string, cursor: number) => {
        const prefix = nextValue.slice(0, cursor);
        const match = /(^|\s)@([^\s@]*)$/.exec(prefix);
        if (!match || !references.some((item) => item.active)) {
            closeMention();
            return false;
        }
        setMention({ start: cursor - match[2].length - 1, query: match[2] });
        setActiveIndex(0);
        closeTagSuggestions();
        return true;
    };

    const syncTagSuggestions = (nextValue: string, cursor: number, hasActiveMention: boolean) => {
        if (!enableTagAutocomplete || hasActiveMention) {
            closeTagSuggestions();
            return;
        }
        scheduleTagSearch(getCurrentWord(nextValue, cursor));
    };

    const insertReference = (reference: CanvasResourceReference) => {
        if (!mention) return;
        const textarea = textareaRef.current;
        const end = textarea?.selectionStart ?? value.length;
        const insertText = `${reference.label} `;
        const next = `${value.slice(0, mention.start)}${insertText}${value.slice(end)}`;
        closeMention();
        updateValue(next, mention.start + insertText.length);
    };

    const insertTag = useCallback(
        (tag: TagSearchResult) => {
            const textarea = textareaRef.current;
            if (!textarea) return;
            const before = value.slice(0, tagCurrentWord.start);
            const after = value.slice(tagCurrentWord.end);
            const tagText = tag.name.replace(/_/g, " ");
            const afterContent = after.trimStart();
            const next = `${before}${tagText}${afterContent ? (/^[,，]/.test(afterContent) ? afterContent : `, ${afterContent}`) : ", "}`;
            closeTagSuggestions();
            updateValue(next, before.length + tagText.length + 2);
        },
        [tagCurrentWord.end, tagCurrentWord.start, value],
    );

    const syncOverlayScroll = () => {
        if (!overlayRef.current || !textareaRef.current) return;
        overlayRef.current.scrollTop = textareaRef.current.scrollTop;
        overlayRef.current.scrollLeft = textareaRef.current.scrollLeft;
    };

    const updateSelectionState = () => {
        const textarea = textareaRef.current;
        setHasSelection(Boolean(textarea && textarea.selectionStart !== textarea.selectionEnd));
    };

    const showOverlay = Boolean(activeLabels.length && !hasSelection);
    const mergedStyle = {
        ...(style || {}),
        color: showOverlay ? "transparent" : style?.color,
        caretColor: style?.color || theme.node.text,
        ...(showOverlay ? { background: "transparent", backgroundColor: "transparent" } : {}),
    } as CSSProperties;
    const menu = mention && candidates.length && textareaRef.current ? <MentionMenu textarea={textareaRef.current} references={candidates} activeIndex={Math.min(activeIndex, candidates.length - 1)} theme={theme} onSelect={insertReference} /> : null;
    const tagMenu =
        !mention && showTagSuggestions && tagSuggestions.length && textareaRef.current ? (
            <TagSuggestionMenu textarea={textareaRef.current} suggestions={tagSuggestions} activeIndex={Math.min(tagSelectedIndex, tagSuggestions.length - 1)} theme={theme} onActiveChange={setTagSelectedIndex} onSelect={insertTag} />
        ) : null;

    return (
        <div className={`relative h-full w-full ${containerClassName || ""}`}>
            {showOverlay ? (
                <div ref={overlayRef} className={`${className || ""} pointer-events-none absolute inset-0 overflow-hidden whitespace-pre-wrap break-words`} style={{ ...style, color: theme.node.text }}>
                    <MentionHighlightText value={value || props.placeholder?.toString() || ""} labels={activeLabels} placeholder={!value} />
                </div>
            ) : null}
            <textarea
                {...props}
                ref={(node) => {
                    textareaRef.current = node;
                    if (typeof forwardedRef === "function") forwardedRef(node);
                    else if (forwardedRef) forwardedRef.current = node;
                }}
                value={value}
                className={className}
                style={mergedStyle}
                onChange={(event) => {
                    const next = event.target.value;
                    onChange(next);
                    const cursor = event.target.selectionStart;
                    const hasActiveMention = syncMention(next, cursor);
                    syncTagSuggestions(next, cursor, hasActiveMention);
                    requestAnimationFrame(() => {
                        syncOverlayScroll();
                        updateSelectionState();
                    });
                }}
                onSelect={(event) => {
                    updateSelectionState();
                    const cursor = event.currentTarget.selectionStart;
                    const hasActiveMention = syncMention(value, cursor);
                    syncTagSuggestions(value, cursor, hasActiveMention);
                    props.onSelect?.(event);
                }}
                onKeyUp={(event) => {
                    updateSelectionState();
                    props.onKeyUp?.(event);
                }}
                onPointerUp={(event) => {
                    updateSelectionState();
                    props.onPointerUp?.(event);
                }}
                onKeyDown={(event) => {
                    if (mention && candidates.length) {
                        if (event.key === "ArrowDown") {
                            event.preventDefault();
                            setActiveIndex((index) => (index + 1) % candidates.length);
                            return;
                        }
                        if (event.key === "ArrowUp") {
                            event.preventDefault();
                            setActiveIndex((index) => (index - 1 + candidates.length) % candidates.length);
                            return;
                        }
                        if (event.key === "Enter") {
                            event.preventDefault();
                            insertReference(candidates[Math.min(activeIndex, candidates.length - 1)]);
                            return;
                        }
                        if (event.key === "Escape") {
                            event.preventDefault();
                            closeMention();
                            return;
                        }
                    }
                    if (!mention && showTagSuggestions && tagSuggestions.length) {
                        if (event.key === "ArrowDown") {
                            event.preventDefault();
                            setTagSelectedIndex((index) => Math.min(index + 1, tagSuggestions.length - 1));
                            setTagHasNavigated(true);
                            return;
                        }
                        if (event.key === "ArrowUp") {
                            event.preventDefault();
                            setTagSelectedIndex((index) => Math.max(index - 1, 0));
                            setTagHasNavigated(true);
                            return;
                        }
                        if (event.key === "Tab") {
                            event.preventDefault();
                            insertTag(tagSuggestions[Math.min(tagSelectedIndex, tagSuggestions.length - 1)]);
                            return;
                        }
                        if (event.key === "Enter" && tagHasNavigated && !event.ctrlKey && !event.metaKey) {
                            event.preventDefault();
                            insertTag(tagSuggestions[Math.min(tagSelectedIndex, tagSuggestions.length - 1)]);
                            return;
                        }
                        if (event.key === "Escape") {
                            event.preventDefault();
                            closeTagSuggestions();
                            return;
                        }
                    }
                    if (event.key === "Enter" && onSubmit && !event.ctrlKey && !event.metaKey && !event.shiftKey) {
                        event.preventDefault();
                        onSubmit();
                        return;
                    }
                    onKeyDown?.(event);
                }}
                onScroll={(event) => {
                    syncOverlayScroll();
                    props.onScroll?.(event);
                }}
                onBlur={(event) => {
                    setHasSelection(false);
                    window.setTimeout(closeMention, 120);
                    window.setTimeout(closeTagSuggestions, 120);
                    props.onBlur?.(event);
                }}
            />
            {menu}
            {tagMenu}
        </div>
    );
});

function MentionHighlightText({ value, labels, placeholder }: { value: string; labels: string[]; placeholder: boolean }) {
    if (placeholder) return <span className="opacity-45">{value}</span>;
    if (!labels.length) return <>{value}</>;
    const pattern = new RegExp(`(${labels.map(escapeRegExp).join("|")})`, "g");
    return (
        <>
            {value.split(pattern).map((part, index) =>
                labels.includes(part) ? (
                    <span key={`${part}-${index}`} className="rounded-md bg-[#2f80ff]/16 px-1 py-0.5 font-medium text-[#2f80ff] ring-1 ring-[#2f80ff]/24">
                        {part}
                    </span>
                ) : (
                    <span key={`${part}-${index}`}>{part}</span>
                ),
            )}
        </>
    );
}

function MentionMenu({
    textarea,
    references,
    activeIndex,
    theme,
    onSelect,
}: {
    textarea: HTMLTextAreaElement;
    references: CanvasResourceReference[];
    activeIndex: number;
    theme: (typeof canvasThemes)[keyof typeof canvasThemes];
    onSelect: (reference: CanvasResourceReference) => void;
}) {
    const selectedRef = useRef(false);
    const rect = textarea.getBoundingClientRect();
    const boundary = textarea.closest(".ant-modal-content")?.getBoundingClientRect() || { left: 8, top: 8, right: window.innerWidth - 8, bottom: window.innerHeight - 8 };
    const menuWidth = 256;
    const maxMenuHeight = 224;
    const gap = 6;
    const left = clamp(rect.left, boundary.left + 8, boundary.right - menuWidth - 8);
    const showAbove = rect.bottom + gap + maxMenuHeight > boundary.bottom && rect.top - gap - maxMenuHeight >= boundary.top;
    const top = clamp(showAbove ? rect.top - gap - maxMenuHeight : rect.bottom + gap, boundary.top + 8, boundary.bottom - maxMenuHeight - 8);

    const stopCanvasInteraction = (event: PointerEvent | MouseEvent) => {
        event.stopPropagation();
    };
    const selectReference = (reference: CanvasResourceReference) => {
        if (selectedRef.current) return;
        selectedRef.current = true;
        onSelect(reference);
    };

    return createPortal(
        <div
            data-canvas-resource-mention-menu="true"
            className="fixed z-[120] max-h-56 w-64 overflow-y-auto rounded-xl border p-1 shadow-2xl backdrop-blur-md"
            style={{ left, top, background: theme.toolbar.panel, borderColor: theme.toolbar.border, color: theme.node.text }}
            onPointerDown={stopCanvasInteraction}
            onMouseDown={stopCanvasInteraction}
            onClick={(event) => event.stopPropagation()}
        >
            {references.map((reference, index) => (
                <button
                    key={reference.id}
                    type="button"
                    className="flex w-full min-w-0 items-center gap-2 rounded-lg px-2 py-1.5 text-left text-xs transition"
                    style={{ background: index === activeIndex ? theme.toolbar.activeBg : "transparent", color: index === activeIndex ? theme.toolbar.activeText : theme.node.text }}
                    onPointerDown={(event) => {
                        event.preventDefault();
                        event.stopPropagation();
                        selectReference(reference);
                    }}
                    onClick={(event) => {
                        event.preventDefault();
                        event.stopPropagation();
                        selectReference(reference);
                    }}
                >
                    <ReferencePreview reference={reference} />
                    <span className="min-w-0 flex-1">
                        <span className="block font-medium">{reference.label}</span>
                        <span className="block truncate opacity-65">{reference.text || reference.title}</span>
                    </span>
                </button>
            ))}
        </div>,
        document.body,
    );
}

function TagSuggestionMenu({
    textarea,
    suggestions,
    activeIndex,
    theme,
    onActiveChange,
    onSelect,
}: {
    textarea: HTMLTextAreaElement;
    suggestions: TagSearchResult[];
    activeIndex: number;
    theme: (typeof canvasThemes)[keyof typeof canvasThemes];
    onActiveChange: (index: number) => void;
    onSelect: (tag: TagSearchResult) => void;
}) {
    const rect = textarea.getBoundingClientRect();
    const menuWidth = 280;
    const maxMenuHeight = 240;
    const gap = 6;
    const left = clamp(rect.left, 8, window.innerWidth - menuWidth - 8);
    const top = clamp(rect.bottom + gap, 8, window.innerHeight - maxMenuHeight - 8);
    const stopCanvasInteraction = (event: PointerEvent | MouseEvent) => event.stopPropagation();

    return createPortal(
        <div
            data-canvas-tag-suggestions-menu="true"
            className="fixed z-[121] max-h-60 w-[280px] overflow-y-auto rounded-xl border p-1 shadow-2xl backdrop-blur-md"
            style={{ left, top, background: theme.toolbar.panel, borderColor: theme.toolbar.border, color: theme.node.text }}
            onPointerDown={stopCanvasInteraction}
            onMouseDown={stopCanvasInteraction}
            onClick={(event) => event.stopPropagation()}
        >
            {suggestions.map((tag, index) => (
                <button
                    key={tag.name}
                    type="button"
                    className="flex w-full min-w-0 items-center gap-2 rounded-lg px-2 py-1.5 text-left text-xs transition"
                    style={{ background: index === activeIndex ? theme.toolbar.activeBg : "transparent", color: index === activeIndex ? theme.toolbar.activeText : theme.node.text }}
                    onMouseEnter={() => onActiveChange(index)}
                    onPointerDown={(event) => {
                        event.preventDefault();
                        event.stopPropagation();
                        onSelect(tag);
                    }}
                    onClick={(event) => {
                        event.preventDefault();
                        event.stopPropagation();
                        onSelect(tag);
                    }}
                >
                    <span className="size-2 shrink-0 rounded-full" style={{ backgroundColor: TAG_CATEGORY_COLORS[tag.category] || TAG_CATEGORY_COLORS[0] }} />
                    <span className="min-w-0 flex-1">
                        <span className="block truncate font-medium">{tag.name.replace(/_/g, " ")}</span>
                        {tag.zhName ? <span className="block truncate opacity-65">{tag.zhName}</span> : null}
                    </span>
                    <span className="shrink-0 opacity-55">{formatCount(tag.count)}</span>
                </button>
            ))}
        </div>,
        document.body,
    );
}

function ReferencePreview({ reference }: { reference: CanvasResourceReference }) {
    if (reference.kind === "image" && reference.previewUrl) return <img src={reference.previewUrl} alt="" className="size-9 rounded-md object-cover" />;
    if (reference.kind === "video" && reference.previewUrl) return <video src={reference.previewUrl} className="size-9 rounded-md bg-black object-cover" muted preload="metadata" />;
    const Icon = reference.kind === "audio" ? Music2 : reference.kind === "video" ? Video : reference.kind === "image" ? ImageIcon : FileText;
    return (
        <span className="grid size-9 shrink-0 place-items-center rounded-md bg-black/10">
            <Icon className="size-4" />
        </span>
    );
}

function getCurrentWord(text: string, cursorPos: number): CurrentWord {
    const separators = /[,，\s]/;
    let start = cursorPos;
    let end = cursorPos;
    while (start > 0 && !separators.test(text[start - 1])) start--;
    while (end < text.length && !separators.test(text[end])) end++;
    return { word: text.slice(start, end).trim(), start, end };
}

function formatCount(count: number) {
    if (count >= 1000000) return `${(count / 1000000).toFixed(1)}M`;
    if (count >= 1000) return `${Math.floor(count / 1000)}K`;
    return String(count);
}

function clamp(value: number, min: number, max: number) {
    if (max < min) return min;
    return Math.min(Math.max(value, min), max);
}

function escapeRegExp(value: string) {
    return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
