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

/**
 * 翻译方向。语言对本身是后台私有设置，这里只允许表达"要不要反向"，
 * 不允许前端传裸语言码（避免把私有配置变成可绕过的入参）。
 *
 * - `config`：按后台配置的 源语言 → 目标语言（缺省行为）
 * - `auto`：文本含中日韩字符时自动反向（例：配置 en→zh，中文输入会走 zh→en）
 * - `reverse`：强制反向
 */
export type PromptTranslationDirection = "config" | "auto" | "reverse";

/** 主动触发的网络翻译，仅供用户点击时调用，不要用于自动翻译。 */
export async function networkTranslatePromptText(text: string, token?: string, direction?: PromptTranslationDirection) {
    return apiPost<string>("/api/prompt-tags/network-translate", direction ? { text, direction } : { text }, token);
}
