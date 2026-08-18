"use client";

import { ImageSettingsPanel } from "@/components/image-settings-panel";
import { ModelPicker } from "@/components/model-picker";
import { canvasThemes } from "@/lib/canvas-theme";
import type { AiConfig } from "@/stores/use-config-store";
import { useThemeStore } from "@/stores/use-theme-store";

export type UpdateAiConfig = <K extends keyof AiConfig>(key: K, value: AiConfig[K]) => void;

type GenerationSettingsProps = {
    config: AiConfig;
    model: string;
    updateConfig: UpdateAiConfig;
    openConfigDialog: (shouldPromptContinue?: boolean) => void;
    /**
     * 是否展示「NovelAI 高级参数」折叠块。
     *
     * 通用生图标签页必须传 false：NovelAI 参数由 novelai 生图标签页负责，
     * 通用生图连请求体都会强制 novelAIEnabled=false，展示出来只会误导用户。
     */
    showNovelAI?: boolean;
};

export function GenerationSettings({ config, model, updateConfig, openConfigDialog, showNovelAI = false }: GenerationSettingsProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];

    return (
        <>
            <label className="col-span-2 block min-w-0 sm:col-span-1">
                <span className="mb-1.5 block text-sm font-semibold sm:mb-2 sm:text-base">模型</span>
                <ModelPicker config={config} value={model} onChange={(value) => updateConfig("imageModel", value)} capability="image" fullWidth onMissingConfig={() => openConfigDialog(false)} />
            </label>
            <div className="col-span-2">
                <ImageSettingsPanel config={config} onConfigChange={(key, value) => updateConfig(key, value)} theme={theme} showTitle={false} className="space-y-4" maxCount={10} showNovelAI={showNovelAI} />
            </div>
        </>
    );
}
