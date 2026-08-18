"use client";

import { Sparkles } from "lucide-react";

/**
 * novelai 生图标签页占位。
 *
 * 通用生图与 NovelAI 生图已经拆成两个标签页，NovelAI 侧的控件重做还没开始，
 * 这里先保证标签可切换、不报错，并把用户引到已经做完的画布 NovelAI 节点。
 */
export function NovelAIGenerationPlaceholder() {
    return (
        <div className="mt-6 flex flex-1 flex-col items-center justify-center rounded-lg border border-dashed border-stone-300 px-6 py-16 text-center dark:border-stone-700">
            <Sparkles className="mb-4 size-10 text-stone-400" />
            <div className="text-base font-semibold">NovelAI 生图正在重做</div>
            <p className="mt-2 max-w-sm text-sm text-stone-500 dark:text-stone-400">这个标签页会承载 WeiLin 风格的 NovelAI 提示词控件（积木块提示词、质量词预设、高级参数）。</p>
            <p className="mt-1 max-w-sm text-sm text-stone-500 dark:text-stone-400">现在需要用 NovelAI 出图，请到「我的画布」使用 NovelAI 节点。</p>
        </div>
    );
}
