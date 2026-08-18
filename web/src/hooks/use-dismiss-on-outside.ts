"use client";

import { useEffect, type RefObject } from "react";

type DismissTarget = RefObject<HTMLElement | null>;

type UseDismissOnOutsideOptions = {
    /** 只有为 true 时才挂监听（一般传浮层的 open 状态），避免常驻 document 监听。 */
    enabled: boolean;
    /** 视为「内部」的元素集合：点在其中任意一个里面都不算外部。 */
    refs: DismissTarget[];
    /** 判定为外部点击时的回调。 */
    onDismiss: () => void;
};

/**
 * 点击浮层外部即关闭。
 *
 * 两个必须保留的实现细节，改动前请先理解：
 *
 *  1. **capture 阶段**监听。提示词编辑器（含 inline 模式）根节点挂了
 *     `onPointerDown={(e) => e.stopPropagation()}`（画布节点靠它避免拖动画布），
 *     冒泡阶段的 document 监听永远收不到事件，只有捕获阶段能拿到。
 *
 *  2. 监听 `pointerdown` 而不是 `click`。候选项的插入走 `onClick`，
 *     若这里也用 click，两者顺序不稳定会出现「点候选项反而先被判定为外部」。
 *     pointerdown 早于 click，配合候选层自身的 `preventDefault` 不会打断插入。
 */
export function useDismissOnOutside({ enabled, refs, onDismiss }: UseDismissOnOutsideOptions) {
    useEffect(() => {
        if (!enabled) return;
        const handlePointerDown = (event: PointerEvent) => {
            const target = event.target as Node | null;
            if (!target) return;
            // ref 尚未挂载（null）时直接跳过，不能当成「点在外部」。
            const isInside = refs.some((ref) => ref.current?.contains(target));
            if (!isInside) onDismiss();
        };
        document.addEventListener("pointerdown", handlePointerDown, true);
        return () => document.removeEventListener("pointerdown", handlePointerDown, true);
        // refs 数组每次渲染都是新数组，依赖它会导致监听反复重挂；
        // 内部只读 ref.current，重挂也拿不到新值，因此这里刻意不放进依赖。
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [enabled, onDismiss]);
}
