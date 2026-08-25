import { apiDelete, apiGet, apiPost, compactApiParams } from "@/services/api/request";
import type { Prompt, PromptListResponse } from "@/services/api/prompts";

export type AdminPromptCategory = {
    category: string;
    name: string;
    description: string;
    file: string;
    githubUrl: string;
    remote: boolean;
};

export type AdminUser = {
    id: string;
    username: string;
    email: string;
    displayName: string;
    avatarUrl: string;
    role: "user" | "admin";
    credits: number;
    affCode: string;
    affCount: number;
    inviterId: string;
    linuxDoId: string;
    status: "active" | "ban";
    lastLoginAt: string;
    createdAt: string;
    updatedAt: string;
};

export type AdminUserListResponse = {
    items: AdminUser[];
    total: number;
};

export type AdminCreditLog = {
    id: string;
    userId: string;
    type: string;
    amount: number;
    balance: number;
    relatedId: string;
    remark: string;
    extra: string;
    createdAt: string;
};

export type AdminCreditLogListResponse = {
    items: AdminCreditLog[];
    total: number;
};

export type AdminUserQuery = {
    keyword?: string;
    page?: number;
    pageSize?: number;
};

export async function fetchAdminUsers(token: string, query: AdminUserQuery = {}) {
    return apiGet<AdminUserListResponse>("/api/admin/users", compactApiParams(query), token);
}

export async function saveAdminUser(token: string, user: Partial<AdminUser> & { password?: string }) {
    return apiPost<AdminUser>("/api/admin/users", user, token);
}

export async function adjustAdminUserCredits(token: string, id: string, credits: number) {
    return apiPost<AdminUser>(`/api/admin/users/${encodeURIComponent(id)}/credits`, { credits }, token);
}

export async function deleteAdminUser(token: string, id: string) {
    return apiDelete<boolean>(`/api/admin/users/${encodeURIComponent(id)}`, token);
}

export async function fetchAdminCreditLogs(token: string, query: AdminUserQuery = {}) {
    return apiGet<AdminCreditLogListResponse>("/api/admin/credit-logs", compactApiParams(query), token);
}

export async function saveAdminCreditLog(token: string, log: Partial<AdminCreditLog>) {
    return apiPost<AdminCreditLog>("/api/admin/credit-logs", log, token);
}

export async function deleteAdminCreditLog(token: string, id: string) {
    return apiDelete<boolean>(`/api/admin/credit-logs/${encodeURIComponent(id)}`, token);
}

export async function fetchAdminPromptCategories(token: string) {
    return apiGet<AdminPromptCategory[]>("/api/admin/prompt-categories", undefined, token);
}

export async function syncAdminPromptCategory(token: string, category: string) {
    return apiPost<AdminPromptCategory[]>("/api/admin/prompt-categories/sync", { category }, token);
}

export type AdminPromptQuery = {
    keyword?: string;
    category?: string;
    tag?: string[];
    page?: number;
    pageSize?: number;
};

export type AdminAsset = {
    id: string;
    title: string;
    type: "text" | "image" | "video";
    coverUrl: string;
    tags: string[];
    category: string;
    description: string;
    content: string;
    url: string;
    createdAt: string;
    updatedAt: string;
};

export type AdminAssetListResponse = {
    items: AdminAsset[];
    tags: string[];
    total: number;
};

export async function fetchAdminPrompts(token: string, query: AdminPromptQuery = {}) {
    return apiGet<PromptListResponse>("/api/admin/prompts", compactApiParams(query), token);
}

export async function saveAdminPrompt(token: string, prompt: Partial<Prompt>) {
    return apiPost<Prompt>("/api/admin/prompts", prompt, token);
}

export async function deleteAdminPrompt(token: string, id: string) {
    return apiDelete<boolean>(`/api/admin/prompts/${encodeURIComponent(id)}`, token);
}

export async function deleteAdminPrompts(token: string, ids: string[]) {
    return apiPost<boolean>("/api/admin/prompts/batch-delete", { ids }, token);
}

export type AdminAssetQuery = {
    keyword?: string;
    type?: string;
    tag?: string[];
    page?: number;
    pageSize?: number;
};

export async function fetchAdminAssets(token: string, query: AdminAssetQuery = {}) {
    return apiGet<AdminAssetListResponse>("/api/admin/assets", compactApiParams(query), token);
}

export async function saveAdminAsset(token: string, asset: Partial<AdminAsset>) {
    return apiPost<AdminAsset>("/api/admin/assets", asset, token);
}

export async function deleteAdminAsset(token: string, id: string) {
    return apiDelete<boolean>(`/api/admin/assets/${encodeURIComponent(id)}`, token);
}

