export type NovelAIResolutionItem = {
    label: string;
    size: string;
    width: number;
    height: number;
};

export type NovelAIResolutionGroup = {
    label: string;
    items: NovelAIResolutionItem[];
};

function item(label: string, width: number, height: number): NovelAIResolutionItem {
    return { label: `${label} (${width}×${height})`, size: `${width}x${height}`, width, height };
}

export const NOVELAI_RESOLUTION_GROUPS: NovelAIResolutionGroup[] = [
    { label: "常规", items: [item("竖屏", 832, 1216), item("横屏", 1216, 832), item("方形", 1024, 1024)] },
    { label: "大尺寸", items: [item("竖屏", 1024, 1536), item("横屏", 1536, 1024), item("方形", 1472, 1472)] },
    { label: "壁纸", items: [item("竖屏", 1088, 1920), item("横屏", 1920, 1088)] },
    { label: "小尺寸", items: [item("竖屏", 512, 768), item("横屏", 768, 512), item("方形", 640, 640)] },
];

export const DEFAULT_NOVELAI_SIZE = "832x1216";

export function parseNovelAISize(size?: string) {
    const match = /^(\d+)x(\d+)$/i.exec((size || "").trim());
    if (!match) return { width: 832, height: 1216 };
    return { width: Number(match[1]), height: Number(match[2]) };
}

export function formatNovelAISize(width: number, height: number) {
    return `${Math.max(64, Math.round(width))}x${Math.max(64, Math.round(height))}`;
}

export function findNovelAIResolution(size?: string) {
    const normalized = (size || "").trim().toLowerCase();
    for (const group of NOVELAI_RESOLUTION_GROUPS) {
        const found = group.items.find((entry) => entry.size === normalized);
        if (found) return { group, item: found };
    }
    return null;
}

export function novelAISizeLabel(size?: string) {
    const found = findNovelAIResolution(size);
    if (found) return `${found.group.label} - ${found.item.label}`;
    const { width, height } = parseNovelAISize(size);
    return `自定义 (${width}×${height})`;
}
