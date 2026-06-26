"use client";

import { type ReactNode } from "react";
import { InputNumber, Select, Switch } from "antd";

import { CharacterPromptsPanel } from "@/components/novelai/character-prompts-panel";
import { NOVELAI_AQT_PRESETS, NOVELAI_MODELS, NOVELAI_NOISE_SCHEDULES, NOVELAI_SAMPLERS, NOVELAI_UC_PRESETS } from "@/components/novelai/novelai-constants";
import type { CanvasTheme } from "@/lib/canvas-theme";
import { normalizeNovelAISettings } from "@/lib/novelai-config";
import type { AiConfig } from "@/stores/use-config-store";

type NovelAISettingsPanelProps = {
    config: AiConfig;
    onConfigChange: <K extends keyof AiConfig>(key: K, value: AiConfig[K]) => void;
    theme: CanvasTheme;
};

export function NovelAISettingsPanel({ config, onConfigChange, theme }: NovelAISettingsPanelProps) {
    const settings = normalizeNovelAISettings(config);
    const setNumber = <K extends keyof AiConfig>(key: K, value: number | string | null) => onConfigChange(key, Number(value ?? 0) as AiConfig[K]);

    return (
        <div className="space-y-3 rounded-2xl border p-3" style={{ borderColor: theme.node.stroke, background: theme.node.panel, color: theme.node.text }}>
            <div className="flex items-start justify-between gap-3">
                <div>
                    <div className="text-sm font-semibold">NovelAI 高级参数</div>
                    <div className="mt-1 text-xs" style={{ color: theme.node.muted }}>
                        仅在非 Gemini 生图接口中发送，关闭后沿用通用参数
                    </div>
                </div>
                <Switch checked={settings.novelAIEnabled} onChange={(checked) => onConfigChange("novelAIEnabled", checked)} />
            </div>

            {settings.novelAIEnabled ? (
                <div className="space-y-3">
                    <div className="grid gap-3 sm:grid-cols-2">
                        <Field label="模型" theme={theme}>
                            <Select value={settings.novelAIModel} options={NOVELAI_MODELS.map((item) => ({ value: item.id, label: item.name }))} onChange={(value) => onConfigChange("novelAIModel", value)} />
                        </Field>
                        <Field label="采样器" theme={theme}>
                            <Select value={settings.novelAISampler} options={NOVELAI_SAMPLERS.map((value) => ({ value, label: value }))} onChange={(value) => onConfigChange("novelAISampler", value)} />
                        </Field>
                        <Field label="Steps" theme={theme}>
                            <InputNumber className="w-full" min={1} max={50} value={settings.novelAISteps} onChange={(value) => setNumber("novelAISteps", value)} />
                        </Field>
                        <Field label="CFG Scale" theme={theme}>
                            <InputNumber className="w-full" min={1} max={25} step={0.1} value={settings.novelAICfgScale} onChange={(value) => setNumber("novelAICfgScale", value)} />
                        </Field>
                        <Field label="Seed（-1 随机）" theme={theme}>
                            <InputNumber className="w-full" min={-1} value={settings.novelAISeed} onChange={(value) => setNumber("novelAISeed", value)} />
                        </Field>
                        <Field label="UC Preset" theme={theme}>
                            <Select value={settings.novelAIUcPreset} options={NOVELAI_UC_PRESETS.map((value) => ({ value, label: value }))} onChange={(value) => onConfigChange("novelAIUcPreset", value)} />
                        </Field>
                        <Field label="CFG Rescale" theme={theme}>
                            <InputNumber className="w-full" min={0} max={1} step={0.01} value={settings.novelAICfgRescale} onChange={(value) => setNumber("novelAICfgRescale", value)} />
                        </Field>
                        <Field label="Noise Schedule" theme={theme}>
                            <Select value={settings.novelAINoiseSchedule} options={NOVELAI_NOISE_SCHEDULES.map((value) => ({ value, label: value }))} onChange={(value) => onConfigChange("novelAINoiseSchedule", value)} />
                        </Field>
                        <Field label="AQT Preset" theme={theme}>
                            <Select value={settings.novelAIAqtPreset} options={NOVELAI_AQT_PRESETS.map((value) => ({ value, label: value }))} onChange={(value) => onConfigChange("novelAIAqtPreset", value)} />
                        </Field>
                    </div>

                    <div className="grid gap-2 sm:grid-cols-2">
                        <SwitchRow label="SMEA" checked={settings.novelAISm} theme={theme} onChange={(checked) => onConfigChange("novelAISm", checked)} />
                        <SwitchRow label="SMEA DYN" checked={settings.novelAISmDyn} theme={theme} onChange={(checked) => onConfigChange("novelAISmDyn", checked)} />
                        <SwitchRow label="Dynamic Thresholding" checked={settings.novelAIDynamicThresholding} theme={theme} onChange={(checked) => onConfigChange("novelAIDynamicThresholding", checked)} />
                        <SwitchRow label="Variety+" checked={settings.novelAIVarietyPlus} theme={theme} onChange={(checked) => onConfigChange("novelAIVarietyPlus", checked)} />
                        <SwitchRow label="Divide Roles" checked={settings.novelAIDivideRoles} theme={theme} onChange={(checked) => onConfigChange("novelAIDivideRoles", checked)} />
                        <SwitchRow label="Auto Positioning" checked={settings.novelAIUseAutoPositioning} theme={theme} onChange={(checked) => onConfigChange("novelAIUseAutoPositioning", checked)} />
                    </div>

                    {settings.novelAIDivideRoles ? (
                        <CharacterPromptsPanel value={settings.novelAICharacterPrompts} useAutoPositioning={settings.novelAIUseAutoPositioning} onChange={(value) => onConfigChange("novelAICharacterPrompts", value)} theme={theme} />
                    ) : null}
                </div>
            ) : null}
        </div>
    );
}

function Field({ label, theme, children }: { label: string; theme: CanvasTheme; children: ReactNode }) {
    return (
        <label className="block min-w-0 space-y-1.5 text-xs font-medium" style={{ color: theme.node.muted }}>
            <span>{label}</span>
            {children}
        </label>
    );
}

function SwitchRow({ label, checked, theme, onChange }: { label: string; checked: boolean; theme: CanvasTheme; onChange: (checked: boolean) => void }) {
    return (
        <div className="flex items-center justify-between gap-3 rounded-xl border px-3 py-2 text-sm" style={{ borderColor: theme.node.stroke, color: theme.node.text }}>
            <span>{label}</span>
            <Switch size="small" checked={checked} onChange={onChange} />
        </div>
    );
}
