"use client";

import type { CSSProperties } from "react";
import { ArrowLeftRight, ChevronDown, Lock, Unlock } from "lucide-react";

import type { CanvasTheme } from "@/lib/canvas-theme";
import { NOVELAI_NOISE_SCHEDULES, NOVELAI_SAMPLERS } from "./novelai-constants";
import { NovelAIResolutionSelect } from "./novelai-resolution-select";
import { formatNovelAISize, parseNovelAISize } from "./novelai-resolutions";
import type { CanvasNodeMetadata } from "@/app/(user)/canvas/types";
import { resolveNovelAIModelId } from "./novelai-presets";
import "./novelai-params-panel.css";

const MAX_COUNT = 15;
const QUICK_COUNT = 10;

type NovelAIParamsPanelProps = {
    metadata: CanvasNodeMetadata;
    theme: CanvasTheme;
    onChange: (patch: Partial<CanvasNodeMetadata>) => void;
    /**
     * popover：画布节点的浮层（固定 300px 宽 + 阴影）。
     * inline：生图工作台内嵌平铺（100% 宽、无阴影、无边框）。
     */
    variant?: "popover" | "inline";
    /**
     * metadata 里没有模型时的兜底（通常传全局 imageModel）。
     *
     * 画布 NovelAI 节点创建时 novelAIModel/model 都是空的（让 ModelPicker 显示全局默认），
     * 不传这个兜底会让 resolveNovelAIModelId 拿到空串并回落 V3，
     * 于是首次打开面板时现代模型判定为 false —— V4/V5 下也不会标注「native 不可用」。
     */
    fallbackModel?: string;
};

const SAMPLER_LABELS: Record<string, string> = {
    k_euler: "Euler",
    k_euler_ancestral: "Euler Ancestral",
    k_dpmpp_2s_ancestral: "DPM++ 2S Ancestral",
    k_dpmpp_2m: "DPM++ 2M",
    k_dpmpp_sde: "DPM++ SDE",
    ddim_v3: "DDIM V3",
};

const NOISE_LABELS: Record<string, string> = {
    native: "Native",
    karras: "Karras",
    exponential: "Exponential",
    polyexponential: "Polyexponential",
};

