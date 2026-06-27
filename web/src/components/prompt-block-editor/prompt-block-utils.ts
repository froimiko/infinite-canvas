import type { PromptTagSearchResult } from "@/services/api/prompt-tags";
import type { PromptBlockToken } from "./prompt-block-types";

const PROMPT_TOKEN_SEPARATOR = /[,，\n]+/;
let fallbackTokenId = 0;

export function createPromptBlockToken(text: string, patch: Partial<PromptBlockToken> = {}): PromptBlockToken {
    return {
        id: patch.id || createTokenId(),
        text: text.trim(),
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
    return value
        .split(PROMPT_TOKEN_SEPARATOR)
        .map((item) => item.trim())
        .filter(Boolean)
        .map((text) => {
            const previousIndex = previousTokens.findIndex((token, index) => !usedPreviousIndexes.has(index) && token.text === text);
            if (previousIndex >= 0) {
                usedPreviousIndexes.add(previousIndex);
                return normalizePromptBlockToken(previousTokens[previousIndex]);
            }
            return createPromptBlockToken(text);
        });
}

export function serializeTokensToPrompt(tokens: PromptBlockToken[] = []): string {
    return tokens
        .filter((token) => !token.disabled)
        .map((token) => token.text.trim())
        .filter(Boolean)
        .join(", ");
}

export function normalizePromptBlockToken(token: PromptBlockToken): PromptBlockToken {
    return createPromptBlockToken(token.text || "", token);
}

export function normalizePromptBlockTokens(tokens: PromptBlockToken[] = []): PromptBlockToken[] {
    return tokens.map(normalizePromptBlockToken).filter((token) => token.text.trim());
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
        translation: suggestion.translation,
        color: suggestion.color,
        category: suggestion.source === "danbooru" ? Number(suggestion.colorId) || 0 : 0,
        source: suggestion.source,
        score: suggestion.score,
        count: suggestion.count || suggestion.hot || suggestion.createTime || 0,
    });
}

export function tokenNeedsTranslation(token: PromptBlockToken) {
    return Boolean(token.text.trim()) && !token.translation?.trim();
}

function createTokenId() {
    if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
        return crypto.randomUUID();
    }
    fallbackTokenId += 1;
    return `prompt-token-${Date.now()}-${fallbackTokenId}`;
}
