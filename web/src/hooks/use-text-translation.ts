"use client";

import { useRef, useState } from "react";
import { App } from "antd";

import { networkTranslatePromptText } from "@/services/api/prompt-tags";
import { useUserStore } from "@/stores/use-user-store";

/**
 * 文本翻译 hook。
 *
 * 抽出来给两处共用：生图工作台的译文区（PromptTranslationField）与画布文本节点。
 * 只负责「发请求 + loading + 丢弃过期响应 + 报错」，**不决定译文存到哪里** ——
 * 由调用方拿到返回值自己落地（组件 state / 节点 metadata）。
 *
 * 方向固定传 auto：后台通常配 en→zh，用户写中文时需要的是 zh→en，
 * 这个判断在后端 applyPromptTranslationDirection 里做，前端不传语言码。
 */
export function useTextTranslation() {
    const { message } = App.useApp();
    const authToken = useUserStore((state) => state.token);
    const [translating, setTranslating] = useState(false);
    // 请求序号：用户连点或改了原文时丢弃过期响应，避免旧译文覆盖新译文。
    const requestRef = useRef(0);

    /** 翻译文本。返回 null 表示空输入 / 失败 / 响应已过期，调用方不要落地。 */
    const translate = async (text: string): Promise<string | null> => {
        const value = text.trim();
        if (!value) {
            message.warning("没有可翻译的内容");
            return null;
        }
        const requestId = requestRef.current + 1;
        requestRef.current = requestId;
        setTranslating(true);
        try {
            const translated = await networkTranslatePromptText(value, authToken || undefined, "auto");
            if (requestRef.current !== requestId) return null;
            return translated.trim() || null;
        } catch (error) {
            if (requestRef.current !== requestId) return null;
            message.error(error instanceof Error ? error.message : "翻译失败");
            return null;
        } finally {
            if (requestRef.current === requestId) setTranslating(false);
        }
    };

    return { translate, translating, canTranslate: Boolean(authToken) };
}
