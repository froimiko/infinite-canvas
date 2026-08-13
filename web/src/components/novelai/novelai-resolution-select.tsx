"use client";

import { useEffect, useRef, useState } from "react";
import { ChevronDown } from "lucide-react";

import { NOVELAI_RESOLUTION_GROUPS, novelAISizeLabel } from "./novelai-resolutions";

type NovelAIResolutionSelectProps = {
    value?: string;
    onChange: (size: string) => void;
};

export function NovelAIResolutionSelect({ value, onChange }: NovelAIResolutionSelectProps) {
    const [open, setOpen] = useState(false);
    const wrapRef = useRef<HTMLDivElement>(null);
    const activeSize = (value || "").trim().toLowerCase();

    useEffect(() => {
        if (!open) return;
        const closeOnOutside = (event: PointerEvent) => {
            if (event.target instanceof Node && wrapRef.current?.contains(event.target)) return;
            setOpen(false);
        };
        window.addEventListener("pointerdown", closeOnOutside, true);
        return () => window.removeEventListener("pointerdown", closeOnOutside, true);
    }, [open]);

    return (
        <div ref={wrapRef} className="relative">
            <button type="button" className="na-select" onClick={() => setOpen((current) => !current)}>
                <span className="truncate">{novelAISizeLabel(value)}</span>
                <ChevronDown className={`size-4 shrink-0 transition ${open ? "rotate-180" : ""}`} />
            </button>
            {open ? (
                <div className="na-dropdown">
                    {NOVELAI_RESOLUTION_GROUPS.map((group) => (
                        <div key={group.label}>
                            <div className="na-dropdown__group">{group.label}</div>
                            {group.items.map((entry) => (
                                <button
                                    key={entry.size}
                                    type="button"
                                    className={`na-dropdown__item ${entry.size === activeSize ? "is-active" : ""}`}
                                    onClick={() => {
                                        onChange(entry.size);
                                        setOpen(false);
                                    }}
                                >
                                    {entry.label}
                                </button>
                            ))}
                        </div>
                    ))}
                </div>
            ) : null}
        </div>
    );
}