export type AdminModelChannel = {
    protocol: string;
    name: string;
    baseUrl: string;
    apiKey: string;
    models: string[];
    weight: number;
    enabled: boolean;
    remark: string;
    freeGenerationLock?: {
        enabled: boolean;
        maxPixels: number;
        maxSteps: number;
        forceCountOne: boolean;
        disableImg2Img: boolean;
        /** 单张预估耗时（秒），仅在没有历史样本时作为冷启动值。 */
        estimatedSecondsPerImage?: number;
        /** 单用户在该渠道队列中的最大排队张数，防一个人灌满队列。 */
        maxUserQueuedImages?: number;
        /** 全队列绝对上限（张），仅防内存失控，正常不会触发。 */
        maxQueuedImages?: number;

        // ── NAI V5 充能条配额守卫 ──────────────────────────────
        // 官方只对 V5 两个模型（nai-diffusion-5-full / nai-diffusion-5-curated）
        // 把 Opus 免费额度改成了随时间回充的配额池；V4.5 / V4 / V3 仍是无限免费小图，
        // 不受这几项影响。

        /** 是否启用 V5 配额守卫：出图前查充能条余量，不足就拦截，避免误消耗 Anlas。 */
        v5QuotaGuardEnabled?: boolean;
        /**
         * 始终保留不花的张数（默认 1）。
         *
         * 上游的「配额已透支」是事后信号 —— 它变真时最后一张已经花掉了。
         * 多留一张余量，耗尽前的最后一张就永远不会被误消耗。
         */
        v5QuotaReserveImages?: number;
        /** 查询配额失败时是否放行。false（默认）= 拦截保点数；true = 照常出图保可用性。 */
        v5QuotaAllowOnLookupFailure?: boolean;
        /** 配额缓存时长（秒，默认 30），避免每张图都去查一次上游订阅接口。 */
        v5QuotaCacheSeconds?: number;
    };
};

export type AdminPublicModelChannelSettings = {
    availableModels: string[];
    modelCosts: AdminModelCost[];
    defaultModel: string;
    defaultImageModel: string;
    defaultVideoModel: string;
    defaultTextModel: string;
    systemPrompt: string;
    allowCustomChannel: boolean;
};

export type AdminModelCost = {
    model: string;
    credits: number;
};

export type AdminPublicSettings = {
    modelChannel: AdminPublicModelChannelSettings;
    auth: {
        allowRegister: boolean;
        linuxDo: {
            enabled: boolean;
        };
    };
};

export type AdminPromptTagPackageType = "tags" | "danbooru";

export type AdminPromptTagPackage = {
    type: AdminPromptTagPackageType;
    kind?: "file" | "dir" | string;
    path: string;
    name: string;
    sha?: string;
    size?: number;
    downloadUrl?: string;
    installed?: boolean;
    installedAt?: string;
    error?: string;
};

export type AdminPromptTagInstalledPackage = {
    path: string;
    type: AdminPromptTagPackageType;
    sourceOwner: string;
    sourceRepo: string;
    branch: string;
    sha: string;
    size: number;
    installedAt: string;
    updatedAt: string;
    error: string;
};

export type AdminPromptTagDatabaseStatus = {
    enabled: boolean;
    owner: string;
    repo: string;
    branch: string;
    tagCount: number;
    danbooruTagCount: number;
    installedPackages: AdminPromptTagInstalledPackage[];
    lastInstalledAt?: string;
    lastError?: string;
};

export type AdminPromptTagInstallResult = {
    installed: AdminPromptTagInstalledPackage[];
    skipped: AdminPromptTagInstalledPackage[];
    failed: AdminPromptTagInstalledPackage[];
    status: AdminPromptTagDatabaseStatus;
};

export type AdminPrivateSettings = {
    channels: AdminModelChannel[];
    promptSync: {
        enabled: boolean;
        cron: string;
    };
    promptTagDatabase: {
        enabled: boolean;
        owner: string;
        repo: string;
        branch: string;
        packages: AdminPromptTagPackage[];
    };
    promptTranslation: {
        enabled: boolean;
        translator: "library";
        service: "alibaba" | "bing" | "youdao";
        sourceLanguage: string;
        targetLanguage: string;
    };
    auth: {
        linuxDo: {
            clientId: string;
            clientSecret: string;
        };
    };
};

export type AdminSettings = {
    public: AdminPublicSettings;
    private: AdminPrivateSettings;
};

export async function fetchAdminSettings(token: string) {
    return apiGet<AdminSettings>("/api/admin/settings", undefined, token);
}

export async function saveAdminSettings(token: string, settings: AdminSettings) {
    return apiPost<AdminSettings>("/api/admin/settings", settings, token);
}

export type AdminChannelActionRequest = {
    index?: number;
    channel: AdminModelChannel;
    model?: string;
};

export async function fetchChannelModels(token: string, payload: AdminChannelActionRequest) {
    return apiPost<string[]>("/api/admin/settings/channel-models", payload, token);
}
export async function testChannelModel(token: string, payload: AdminChannelActionRequest) {
    return apiPost<string>("/api/admin/settings/channel-test", payload, token);
}

export async function fetchPromptTagDatabaseStatus(token: string) {
    return apiGet<AdminPromptTagDatabaseStatus>("/api/admin/prompt-tag-database/status", undefined, token);
}

export async function fetchPromptTagDatabaseMainTree(token: string) {
    return apiGet<AdminPromptTagPackage[]>("/api/admin/prompt-tag-database/tree/main", undefined, token);
}

export async function fetchPromptTagDatabaseTree(token: string, path: string) {
    return apiPost<AdminPromptTagPackage[]>("/api/admin/prompt-tag-database/tree", { path }, token);
}

export async function installPromptTagDatabasePackages(token: string, payload: { type: AdminPromptTagPackageType; paths: string[] }) {
    return apiPost<AdminPromptTagInstallResult>("/api/admin/prompt-tag-database/install", payload, token);
}

export async function testPromptTranslation(token: string, payload: { text: string; setting: AdminPrivateSettings["promptTranslation"] }) {
    return apiPost<string>("/api/admin/prompt-translation/test", payload, token);
}
