"use client";

import { create } from "zustand";
import { persist } from "zustand/middleware";

import { DEFAULT_NOVELAI_SETTINGS } from "@/components/novelai/novelai-constants";
import { DEFAULT_NOVELAI_QUALITY_PRESET, DEFAULT_NOVELAI_UC_PRESET, normalizeNovelAIQualityPreset, normalizeNovelAIUcPreset, type NovelAIQualityPreset, type NovelAIUcPreset } from "@/components/novelai/novelai-presets";
import { DEFAULT_NOVELAI_SIZE } from "@/components/novelai/novelai-resolutions";
import type { PromptBlockToken } from "@/components/prompt-block-editor/prompt-block-types";
import { normalizeNovelAICharacterPrompts } from "@/lib/novelai-config";
import type { NovelAICharacterPrompt, NovelAINoiseSchedule } from "@/types/image";

/**
 * 生图工作台「novelai生图」标签页的私有状态。
 *
 * 为什么不复用 useConfigStore：
 *  - 通用生图的 size 是 `1:1` / `auto` / `1024x1024` 这类值，NovelAI 是严格的 `832x1216`（64 倍数）。
 *    两边共用一个 config.size 会互相污染，切标签页就把对方的尺寸洗掉。
 *  - count 同理（通用页最多 10，NAI 面板最多 15）。
 *
 * 字段名故意与 CanvasNodeMetadata / NovelAISettings 对齐，
 * 这样可以直接把整个 state 喂给 NovelAIParamsPanel，不需要任何字段映射。
 * 新增参数时请同时确认 novelai-params-panel.tsx 读的是同一个名字。
 */
export type NovelAIWorkbenchState = {
    positivePrompt: string;
    positiveTokens: PromptBlockToken[];
    negativePrompt: string;
    negativeTokens: PromptBlockToken[];
    /** 质量词预设：决定前端按模型注入哪套质量词。 */
    naQualityPreset: NovelAIQualityPreset;
    /** 负面质量词预设：决定前端注入哪套负面词，并同步上报 uc_preset。 */
    naUcPreset: NovelAIUcPreset;
    /** Tag 候选下拉开关（示意图里的「tag候选」勾选框）。 */
    suggestionsEnabled: boolean;

    model: string;
    size: string;
    count: number;

    novelAISampler: string;
    novelAISteps: number;
    novelAICfgScale: number;
    novelAICfgRescale: number;
    novelAINoiseSchedule: NovelAINoiseSchedule;
    novelAISeed: number;
    novelAIVarietyPlus: boolean;
    naSeedLocked: boolean;
    naQualityToggle: boolean;
    naAddOriginalImage: boolean;

    /**
     * 多角色列表（NAI V4+ 的分离出图）。
     *
     * 位置用 NovelAICharacterPrompt.center（连续 0-1），不是画布那套 0-4 网格。
     * 这里只存数据；`selectedCharacterId`、位置弹窗开关这类 UI 瞬态**不要**加进来 ——
     * 它们每次拖拽/点选都会变，进了 persist 就是高频写盘，而且刷新后恢复「弹窗开着」很怪。
     */
    characters: NovelAICharacterPrompt[];
    /**
     * 位置模式：true = 「AI 选择」（由 NAI 自行构图），false = 「自定义」（用 center 坐标）。
     *
     * 对应参考项目 CharacterPromptConfig.globalAiChoice，默认同样是 true。
     * 注意它是全局开关，会盖掉所有角色的位置；切回自定义时 center 数据原样保留。
     */
    charactersAiChoice: boolean;
};

export type NovelAIWorkbenchStore = NovelAIWorkbenchState & {
    patch: (partial: Partial<NovelAIWorkbenchState>) => void;
    reset: () => void;
};

const STORE_KEY = "infinite-canvas:novelai_workbench";

