export interface TagSearchResult {
    name: string;
    zhName: string;
    category: number;
    count: number;
    score: number;
}

export const TAG_CATEGORY_NAMES: Record<number, string> = {
    0: "通用",
    1: "艺术家",
    3: "版权",
    4: "角色",
    5: "元数据",
};

export const TAG_CATEGORY_COLORS: Record<number, string> = {
    0: "#3b82f6",
    1: "#ef4444",
    3: "#a855f7",
    4: "#22c55e",
    5: "#f59e0b",
};

type PendingRequest = {
    resolve: (value: TagSearchResult[]) => void;
    reject: (reason: Error) => void;
};

let messageId = 0;
let worker: Worker | null = null;
let isLoading = false;
let isLoaded = false;
let loadPromise: Promise<void> | null = null;
const pendingRequests = new Map<number, PendingRequest>();

function initWorker() {
    if (worker) return worker;
    worker = new Worker(new URL("./workers/tag-search-worker.ts", import.meta.url), { type: "module" });
    worker.onmessage = (event: MessageEvent) => {
        const { type, id, results, error } = event.data || {};
        if (type === "ready") return;
        if (type === "loaded") {
            isLoaded = true;
            isLoading = false;
            const pending = pendingRequests.get(id);
            if (pending) {
                pendingRequests.delete(id);
                pending.resolve([]);
            }
            return;
        }
        const pending = pendingRequests.get(id);
        if (!pending) return;
        pendingRequests.delete(id);
        if (type === "error") pending.reject(new Error(error || "标签搜索失败"));
        if (type === "searchResult") pending.resolve(results || []);
    };
    worker.onerror = () => {
        pendingRequests.forEach(({ reject }) => reject(new Error("标签 Worker 错误")));
        pendingRequests.clear();
        isLoading = false;
        isLoaded = false;
    };
    return worker;
}

export async function loadTagData() {
    if (isLoaded) return;
    if (loadPromise) return loadPromise;
    if (isLoading) return;
    isLoading = true;
    const currentWorker = initWorker();
    loadPromise = new Promise<void>((resolve, reject) => {
        const id = messageId++;
        const timeout = window.setTimeout(() => {
            pendingRequests.delete(id);
            reject(new Error("标签数据加载超时"));
        }, 30000);
        pendingRequests.set(id, {
            resolve: () => {
                window.clearTimeout(timeout);
                resolve();
            },
            reject: (error) => {
                window.clearTimeout(timeout);
                reject(error);
            },
        });
        currentWorker.postMessage({ type: "load", id });
    }).finally(() => {
        isLoading = false;
    });
    return loadPromise;
}

export async function searchTags(query: string, limit = 20): Promise<TagSearchResult[]> {
    const trimmed = query.trim();
    if (!trimmed) return [];
    const currentWorker = initWorker();
    if (!isLoaded) {
        try {
            await loadTagData();
        } catch {
            return [];
        }
    }
    return new Promise((resolve) => {
        const id = messageId++;
        const timeout = window.setTimeout(() => {
            pendingRequests.delete(id);
            resolve([]);
        }, 5000);
        pendingRequests.set(id, {
            resolve: (results) => {
                window.clearTimeout(timeout);
                resolve(results);
            },
            reject: () => {
                window.clearTimeout(timeout);
                resolve([]);
            },
        });
        currentWorker.postMessage({ type: "search", id, query: trimmed, limit });
    });
}

export function preloadTagData() {
    void loadTagData().catch(() => undefined);
}

export function destroyWorker() {
    worker?.terminate();
    worker = null;
    isLoaded = false;
    isLoading = false;
    loadPromise = null;
    pendingRequests.clear();
}
