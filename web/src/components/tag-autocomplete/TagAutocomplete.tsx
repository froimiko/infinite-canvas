"use client";

import { useCallback, useEffect, useRef, useState, type CSSProperties, type KeyboardEvent, type RefObject } from "react";

import { searchTags, preloadTagData, TAG_CATEGORY_COLORS, type TagSearchResult } from "@/services/tag-service";
import "./tag-autocomplete.css";

type TagAutocompleteProps = {
    value: string;
    onChange: (value: string) => void;
    placeholder?: string;
    disabled?: boolean;
    className?: string;
    style?: CSSProperties;
    inputType?: "input" | "textarea";
    rows?: number;
    maxRows?: number;
    autoResize?: boolean;
    onKeyDown?: (event: KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>) => void;
    inputRef?: RefObject<HTMLInputElement | HTMLTextAreaElement | null>;
};

type CurrentWord = { word: string; start: number; end: number };

export function TagAutocomplete({ value, onChange, placeholder, disabled, className, style, inputType = "textarea", rows = 1, maxRows = 10, autoResize = true, onKeyDown, inputRef: externalRef }: TagAutocompleteProps) {
    const [suggestions, setSuggestions] = useState<TagSearchResult[]>([]);
    const [isLoading, setIsLoading] = useState(false);
    const [showSuggestions, setShowSuggestions] = useState(false);
    const [selectedIndex, setSelectedIndex] = useState(0);
    const [currentWord, setCurrentWord] = useState<CurrentWord>({ word: "", start: 0, end: 0 });
    const [hasNavigated, setHasNavigated] = useState(false);
    const internalRef = useRef<HTMLInputElement | HTMLTextAreaElement>(null);
    const inputRefToUse = externalRef || internalRef;
    const containerRef = useRef<HTMLDivElement>(null);
    const suggestionsRef = useRef<HTMLDivElement>(null);
    const searchTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    useEffect(() => {
        preloadTagData();
    }, []);

    useEffect(() => {
        if (!autoResize || inputType !== "textarea") return;
        const textarea = inputRefToUse.current as HTMLTextAreaElement | null;
        if (!textarea) return;
        textarea.style.height = "auto";
        textarea.style.overflow = "hidden";
        const computedStyle = window.getComputedStyle(textarea);
        const lineHeight = Number.parseInt(computedStyle.lineHeight) || 20;
        const paddingTop = Number.parseInt(computedStyle.paddingTop) || 0;
        const paddingBottom = Number.parseInt(computedStyle.paddingBottom) || 0;
        const borderTop = Number.parseInt(computedStyle.borderTopWidth) || 0;
        const borderBottom = Number.parseInt(computedStyle.borderBottomWidth) || 0;
        const minHeight = lineHeight * rows + paddingTop + paddingBottom + borderTop + borderBottom;
        const maxHeight = lineHeight * maxRows + paddingTop + paddingBottom + borderTop + borderBottom;
        const contentHeight = textarea.scrollHeight;
        const nextHeight = Math.min(Math.max(contentHeight, minHeight), maxHeight);
        textarea.style.height = `${nextHeight}px`;
        textarea.style.overflow = contentHeight > maxHeight ? "auto" : "hidden";
    }, [autoResize, inputRefToUse, inputType, maxRows, rows, value]);

    const doSearch = useCallback(async (query: string) => {
        if (!query.trim()) {
            setSuggestions([]);
            setShowSuggestions(false);
            return;
        }
        setIsLoading(true);
        try {
            const results = await searchTags(query, 15);
            setSuggestions(results);
            setShowSuggestions(results.length > 0);
            setSelectedIndex(0);
            setHasNavigated(false);
        } finally {
            setIsLoading(false);
        }
    }, []);

    const scheduleSearch = (wordInfo: CurrentWord) => {
        setCurrentWord(wordInfo);
        if (searchTimeoutRef.current) clearTimeout(searchTimeoutRef.current);
        searchTimeoutRef.current = setTimeout(() => void doSearch(wordInfo.word), 150);
    };

    const handleInputChange = (event: React.ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => {
        const nextValue = event.target.value;
        onChange(nextValue);
        scheduleSearch(getCurrentWord(nextValue, event.target.selectionStart || 0));
    };

    const handleSelectionChange = () => {
        const input = inputRefToUse.current;
        if (!input) return;
        const wordInfo = getCurrentWord(value, input.selectionStart || 0);
        if (wordInfo.word !== currentWord.word || wordInfo.start !== currentWord.start) scheduleSearch(wordInfo);
    };

    const selectTag = useCallback(
        (tag: TagSearchResult) => {
            const input = inputRefToUse.current;
            if (!input) return;
            const before = value.slice(0, currentWord.start);
            const after = value.slice(currentWord.end);
            const formattedTagName = tag.name.replace(/_/g, " ");
            const afterContent = after.trimStart();
            let nextValue = before + formattedTagName;
            nextValue += afterContent ? (/^[,，]/.test(afterContent) ? afterContent : `, ${afterContent}`) : ", ";
            onChange(nextValue);
            setShowSuggestions(false);
            setSuggestions([]);
            setHasNavigated(false);
            window.setTimeout(() => {
                input.focus();
                const cursor = before.length + formattedTagName.length + 2;
                input.setSelectionRange(cursor, cursor);
            }, 10);
        },
        [currentWord.end, currentWord.start, inputRefToUse, onChange, value],
    );

    const handleKeyDown = (event: KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>) => {
        if (showSuggestions && suggestions.length) {
            if (event.key === "ArrowDown") {
                event.preventDefault();
                setSelectedIndex((index) => Math.min(index + 1, suggestions.length - 1));
                setHasNavigated(true);
                return;
            }
            if (event.key === "ArrowUp") {
                event.preventDefault();
                setSelectedIndex((index) => Math.max(index - 1, 0));
                setHasNavigated(true);
                return;
            }
            if (event.key === "Tab") {
                event.preventDefault();
                selectTag(suggestions[selectedIndex]);
                return;
            }
            if (event.key === "Enter" && hasNavigated && !event.ctrlKey && !event.metaKey) {
                event.preventDefault();
                selectTag(suggestions[selectedIndex]);
                return;
            }
            if (event.key === "Escape") {
                event.preventDefault();
                setShowSuggestions(false);
                setHasNavigated(false);
                return;
            }
        }
        onKeyDown?.(event);
    };

    useEffect(() => {
        const handleClickOutside = (event: MouseEvent) => {
            if (containerRef.current && !containerRef.current.contains(event.target as Node)) setShowSuggestions(false);
        };
        document.addEventListener("mousedown", handleClickOutside);
        return () => document.removeEventListener("mousedown", handleClickOutside);
    }, []);

    useEffect(() => {
        if (!suggestionsRef.current || !showSuggestions) return;
        suggestionsRef.current.querySelector(".selected")?.scrollIntoView({ block: "nearest" });
    }, [selectedIndex, showSuggestions]);

    const inputProps = {
        value,
        onChange: handleInputChange,
        onKeyDown: handleKeyDown,
        onSelect: handleSelectionChange,
        onClick: handleSelectionChange,
        onMouseDown: (event: React.MouseEvent) => event.stopPropagation(),
        placeholder,
        disabled,
        className,
        style,
    };

    return (
        <div className="tag-autocomplete-container" ref={containerRef}>
            <div className="tag-autocomplete-input-wrapper">
                {inputType === "textarea" ? <textarea {...inputProps} ref={inputRefToUse as RefObject<HTMLTextAreaElement>} rows={rows} /> : <input {...inputProps} ref={inputRefToUse as RefObject<HTMLInputElement>} type="text" />}
            </div>
            {showSuggestions ? (
                <div className="tag-suggestions-dropdown" ref={suggestionsRef} onMouseDown={(event) => event.stopPropagation()}>
                    {isLoading ? (
                        <div className="tag-suggestions-loading">搜索中...</div>
                    ) : (
                        suggestions.map((tag, index) => (
                            <button key={tag.name} type="button" className={`tag-suggestion-item ${index === selectedIndex ? "selected" : ""}`} onMouseEnter={() => setSelectedIndex(index)} onClick={() => selectTag(tag)}>
                                <span className="tag-category-dot" style={{ backgroundColor: TAG_CATEGORY_COLORS[tag.category] || TAG_CATEGORY_COLORS[0] }} />
                                <span className="tag-info">
                                    <span className="tag-name">{tag.name.replace(/_/g, " ")}</span>
                                    {tag.zhName ? <span className="tag-zh">{tag.zhName}</span> : null}
                                </span>
                                <span className="tag-count">{formatCount(tag.count)}</span>
                            </button>
                        ))
                    )}
                </div>
            ) : null}
        </div>
    );
}

export default TagAutocomplete;

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
