import type { CSSProperties, KeyboardEvent } from "react";

import type { PromptTagSearchResult, PromptTagSource } from "@/services/api/prompt-tags";

export type PromptBlockMentionReference = {
    id: string;
    nodeId: string;
    kind: "image" | "video" | "audio" | "text" | string;
    label: string;
    title: string;
    previewUrl?: string;
    text?: string;
    active?: boolean;
};

export type PromptBlockTokenKind = "tag" | "text" | "mention" | "newline" | "lora";

export type PromptBlockToken = {
    id: string;
    text: string;
    kind?: PromptBlockTokenKind;
    translation?: string;
    disabled?: boolean;
    color?: string;
    category?: number;
    source?: PromptTagSource;
    score?: number;
    count?: number;
    editing?: boolean;
    referenceNodeId?: string;
    referenceKind?: string;
};

export type PromptBlockEditorProps = {
    value: string;
    onChange: (value: string) => void;
    tokens?: PromptBlockToken[];
    onTokensChange?: (tokens: PromptBlockToken[]) => void;
    mentionReferences?: PromptBlockMentionReference[];
    onSubmit?: () => void;
    placeholder?: string;
    disabled?: boolean;
    className?: string;
    style?: CSSProperties;
    compact?: boolean;
    rows?: number;
    maxSuggestions?: number;
    onKeyDown?: (event: KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>) => void;
};

export type PromptBlockSuggestion = PromptTagSearchResult;
