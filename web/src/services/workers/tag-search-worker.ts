type TagEntry = [string, string, string, string, number, number];

type SearchResult = {
    name: string;
    zhName: string;
    category: number;
    count: number;
    score: number;
};

type WorkerMessage = {
    type: "search" | "load";
    id: number;
    query?: string;
    limit?: number;
};

let tags: TagEntry[] = [];
let isLoaded = false;

async function loadData() {
    if (isLoaded) return tags.length;
    const response = await fetch("/novelai-tags/tags_batch_1.tsv");
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const text = await response.text();
    tags = parseTsv(text);
    isLoaded = true;
    return tags.length;
}

function parseTsv(text: string): TagEntry[] {
    return text
        .split(/\r?\n/)
        .map((line): TagEntry | null => {
            const trimmed = line.trim();
            if (!trimmed) return null;
            const parts = trimmed.split("\t");
            const name = (parts[0] || "").trim();
            if (!name) return null;
            const zhName = (parts[1] || "").trim();
            return [name, zhName, "", "", inferCategory(name), inferCount(name)];
        })
        .filter((item): item is TagEntry => Boolean(item));
}

function inferCategory(name: string) {
    if (name.includes("copyright") || name.includes("logo")) return 3;
    if (name.includes("artist")) return 1;
    if (name.includes("boy") || name.includes("girl") || name.includes("character")) return 4;
    if (/^\d{4}$/.test(name) || name.includes("rating") || name.includes("style")) return 5;
    return 0;
}

function inferCount(name: string) {
    return Math.max(1, 100000 - Math.min(90000, name.length * 997));
}

function normalize(value: string) {
    return value.toLowerCase().replace(/_/g, " ").trim();
}

function cleanQuery(query: string) {
    return normalize(query).replace(/[{}[\]()]/g, "").trim();
}

function search(query: string, limit = 20): SearchResult[] {
    const value = cleanQuery(query);
    if (!isLoaded || !value) return [];

    const results: Array<{ index: number; score: number }> = [];
    for (let index = 0; index < tags.length; index++) {
        const tag = tags[index];
        const rawName = tag[0].toLowerCase();
        const displayName = normalize(tag[0]);
        const zhName = normalize(tag[1] || "");
        let score = 0;
        if (rawName === value || displayName === value) score = 1000000;
        else if (rawName.startsWith(value) || displayName.startsWith(value)) score = 800000;
        else if (rawName.includes(value) || displayName.includes(value)) score = 500000;
        else if (zhName && zhName.includes(value)) score = 450000;
        else score = fuzzyScore(displayName, value);
        if (score > 0) results.push({ index, score: score + tag[5] / 1000 });
    }

    return results
        .sort((a, b) => b.score - a.score)
        .slice(0, limit)
        .map(({ index, score }) => {
            const tag = tags[index];
            return {
                name: tag[0],
                zhName: tag[1],
                category: tag[4],
                count: tag[5],
                score,
            };
        });
}

function fuzzyScore(text: string, query: string) {
    if (query.length < 2) return 0;
    let cursor = 0;
    let score = 0;
    for (const char of query) {
        const next = text.indexOf(char, cursor);
        if (next < 0) return 0;
        score += Math.max(1, 100 - (next - cursor) * 5);
        cursor = next + 1;
    }
    return score;
}

self.onmessage = async (event: MessageEvent<WorkerMessage>) => {
    const { type, id, query, limit } = event.data;
    try {
        if (type === "load") {
            const count = await loadData();
            self.postMessage({ type: "loaded", id, tagCount: count, fromCache: false });
            return;
        }
        if (type === "search") {
            if (!isLoaded) await loadData();
            self.postMessage({ type: "searchResult", id, results: search(query || "", limit || 20) });
        }
    } catch (error) {
        self.postMessage({ type: "error", id, error: error instanceof Error ? error.message : String(error) });
    }
};

self.postMessage({ type: "ready" });
