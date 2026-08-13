import type { SVGProps } from "react";

/** 翻译图标：左上「文」框 + 右下「A」框 + 双向弧形箭头（对角相接，无需遮挡）。 */
export function TranslateIcon({ size = 14, ...props }: SVGProps<SVGSVGElement> & { size?: number }) {
    return (
        <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" {...props}>
            <rect x="1.5" y="1.5" width="11" height="11" rx="2.5" />
            <rect x="11.5" y="11.5" width="11" height="11" rx="2.5" />
            <path d="M4.3 4.6h5.4M7 4.6v1.1M8.9 5.8c-.5 1.9-1.8 3.2-3.6 3.9M5.8 7.2c.6 1.2 1.6 2.1 2.9 2.5" strokeWidth={1.2} />
            <path d="M14.2 19.6l2.8-5.9 2.8 5.9M15.2 17.8h3.6" strokeWidth={1.2} />
            <path d="M15.2 4.2c2.3.3 4.1 1.9 4.6 4.1M19.9 6.3l.1 2-1.9.3" strokeWidth={1.2} />
            <path d="M8.8 19.8c-2.3-.3-4.1-1.9-4.6-4.1M4.1 17.7l-.1-2 1.9-.3" strokeWidth={1.2} />
        </svg>
    );
}
