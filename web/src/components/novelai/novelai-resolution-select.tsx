"use client";

import { useEffect, useRef, useState } from "react";
import type { CSSProperties } from "react";
import { createPortal } from "react-dom";
import { ChevronDown } from "lucide-react";

import type { CanvasTheme } from "@/lib/canvas-theme";
import { NOVELAI_RESOLUTION_GROUPS, novelAISizeLabel } from "./novelai-resolutions";

const DROPDOWN_MARGIN = 12;
const DROPDOWN_GAP = 4;
const DROPDOWN_MAX_HEIGHT = 300;

/**
 * 本组件的下拉被 portal 到 body，不在高级参数面板的 DOM 子树里。
 * 外层面板判断"点击是否发生在面板外"时必须放行带这个标记的元素，
 * 否则 pointerdown 阶段面板就被关掉、下拉随之卸载，click 永远不会触发，
 * 表现为"选了尺寸没反应，而且面板自己关了"。
 */
export const NOVELAI_PORTAL_DROPDOWN_ATTR = "data-na-portal-dropdown";

type NovelAIResolutionSelectProps = {
    value?: string;
    theme: CanvasTheme;
    onChange: (size: string) => void;
};

export function NovelAIResolutionSelect({ value, theme, onChange }: NovelAIResolutionSelectProps) {
    const [open, setOpen] = useState(false);
    const [rect, setRect] = useState<DOMRect | null>(null);
    const triggerRef = useRef<HTMLButtonElement>(null);
    const dropdownRef = useRef<HTMLDivElement>(null);
    const activeSize = (value || "").trim().toLowerCase();

    useEffect(() => {
        if (!open) return;
        const syncPosition = () => setRect(triggerRef.current?.getBoundingClientRect() || null);
        const closeOnOutside = (event: PointerEvent) => {
            const target = event.target;
            if (!(target instanceof Node)) return;
            if (triggerRef.current?.contains(target) || dropdownRef.current?.contains(target)) return;
            setOpen(false);
        };
        syncPosition();
        window.addEventListener("resize", syncPosition);
        window.addEventListener("scroll", syncPosition, true);
        window.addEventListener("pointerdown", closeOnOutside, true);
        return () => {
            window.removeEventListener("resize", syncPosition);
            window.removeEventListener("scroll", syncPosition, true);
            window.removeEventListener("pointerdown", closeOnOutside, true);
        };
    }, [open]);

    return (
        <>
            <button ref={triggerRef} type="button" className="na-select" onClick={() => setOpen((current) => !current)}>
                <span className="truncate">{novelAISizeLabel(value)}</span>
                <ChevronDown className={`size-4 shrink-0 transition ${open ? "rotate-180" : ""}`} />
            </button>
            {open && rect && typeof document !== "undefined"
                ? createPortal(
                      <div ref={dropdownRef} className="na-dropdown-portal" {...{ [NOVELAI_PORTAL_DROPDOWN_ATTR]: "true" }} style={dropdownStyle(rect, theme)} onPointerDown={(event) => event.stopPropagation()} onWheel={(event) => event.stopPropagation()}>
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
                      </div>,
                      document.body,
                  )
                : null}
        </>
    );
}

/** 下拉跟随触发按钮定位，空间不足时向上翻转，并把高度限制在可视区内。 */
function dropdownStyle(rect: DOMRect, theme: CanvasTheme): CSSProperties {
    const spaceBelow = window.innerHeight - rect.bottom - DROPDOWN_GAP - DROPDOWN_MARGIN;
    const spaceAbove = rect.top - DROPDOWN_GAP - DROPDOWN_MARGIN;
    const placeAbove = spaceBelow < 200 && spaceAbove > spaceBelow;
    return {
        position: "fixed",
        zIndex: 1400,
        left: Math.max(DROPDOWN_MARGIN, Math.min(window.innerWidth - rect.width - DROPDOWN_MARGIN, rect.left)),
        width: rect.width,
        maxHeight: Math.min(DROPDOWN_MAX_HEIGHT, Math.max(160, placeAbove ? spaceAbove : spaceBelow)),
        overflowY: "auto",
        padding: "6px 0",
        border: `1px solid ${theme.node.stroke}`,
        borderRadius: 10,
        background: theme.toolbar.panel,
        color: theme.node.text,
        boxShadow: "0 14px 40px rgb(0 0 0 / 0.36)",
        // 下拉被 portal 到 body，需要自带变量，否则继承不到面板作用域。
        "--na-text": theme.node.text,
        "--na-field-bg": theme.node.fill,
        "--na-accent": theme.node.activeStroke,
        ...(placeAbove ? { bottom: window.innerHeight - rect.top + DROPDOWN_GAP } : { top: rect.bottom + DROPDOWN_GAP }),
    } as CSSProperties;
}