const DEFAULT_STATE: NovelAIWorkbenchState = {
    positivePrompt: "",
    positiveTokens: [],
    negativePrompt: "",
    negativeTokens: [],
    naQualityPreset: DEFAULT_NOVELAI_QUALITY_PRESET,
    naUcPreset: DEFAULT_NOVELAI_UC_PRESET,
    suggestionsEnabled: true,

    model: "",
    size: DEFAULT_NOVELAI_SIZE,
    count: 1,

    novelAISampler: DEFAULT_NOVELAI_SETTINGS.novelAISampler,
    novelAISteps: DEFAULT_NOVELAI_SETTINGS.novelAISteps,
    novelAICfgScale: DEFAULT_NOVELAI_SETTINGS.novelAICfgScale,
    novelAICfgRescale: DEFAULT_NOVELAI_SETTINGS.novelAICfgRescale,
    novelAINoiseSchedule: DEFAULT_NOVELAI_SETTINGS.novelAINoiseSchedule,
    novelAISeed: DEFAULT_NOVELAI_SETTINGS.novelAISeed,
    novelAIVarietyPlus: DEFAULT_NOVELAI_SETTINGS.novelAIVarietyPlus,
    naSeedLocked: false,
    naQualityToggle: DEFAULT_NOVELAI_SETTINGS.novelAIQualityToggle,
    naAddOriginalImage: DEFAULT_NOVELAI_SETTINGS.novelAIAddOriginalImage,

    characters: [],
    charactersAiChoice: true,
};

export const useNovelAIWorkbenchStore = create<NovelAIWorkbenchStore>()(
    persist(
        (set) => ({
            ...DEFAULT_STATE,
            patch: (partial) => set((state) => ({ ...state, ...partial })),
            // 保留 suggestionsEnabled：它是「界面偏好」而不是本次生成的内容，
            // 新建会话时把用户的勾选状态一起清掉很反直觉。
            reset: () => set((state) => ({ ...DEFAULT_STATE, suggestionsEnabled: state.suggestionsEnabled })),
        }),
        {
            // 不显式传 storage：persist 默认就是 localStorage + JSON，
            // 这里的数据都是小体积标量/短字符串，不需要 localforage。
            name: STORE_KEY,
            // 只持久化数据字段，方法不入库。
            partialize: (state) => {
                const { patch, reset, ...data } = state;
                return data;
            },
            // 老版本存档可能缺字段（例如后来新增的 naQualityToggle），
            // 这里与默认值合并，避免读出 undefined 让面板控件变成不可控。
            merge: (persisted, current) => normalizeWorkbenchState({ ...current, ...(persisted as Partial<NovelAIWorkbenchState>) }),
        },
    ),
);

function normalizeWorkbenchState(state: NovelAIWorkbenchStore): NovelAIWorkbenchStore {
    return {
        ...state,
        positivePrompt: String(state.positivePrompt ?? ""),
        negativePrompt: String(state.negativePrompt ?? ""),
        positiveTokens: Array.isArray(state.positiveTokens) ? state.positiveTokens : [],
        negativeTokens: Array.isArray(state.negativeTokens) ? state.negativeTokens : [],
        naQualityPreset: normalizeNovelAIQualityPreset(state.naQualityPreset),
        naUcPreset: normalizeNovelAIUcPreset(state.naUcPreset),
        suggestionsEnabled: state.suggestionsEnabled !== false,
        size: /^\d+x\d+$/i.test(String(state.size || "")) ? String(state.size) : DEFAULT_NOVELAI_SIZE,
        count: Math.max(1, Math.min(15, Math.floor(Number(state.count) || 1))),
        naQualityToggle: state.naQualityToggle === true,
        naAddOriginalImage: state.naAddOriginalImage === true,
        // 复用 normalizeNovelAICharacterPrompts：id/gender/enabled/center 的兜底规则
        // 与请求链路共用一份，避免存档里的角色和实际发出去的对不上。
        characters: normalizeNovelAICharacterPrompts(state.characters),
        // 默认「AI 选择」，所以用 !== false（老存档没这个字段时要读成 true）。
        charactersAiChoice: state.charactersAiChoice !== false,
    };
}
