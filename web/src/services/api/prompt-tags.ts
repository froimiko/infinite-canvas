import { apiPost } from "@/services/api/request";

export type PromptTagSource = "tags" | "danbooru";

export type PromptTagEntry = {
    idIndex: number;
    source: PromptTagSource;
    text: string;
    translation?: string;
    color?: string;
    colorId?: number;
    hot?: number;
    aliases?: number;
    subgroupId?: number;
    createTime?: number;
    tUuid?: string;
    gUuid?: string;
};

export type PromptTagSearchResult = PromptTagEntry & {
    score: number;
    count: number;
};

export type PromptTagSearchPayload = {
    keyword: string;
    limit?: number;
    sources?: PromptTagSource[];
};

export async function searchPromptTags(payload: PromptTagSearchPayload) {
    return apiPost<PromptTagSearchResult[]>("/api/prompt-tags/search", payload);
}

export async function translatePromptTags(tags: string[]) {
    return apiPost<Record<string, string>>("/api/prompt-tags/translate", { tags });
}
