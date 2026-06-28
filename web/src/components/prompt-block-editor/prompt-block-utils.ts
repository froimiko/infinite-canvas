import type { PromptTagSearchResult } from "@/services/api/prompt-tags";
import type { PromptBlockToken, PromptBlockTokenKind } from "./prompt-block-types";

const PROMPT_TOKEN_SEPARATOR = /([,，]|\r\n|\r|\n)/;
let fallbackTokenId = 0;

export function createPromptBlockToken(text: string, patch: Partial<PromptBlockToken> = {}): PromptBlockToken {
    const kind = patch.kind || inferPromptBlockTokenKind(text, patch);
    return {
        id: patch.id || createTokenId(),
        text: kind === "newline" ? "\n" : text.trim(),
        kind,
        translation: patch.translation,
        disabled: patch.disabled === true,
        color: patch.color,
        category: patch.category,
        source: patch.source,
        score: patch.score,
        count: patch.count,
        editing: patch.editing,
        referenceNodeId: patch.referenceNodeId,
        referenceKind: patch.referenceKind,
    };
}

export function parsePromptToTokens(value: string, previousTokens: PromptBlockToken[] = []): PromptBlockToken[] {
    const usedPreviousIndexes = new Set<number>();
    const parsedTokens = value
        .split(PROMPT_TOKEN_SEPARATOR)
        .map((item) => {
            if (isPromptNewline(item)) return reusePreviousToken("\n", "newline", previousTokens, usedPreviousIndexes) || createPromptBlockToken("\n", { kind: "newline" });
            if (item === "," || item === "，") return null;
            const text = item.trim();
            if (!text) return null;
            return reusePreviousToken(text, undefined, previousTokens, usedPreviousIndexes) || createPromptBlockToken(text);
        })
        .filter((token): token is PromptBlockToken => Boolean(token));
    return preserveDisabledPreviousTokens(parsedTokens, previousTokens, usedPreviousIndexes);
}

export function serializeTokensToPrompt(tokens: PromptBlockToken[] = []): string {
    let value = "";
    tokens
        .map(normalizePromptBlockToken)
        .filter((token) => !token.disabled)
        .forEach((token) => {
            if (token.kind === "newline") {
                value = value.replace(/[ \t]+$/g, "");
                if (value) value += "\n";
                return;
            }
            const text = token.text.trim();
            if (!text) return;
            value += !value || value.endsWith("\n") ? text : `, ${text}`;
        });
    return value.trim();
}

export function normalizePromptBlockToken(token: PromptBlockToken): PromptBlockToken {
    return createPromptBlockToken(token.text || "", token);
}

export function normalizePromptBlockTokens(tokens: PromptBlockToken[] = []): PromptBlockToken[] {
    return tokens.map(normalizePromptBlockToken).filter((token) => token.kind === "newline" || Boolean(token.text.trim()));
}

export function reorderPromptBlockTokens(tokens: PromptBlockToken[], fromIndex: number, toIndex: number): PromptBlockToken[] {
    if (fromIndex < 0 || fromIndex >= tokens.length) return tokens;
    const next = [...tokens];
    const [item] = next.splice(fromIndex, 1);
    const safeTarget = Math.max(0, Math.min(toIndex, next.length));
    next.splice(safeTarget, 0, item);
    return next;
}

export function promptTagSuggestionToToken(suggestion: PromptTagSearchResult): PromptBlockToken {
    return createPromptBlockToken(suggestion.text, {
        kind: "tag",
        translation: suggestion.translation,
        color: suggestion.color,
        category: suggestion.source === "danbooru" ? Number(suggestion.colorId) || 0 : 0,
        source: suggestion.source,
        score: suggestion.score,
        count: suggestion.count || suggestion.hot || suggestion.createTime || 0,
    });
}

export function tokenNeedsTranslation(token: PromptBlockToken) {
    const normalized = normalizePromptBlockToken(token);
    if (normalized.kind !== "tag") return false;
    return Boolean(normalized.text.trim()) && !normalized.translation?.trim();
}

function reusePreviousToken(text: string, kind: PromptBlockTokenKind | undefined, previousTokens: PromptBlockToken[], usedPreviousIndexes: Set<number>) {
    const previousIndex = previousTokens.findIndex((token, index) => {
        if (usedPreviousIndexes.has(index)) return false;
        const normalized = normalizePromptBlockToken(token);
        return normalized.text === text && (!kind || normalized.kind === kind);
    });
    if (previousIndex < 0) return null;
    usedPreviousIndexes.add(previousIndex);
    const normalized = normalizePromptBlockToken(previousTokens[previousIndex]);
    return normalized.disabled ? { ...normalized, disabled: false } : normalized;
}

function preserveDisabledPreviousTokens(parsedTokens: PromptBlockToken[], previousTokens: PromptBlockToken[], usedPreviousIndexes: Set<number>) {
    const restored = previousTokens
        .map((token, index) => ({ token: normalizePromptBlockToken(token), index }))
        .filter(({ token, index }) => token.disabled && !usedPreviousIndexes.has(index) && token.kind !== "newline" && Boolean(token.text.trim()))
        .map(({ token }) => token);
    return restored.length ? [...parsedTokens, ...restored] : parsedTokens;
}

function inferPromptBlockTokenKind(text: string, patch: Partial<PromptBlockToken> = {}): PromptBlockTokenKind {
    if (patch.referenceNodeId) return "mention";
    if (isPromptNewline(text)) return "newline";
    const trimmed = text.trim();
    if (isLoraPromptText(trimmed)) return "lora";
    if (/\s/.test(trimmed)) return "text";
    return "tag";
}

function isPromptNewline(text: string) {
    return text === "\n" || text === "\r" || text === "\r\n";
}

function isLoraPromptText(text: string) {
    return /^<\s*(?:wlr|lora|lyco|locon|loha):[^>]+>$/i.test(text) || /^lora:[^,\s]+(?::[-+]?\d*\.?\d+)?$/i.test(text);
}

function createTokenId() {
    if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
        return crypto.randomUUID();
    }
    fallbackTokenId += 1;
    return `prompt-token-${Date.now()}-${fallbackTokenId}`;
}
