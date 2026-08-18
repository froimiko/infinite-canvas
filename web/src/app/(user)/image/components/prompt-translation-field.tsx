"use client";

import { useRef, useState } from "react";
import { App, Button, Tooltip } from "antd";

import { TranslateIcon } from "@/components/prompt-editor-dialog/translate-icon";
import { networkTranslatePromptText } from "@/services/api/prompt-tags";
import { useUserStore } from "@/stores/use-user-store";

type PromptTranslationFieldProps = {
    /** 原文（提示词框内容），翻译按钮以它为输入。 */
    promptText: string;
    /** 译文内容。可编辑，方便交换前微调。 */
    value: string;
    onChange: (value: string) => void;
    /** 与提示词框互换原文/译文。 */
    onSwap: () => void;
    rows?: number;
    disabled?: boolean;
};

/**
 * 「一键翻译」译文区。
 *
 * 两个硬约束，改动前务必确认：
 *  1. 译文**只用于查看和与提示词互换**，绝不能进入任何生成请求体。
 *     生成快照里不要读这个值（`buildRequestSnapshot` 只取提示词框）。
 *  2. 方向固定传 `auto`：后台通常配 en→zh，用户写中文时需要的是 zh→en。
 *     这个判断在后端 `applyPromptTranslationDirection` 里做，前端不传语言码。
 */
export function PromptTranslationField({ promptText, value, onChange, onSwap, rows = 4, disabled = false }: PromptTranslationFieldProps) {
    const { message } = App.useApp();
    const authToken = useUserStore((state) => state.token);
    const [translating, setTranslating] = useState(false);
    // 请求序号：用户连点或改了原文时，丢弃过期响应，避免旧译文覆盖新译文。
    const requestRef = useRef(0);

    const translate = async () => {
        const text = promptText.trim();
        if (!text) {
            message.warning("请先输入提示词");
            return;
        }
        const requestId = requestRef.current + 1;
        requestRef.current = requestId;
        setTranslating(true);
        try {
            const translated = await networkTranslatePromptText(text, authToken || undefined, "auto");
            if (requestRef.current !== requestId) return;
            onChange(translated.trim());
        } catch (error) {
            if (requestRef.current !== requestId) return;
            message.error(error instanceof Error ? error.message : "翻译失败");
        } finally {
            if (requestRef.current === requestId) setTranslating(false);
        }
    };

    const swapDisabled = disabled || (!promptText.trim() && !value.trim());

    return (
        <div>
            <div className="mb-2 flex items-center justify-between gap-3">
                <div className="flex min-w-0 items-center gap-2">
                    <span className="text-base font-semibold">译文</span>
                    <span className="truncate text-xs text-stone-500 dark:text-stone-400">仅供查看，不会发给模型</span>
                </div>
                <div className="flex shrink-0 gap-2">
                    <Button size="small" icon={<TranslateIcon size={14} />} loading={translating} disabled={disabled || !authToken} title={authToken ? "使用后台配置的网络翻译" : "请先登录"} onClick={() => void translate()}>
                        一键翻译
                    </Button>
                    <Tooltip title="与提示词互换原文/译文">
                        <Button size="small" icon={<SwapVerticalIcon />} disabled={swapDisabled} aria-label="与提示词互换原文译文" onClick={onSwap} />
                    </Tooltip>
                </div>
            </div>
            <textarea
                className="thin-scrollbar w-full resize-y rounded-md border border-stone-300 bg-transparent px-3 py-2 text-sm leading-6 outline-none transition placeholder:text-stone-400 focus:border-blue-500 dark:border-stone-700"
                rows={rows}
                value={value}
                disabled={disabled}
                placeholder="点击「一键翻译」查看译文，可用 ↑↓ 按钮与提示词互换"
                onChange={(event) => onChange(event.target.value)}
            />
        </div>
    );
}

/** ↑↓ 交换图标。用内联 SVG 而不是图标库，避免依赖具体图标名是否存在。 */
function SwapVerticalIcon({ size = 14 }: { size?: number }) {
    return (
        <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.8} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M8 20V4" />
            <path d="M4 8l4-4 4 4" />
            <path d="M16 4v16" />
            <path d="M20 16l-4 4-4-4" />
        </svg>
    );
}
