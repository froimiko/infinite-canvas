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

/** 用镜像元素测量 textarea 光标位置，这是浏览器里唯一可靠的方案。 */
export function measureCaretPosition(textarea: HTMLTextAreaElement, wrap: HTMLElement): CSSProperties {
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
    const menuWidth = 360;
    const left = Math.max(4, Math.min(caret.offsetLeft - textarea.scrollLeft, wrap.clientWidth - menuWidth - 4));
    const top = Math.max(4, caret.offsetTop + lineHeight - textarea.scrollTop);
    document.body.removeChild(mirror);
    return { left, top };
}
