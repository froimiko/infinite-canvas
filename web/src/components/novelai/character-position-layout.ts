import { nanoid } from "nanoid";

import type { NovelAICharacterGender, NovelAICharacterPrompt } from "@/types/image";

import { resolveNovelAIModelId } from "./novelai-presets";

/**
 * 多角色出图的连续坐标布局与角色元数据规则，抄参考项目 Aaalice_NAI_Launcher：
 *  - `lib/data/models/character/character_prompt.dart`
 *    · `CharacterPositionLayout.positionsForCount` / `positionForIndex` / `clampPosition`（第 67-209 行）
 *    · `CharacterPrompt.effectiveGender`（第 276-290 行）
 *    · `CharacterPromptConfig.addCharacter` 的初始提示词、`getNextCharacterName`（第 410-412、462-489 行）
 *  - `lib/presentation/widgets/character/character_position_canvas.dart` 的 `_genderColor`（第 584-593 行）
 *  - `lib/core/constants/model_capabilities.dart` 的 `maxCharacters`（V4/V4.5 = 6，V5 = 32）
 *
 * 坐标一律是连续的 0-1，**不是** 后端历史的 0-4 网格（见 NovelAICharacterPrompt.center 注释）。
 * 参考项目的 CharacterPosition 用 row/column 表达，这里换成前端惯用的 x/y：column → x，row → y。
 *
 * 布局规则要与请求构造共用同一份，否则「画布上显示的位置」和「实际发给 NAI 的 centers」会分叉。
 */

export type CharacterPosition = { x: number; y: number };

const CENTER: CharacterPosition = { x: 0.5, y: 0.5 };

/** 7 人及以上的兜底网格列数，与参考实现的 `const columns = 3` 一致。 */
const FALLBACK_COLUMNS = 3;

/**
 * 按角色数量返回稳定的默认位置（抄 positionsForCount）。
 *
 * 1-6 人是手工调过的固定布局，不是算法生成的，所以只能照抄常量，不要「顺手优化」成公式：
 * 3 人是 0.2/0.5/0.8 而不是 0.25/0.5/0.75，6 人的行位置是 0.25/0.75 而列是 0.2/0.5/0.8，
 * 都与均分网格不同。
 */
export function characterPositionsForCount(count: number): CharacterPosition[] {
    switch (true) {
        case count <= 0:
            return [];
        case count === 1:
            return [{ ...CENTER }];
        case count === 2:
            return [
                { x: 0.25, y: 0.5 },
                { x: 0.75, y: 0.5 },
            ];
        case count === 3:
            return [{ x: 0.2, y: 0.5 }, { ...CENTER }, { x: 0.8, y: 0.5 }];
        case count === 4:
            return [
                { x: 0.25, y: 0.25 },
                { x: 0.75, y: 0.25 },
                { x: 0.25, y: 0.75 },
                { x: 0.75, y: 0.75 },
            ];
        case count === 5:
            return [{ x: 0.2, y: 0.2 }, { x: 0.8, y: 0.2 }, { ...CENTER }, { x: 0.2, y: 0.8 }, { x: 0.8, y: 0.8 }];
        case count === 6:
            return [
                { x: 0.2, y: 0.25 },
                { x: 0.5, y: 0.25 },
                { x: 0.8, y: 0.25 },
                { x: 0.2, y: 0.75 },
                { x: 0.5, y: 0.75 },
                { x: 0.8, y: 0.75 },
            ];
        default: {
            // 7 人以上退化成 3 列网格。分母用 +1 而不是 -1，是为了让锚点落在格心而不是贴边。
            const rows = Math.ceil(count / FALLBACK_COLUMNS);
            return Array.from({ length: count }, (_, index) => ({
                x: ((index % FALLBACK_COLUMNS) + 1) / (FALLBACK_COLUMNS + 1),
                y: (Math.floor(index / FALLBACK_COLUMNS) + 1) / (rows + 1),
            }));
        }
    }
}

/** 取第 index 个角色的默认位置，越界回落画面中心（抄 positionForIndex）。 */
export function characterPositionForIndex(index: number, total: number): CharacterPosition {
    const positions = characterPositionsForCount(total);
    if (index >= 0 && index < positions.length) return { ...positions[index] };
    return { ...CENTER };
}

