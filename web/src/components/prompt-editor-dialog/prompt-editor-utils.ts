import type { CSSProperties } from "react";

export type CurrentWord = {
    query: string;
    replaceStart: number;
    replaceEnd: number;
};

const WORD_SEPARATOR = /[,，]|\s/;

export function getCurrentWord(value: string, selectionStart: number, selectionEnd = selectionStart): CurrentWord {
    let replaceStart = Math.max(0, Math.min(selectionStart, value.length));
    let replaceEnd = Math.max(replaceStart, Math.min(selectionEnd, value.length));
    while (replaceStart > 0 && !WORD_SEPARATOR.test(value[replaceStart - 1])) replaceStart -= 1;
    while (replaceEnd < value.length && !WORD_SEPARATOR.test(value[replaceEnd])) replaceEnd += 1;
    return { query: value.slice(replaceStart, replaceEnd), replaceStart, replaceEnd };
}

export function replaceCurrentWord(value: string, word: CurrentWord, text: string) {
    const nextChar = value[word.replaceEnd] || "";
    const suffix = !nextChar || !WORD_SEPARATOR.test(nextChar) ? ", " : "";
    const nextValue = `${value.slice(0, word.replaceStart)}${text}${suffix}${value.slice(word.replaceEnd)}`;
    return { value: nextValue, caret: word.replaceStart + text.length + suffix.length };
}

/** 候选层宽度，与 CSS 里 .pe-suggestions 的 width 保持一致。 */
const MENU_WIDTH = 360;
/** 候选层高度上限，同时也是首帧拿不到真实高度时的估算值。 */
const MENU_MAX_HEIGHT = 280;
/** 至少要能露出这么高才认为「这一侧放得下」，否则考虑翻转。 */
const MENU_MIN_USABLE_HEIGHT = 96;
/** 候选层与光标行之间的间隙，也用作贴边留白。 */
const MENU_GAP = 4;

/**
 * 用镜像元素测量 textarea 光标位置，这是浏览器里唯一可靠的方案。
 *
 * 返回的定位会**避让光标所在行**：默认展开在行下方，若下方空间不足则翻转到行上方，
 * 并按所在侧的可用空间收紧 maxHeight。
 *
 * 之所以必须翻转：inline 模式（生图工作台）把编辑器压到 168px 高，
 * 而候选层最高 280px —— 只放下方时光标一旦不在第一行，候选层就会盖住光标那一行。
 *
 * @param menuHeight 候选层实测高度（offsetHeight）。首帧未挂载时传 0，会退回估算值。
 */
export function measureCaretPosition(textarea: HTMLTextAreaElement, wrap: HTMLElement, menuHeight = 0): CSSProperties {
    const position = textarea.selectionStart ?? textarea.value.length;
    const style = window.getComputedStyle(textarea);
    const mirror = document.createElement("div");
    const caret = document.createElement("span");
    const properties = [
        "box-sizing",
        "width",
        "border-top-width",
        "border-right-width",
        "border-bottom-width",
        "border-left-width",
        "padding-top",
        "padding-right",
        "padding-bottom",
        "padding-left",
        "font-family",
        "font-size",
        "font-weight",
        "font-style",
        "letter-spacing",
        "line-height",
        "text-transform",
        "text-indent",
        "white-space",
        "word-spacing",
    ];
    properties.forEach((property) => mirror.style.setProperty(property, style.getPropertyValue(property)));
    mirror.style.position = "absolute";
    mirror.style.visibility = "hidden";
    mirror.style.overflow = "hidden";
    mirror.style.whiteSpace = "pre-wrap";
    mirror.style.overflowWrap = "break-word";
    mirror.style.top = "0";
    mirror.style.left = "0";
    mirror.textContent = textarea.value.slice(0, position);
    caret.textContent = textarea.value.slice(position, position + 1) || ".";
    mirror.appendChild(caret);
    document.body.appendChild(mirror);

    const lineHeight = Number.parseFloat(style.lineHeight) || Number.parseFloat(style.fontSize) * 1.4 || 20;
    const caretOffsetTop = caret.offsetTop;
    const caretOffsetLeft = caret.offsetLeft;
    document.body.removeChild(mirror);

    const containerHeight = wrap.clientHeight;
    const left = Math.max(MENU_GAP, Math.min(caretOffsetLeft - textarea.scrollLeft, wrap.clientWidth - MENU_WIDTH - MENU_GAP));
    // 光标所在行在容器坐标系里的上下边界，候选层要整体避开这个区间。
    const lineTop = caretOffsetTop - textarea.scrollTop;
    const lineBottom = lineTop + lineHeight;

    const spaceBelow = containerHeight - lineBottom - MENU_GAP;
    const spaceAbove = lineTop - MENU_GAP;
    // 首帧候选层还没渲染（menuHeight=0），用上限做估算，宁可提前翻转也不要盖住光标。
    const desiredHeight = Math.min(menuHeight || MENU_MAX_HEIGHT, MENU_MAX_HEIGHT);
    // 下方放得下就放下方；否则只在「上方明显更宽裕」时才翻转，避免两边都窄时来回跳。
    const placeAbove = spaceBelow < Math.min(desiredHeight, MENU_MIN_USABLE_HEIGHT) && spaceAbove > spaceBelow;
    const available = Math.max(MENU_MIN_USABLE_HEIGHT, placeAbove ? spaceAbove : spaceBelow);
    const maxHeight = Math.min(MENU_MAX_HEIGHT, available);
    // 翻转时以行顶为基准上移自身高度；高度未知的首帧会略偏，组件会在挂载后再同步一次。
    const top = placeAbove ? Math.max(MENU_GAP, lineTop - Math.min(desiredHeight, maxHeight) - MENU_GAP) : Math.max(MENU_GAP, lineBottom + MENU_GAP);

    return { left, top, maxHeight };
}
