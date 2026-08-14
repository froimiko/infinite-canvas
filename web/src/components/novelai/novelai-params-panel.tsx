"use client";

import type { CSSProperties } from "react";
import { ArrowLeftRight, ChevronDown, Lock, Unlock } from "lucide-react";

import type { CanvasTheme } from "@/lib/canvas-theme";
import { NOVELAI_NOISE_SCHEDULES, NOVELAI_SAMPLERS } from "./novelai-constants";
import { NovelAIResolutionSelect } from "./novelai-resolution-select";
import { formatNovelAISize, parseNovelAISize } from "./novelai-resolutions";
import type { CanvasNodeMetadata } from "@/app/(user)/canvas/types";
import "./novelai-params-panel.css";

const MAX_COUNT = 15;
const QUICK_COUNT = 10;

type NovelAIParamsPanelProps = {
    metadata: CanvasNodeMetadata;
    theme: CanvasTheme;
    onChange: (patch: Partial<CanvasNodeMetadata>) => void;
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

export function NovelAIParamsPanel({ metadata, theme, onChange }: NovelAIParamsPanelProps) {
    const { width, height } = parseNovelAISize(metadata.size);
    const steps = clamp(Number(metadata.novelAISteps ?? 28), 1, 50);
    const cfgScale = clamp(Number(metadata.novelAICfgScale ?? 6), 1, 25);
    const seed = Number(metadata.novelAISeed ?? -1);
    const seedLocked = Boolean(metadata.naSeedLocked);
    const count = Math.max(1, Math.min(MAX_COUNT, Math.floor(Math.abs(Number(metadata.count)) || 1)));

    const updateDimension = (key: "width" | "height", next: number) => {
        const safe = Math.max(64, Math.round(next || 0));
        onChange({ size: key === "width" ? formatNovelAISize(safe, height) : formatNovelAISize(width, safe) });
    };

    return (
        <div
            className="na-panel"
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
                <div className="na-label">采样器</div>
                <NativeSelect value={metadata.novelAISampler || "k_euler"} options={NOVELAI_SAMPLERS.map((value) => ({ value, label: SAMPLER_LABELS[value] || value }))} onChange={(value) => onChange({ novelAISampler: value })} />
            </div>

            <div className="na-field">
                <div className="na-label">噪声调度</div>
                <NativeSelect
                    value={metadata.novelAINoiseSchedule || "karras"}
                    options={NOVELAI_NOISE_SCHEDULES.map((value) => ({ value, label: NOISE_LABELS[value] || value }))}
                    onChange={(value) => onChange({ novelAINoiseSchedule: value as CanvasNodeMetadata["novelAINoiseSchedule"] })}
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