/**
 * 把坐标钳到 0-1（抄 clampPosition）。
 *
 * 非法值（NaN / undefined / 字符串）一律按 0.5 兜底，避免脏数据把锚点甩到画布外，
 * 或者让 JSON 里出现 NaN 直接把上游请求打成 400。
 */
export function clampCharacterPosition(position: { x?: unknown; y?: unknown } | null | undefined): CharacterPosition {
    return {
        x: clampUnit(position?.x),
        y: clampUnit(position?.y),
    };
}

/**
 * 按提示词首个 tag 推导有效性别（抄 effectiveGender）。
 *
 * UI 的色点 / 锚点配色必须用这个而不是 gender 字段：用户把提示词里的 `girl` 手改成 `boy`
 * 时颜色要跟着变，而 gender 只是「添加角色时点了哪个按钮」的历史值，不会随提示词更新。
 *
 * 语义严格照抄参考实现：只看第一个非空 tag，命中 girl/1girl 是 female、boy/1boy 是 male，
 * **其余直接 return other**，不会继续往后找。所以 `red hair, 1girl` 是 other 而不是 female ——
 * 这是刻意的，配色跟着「打头的那个词」走。
 */
export function resolveEffectiveGender(prompt: string): NovelAICharacterGender {
    for (const tag of String(prompt || "").split(",")) {
        const trimmed = tag.trim();
        if (!trimmed) continue;
        switch (trimmed.toLowerCase()) {
            case "girl":
            case "1girl":
                return "female";
            case "boy":
            case "1boy":
                return "male";
            default:
                return "other";
        }
    }
    return "other";
}

/** 性别配色（抄 _genderColor）：pink-500 / blue-500 / violet-500。 */
export const CHARACTER_GENDER_COLORS: Record<NovelAICharacterGender, string> = {
    female: "#EC4899",
    male: "#3B82F6",
    other: "#8B5CF6",
};

/** 合法性别取值，供归一化做白名单校验。 */
export const CHARACTER_GENDER_OPTIONS: readonly NovelAICharacterGender[] = ["female", "male", "other"];

/**
 * 当前模型的官方角色数量上限，0 表示不支持多角色。
 *
 * 数值抄参考项目 model_capabilities.dart：V5 Full/Curated 都是 32，V4/V4.5 全系是 6。
 * 归一化侧的 MAX_NOVELAI_CHARACTERS 必须 ≥ 这里的最大值，否则 V5 的角色会被静默截断。
 */
export function resolveCharacterLimit(model: string | undefined): number {
    const modelId = resolveNovelAIModelId(model);
    if (modelId.startsWith("nai-diffusion-5")) return 32;
    if (modelId.startsWith("nai-diffusion-4")) return 6;
    return 0;
}

/**
 * 该模型是否支持多角色。
 *
 * 判据（nai-diffusion-4 / nai-diffusion-5 前缀）必须与 novelai-params-panel.tsx:62 的
 * `usesStructuredPrompt` 保持同一套：多角色靠 v4_prompt.char_captions 承载，
 * 两处若判歪一边，就会出现「面板显示角色区、请求里却没有结构化 prompt」的空转。
 */
export function supportsMultiCharacter(model: string | undefined): boolean {
    return resolveCharacterLimit(model) > 0;
}

/** 按性别给的初始提示词（抄 addCharacter）：其他性别刻意留空，不塞任何 tag。 */
const GENDER_INITIAL_PROMPT: Record<NovelAICharacterGender, string> = {
    female: "girl, ",
    male: "boy, ",
    other: "",
};

/**
 * 创建一个新角色。
 *
 * displayName 用 `Character N`（抄 getNextCharacterName）：参考项目界面上就是英文，
 * 示意图里也是 `Character 1`，这里不做中文化，避免和词库导入的名字风格打架。
 * 初始提示词带尾随的 `", "` 也是抄来的 —— 用户接着往后打字就能直接续 tag。
 */
export function createCharacterPrompt(gender: NovelAICharacterGender, index: number, position?: CharacterPosition): NovelAICharacterPrompt {
    return {
        id: nanoid(),
        displayName: `Character ${index + 1}`,
        characterPrompt: GENDER_INITIAL_PROMPT[gender],
        gender,
        enabled: true,
        center: clampCharacterPosition(position ?? characterPositionForIndex(index, index + 1)),
    };
}

function clampUnit(value: unknown): number {
    const number = Number(value);
    if (!Number.isFinite(number)) return 0.5;
    return Math.max(0, Math.min(1, number));
}