export function NovelAIParamsPanel({ metadata, theme, onChange, variant = "popover", fallbackModel }: NovelAIParamsPanelProps) {
    const { width, height } = parseNovelAISize(metadata.size);
    const steps = clamp(Number(metadata.novelAISteps ?? 28), 1, 50);
    const cfgScale = clamp(Number(metadata.novelAICfgScale ?? 5), 1, 25);
    const cfgRescale = clamp(Number(metadata.novelAICfgRescale ?? 0), 0, 1);
    const seed = Number(metadata.novelAISeed ?? -1);
    const seedLocked = Boolean(metadata.naSeedLocked);
    const count = Math.max(1, Math.min(MAX_COUNT, Math.floor(Math.abs(Number(metadata.count)) || 1)));
    const modelId = resolveNovelAIModelId(metadata.novelAIModel || metadata.model || fallbackModel);
    // V4 / V4.5 / V5 都使用结构化 Prompt 协议，并共享 native/SMEA/Decrisp 等限制。
    const usesStructuredPrompt = modelId.startsWith("nai-diffusion-4") || modelId.startsWith("nai-diffusion-5");
    // 默认关闭，所以用 === true 判定（!== false 会让 undefined 变成开启）。
    const qualityToggle = metadata.naQualityToggle === true;
    const addOriginalImage = metadata.naAddOriginalImage === true;

    const updateDimension = (key: "width" | "height", next: number) => {
        const safe = Math.max(64, Math.round(next || 0));
        onChange({ size: key === "width" ? formatNovelAISize(safe, height) : formatNovelAISize(width, safe) });
    };

    return (
        <div
            className={variant === "inline" ? "na-panel na-panel--inline" : "na-panel"}
            style={
                {
                    "--na-panel-bg": theme.toolbar.panel,
                    "--na-field-bg": theme.node.fill,
                    "--na-border": theme.node.stroke,
                    "--na-text": theme.node.text,
                    "--na-muted": theme.node.muted,
                    "--na-accent": theme.node.activeStroke,
                    "--na-invert": theme.node.panel,
                } as CSSProperties
            }
            onMouseDown={(event) => event.stopPropagation()}
            onPointerDown={(event) => event.stopPropagation()}
            onWheel={(event) => event.stopPropagation()}
        >
            <div className="na-field">
                <div className="na-label">图像尺寸</div>
                <NovelAIResolutionSelect value={metadata.size} theme={theme} onChange={(size) => onChange({ size })} />
                <div className="na-dimension-row">
                    <label className="na-dimension">
                        <span className="na-dimension__tag">宽度</span>
                        <input type="number" min={64} step={64} value={width} onChange={(event) => updateDimension("width", Number(event.target.value))} />
                    </label>
                    <span className="na-dimension__x">×</span>
                    <label className="na-dimension">
                        <span className="na-dimension__tag">高度</span>
                        <input type="number" min={64} step={64} value={height} onChange={(event) => updateDimension("height", Number(event.target.value))} />
                    </label>
                    <button type="button" className="na-icon-button" title="交换宽高" onClick={() => onChange({ size: formatNovelAISize(height, width) })}>
                        <ArrowLeftRight className="size-4" />
                    </button>
                </div>
            </div>

            <div className="na-field">
                <div className="na-label">采样器{usesStructuredPrompt ? "（V4+ 不支持 DDIM）" : ""}</div>
                <NativeSelect
                    // DDIM 在 V4+ 上会让上游直接返回 500，所以这里连显示都不给：
                    // 后端虽然有兜底回退（mapNovelAISamplerForModel），但让用户选一个
                    // 「选了也不生效」的选项本身就是误导。
                    value={usesStructuredPrompt && metadata.novelAISampler === "ddim_v3" ? "k_euler_ancestral" : metadata.novelAISampler || "k_euler_ancestral"}
                    options={NOVELAI_SAMPLERS.filter((value) => !(usesStructuredPrompt && value === "ddim_v3")).map((value) => ({ value, label: SAMPLER_LABELS[value] || value }))}
                    onChange={(value) => onChange({ novelAISampler: value })}
                />
            </div>

            <div className="na-field">
                <div className="na-label">噪声调度{usesStructuredPrompt ? "（V4+ 不支持 Native）" : ""}</div>
                <NativeSelect
                    value={usesStructuredPrompt && metadata.novelAINoiseSchedule === "native" ? "karras" : metadata.novelAINoiseSchedule || "karras"}
                    options={NOVELAI_NOISE_SCHEDULES.map((value) => ({ value, label: usesStructuredPrompt && value === "native" ? "Native（V4+ 不可用）" : NOISE_LABELS[value] || value }))}
                    onChange={(value) => {
                        if (usesStructuredPrompt && value === "native") return;
                        onChange({ novelAINoiseSchedule: value as CanvasNodeMetadata["novelAINoiseSchedule"] });
                    }}
                />
            </div>

            <div className="na-field">
                <div className="na-label">步数: {steps}</div>
                <input className="na-slider" type="range" min={1} max={50} step={1} value={steps} onChange={(event) => onChange({ novelAISteps: Number(event.target.value) })} />
            </div>

            <div className="na-field">
                <div className="na-label-row">
                    <span className="na-label">CFG Scale: {cfgScale.toFixed(1)}</span>
                    <button type="button" className={`na-pill ${metadata.novelAIVarietyPlus ? "is-active" : ""}`} onClick={() => onChange({ novelAIVarietyPlus: !metadata.novelAIVarietyPlus })}>
                        Variety+
                    </button>
                </div>
                <input className="na-slider" type="range" min={1} max={25} step={0.1} value={cfgScale} onChange={(event) => onChange({ novelAICfgScale: Number(event.target.value) })} />
            </div>

            <div className="na-field">
                <div className="na-label">CFG Rescale: {cfgRescale.toFixed(2)}</div>
                <input className="na-slider" type="range" min={0} max={1} step={0.01} value={cfgRescale} onChange={(event) => onChange({ novelAICfgRescale: Number(event.target.value) })} />
            </div>

            <div className="na-field">
                <div className="na-label">种子</div>
                <div className="na-seed-row">
                    <input
                        className="na-input"
                        type="text"
                        inputMode="numeric"
                        placeholder="随机"
                        value={seed < 0 ? "" : String(seed)}
                        onChange={(event) => {
                            const raw = event.target.value.trim();
                            onChange({ novelAISeed: raw ? Math.max(0, Math.floor(Number(raw) || 0)) : -1 });
                        }}
                    />
                    <button type="button" className={`na-icon-button ${seedLocked ? "is-active" : ""}`} title={seedLocked ? "解锁种子" : "锁定种子"} onClick={() => onChange({ naSeedLocked: !seedLocked })}>
                        {seedLocked ? <Lock className="size-4" /> : <Unlock className="size-4" />}
                    </button>
                </div>
            </div>

            <div className="na-field">
                <div className="na-label-row">
                    <span className="na-label">质量词增强</span>
                    <button type="button" className={`na-pill ${qualityToggle ? "is-active" : ""}`} onClick={() => onChange({ naQualityToggle: !qualityToggle })}>
                        {qualityToggle ? "开" : "关"}
                    </button>
                </div>
            </div>

            <div className="na-field">
                <div className="na-label-row">
                    <span className="na-label">附加原图（img2img）</span>
                    <button type="button" className={`na-pill ${addOriginalImage ? "is-active" : ""}`} onClick={() => onChange({ naAddOriginalImage: !addOriginalImage })}>
                        {addOriginalImage ? "开" : "关"}
                    </button>
                </div>
            </div>

            <div className="na-field">
                <div className="na-label">生成张数</div>
                <div className="na-count-grid">
                    {Array.from({ length: QUICK_COUNT }, (_, index) => index + 1).map((value) => (
                        <button key={value} type="button" className={`na-count-pill ${count === value ? "is-active" : ""}`} onClick={() => onChange({ count: value })}>
                            {value} 张
                        </button>
                    ))}
                    <label className="na-count-input">
                        <input type="number" min={1} max={MAX_COUNT} value={count} onChange={(event) => onChange({ count: Math.max(1, Math.min(MAX_COUNT, Number(event.target.value) || 1)) })} />
                    </label>
                </div>
            </div>
        </div>
    );
}

function NativeSelect({ value, options, onChange }: { value: string; options: { value: string; label: string }[]; onChange: (value: string) => void }) {
    return (
        <div className="na-select-wrap">
            <select className="na-select" value={value} onChange={(event) => onChange(event.target.value)}>
                {options.map((option) => (
                    <option key={option.value} value={option.value}>
                        {option.label}
                    </option>
                ))}
            </select>
            <ChevronDown className="na-select-wrap__icon size-4" />
        </div>
    );
}

function clamp(value: number, min: number, max: number) {
    return Math.max(min, Math.min(max, Number.isFinite(value) ? value : min));
}
