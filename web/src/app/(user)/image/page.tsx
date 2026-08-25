"use client";

import { CheckSquare, Download, FolderPlus, History, ImagePlus, LoaderCircle, PenLine, Plus, SlidersHorizontal, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { App, Button, Checkbox, Drawer, Empty, Image, Modal, Segmented, Tag, Tooltip, Typography } from "antd";
import localforage from "localforage";
import { saveAs } from "file-saver";

import { PromptSelectDialog } from "@/components/prompts/prompt-select-dialog";
import { AssetPickerModal, type InsertAssetPayload } from "@/app/(user)/canvas/components/asset-picker-modal";
import { countEffectiveNovelAICharacters, normalizeNovelAICharacterPrompts, normalizeNovelAISettings } from "@/lib/novelai-config";
import { useConfigStore, useEffectiveConfig, type AiConfig } from "@/stores/use-config-store";
import { nanoid } from "nanoid";
import { formatBytes, formatDuration, getDataUrlByteSize, readImageMeta } from "@/lib/image-utils";
import { requestEdit, requestGeneration, type ImageQueueProgress } from "@/services/api/image";
import { deleteStoredImages, resolveImageUrl, uploadImage } from "@/services/image-storage";
import { useAssetStore } from "@/stores/use-asset-store";
import type { NovelAISettings, ReferenceImage } from "@/types/image";
import type { PromptBlockToken } from "@/components/prompt-block-editor/prompt-block-types";
import { supportsMultiCharacter } from "@/components/novelai/character-position-layout";
import { applyNovelAIQualityTags, applyNovelAIUcPreset, normalizeNovelAIQualityPreset, normalizeNovelAIUcPreset, novelAIUcPresetApiName, resolveNovelAIModelId } from "@/components/novelai/novelai-presets";
import { useNovelAIWorkbenchStore } from "@/stores/use-novelai-workbench-store";
import { GeneralGenerationPanel } from "./components/general-generation-panel";
import { GenerationSettings } from "./components/generation-settings";
import { NovelAIGenerationPanel } from "./components/novelai-generation-panel";

type GeneratedImage = {
    id: string;
    dataUrl: string;
    storageKey?: string;
    durationMs: number;
    width: number;
    height: number;
    bytes: number;
    mimeType?: string;
};

type GenerationResult = {
    id: string;
    status: "pending" | "success" | "failed";
    image?: GeneratedImage;
    error?: string;
};

/** 生成记录所属标签页。缺省（老记录）按通用生图处理。 */
type GenerationMode = "general" | "novelai";

type GenerationLog = {
    id: string;
    createdAt: number;
    /** 记录来自哪个标签页，决定恢复时要不要回写 NovelAI 参数。 */
    mode: GenerationMode;
    title: string;
    prompt: string;
    /** 一键翻译的译文。只用于恢复界面显示，从不参与生成。 */
    promptTranslation?: string;
    /** 历史字段：积木块提示词。通用生图已改纯文本框，仅为兼容老记录保留。 */
    promptTokens?: PromptBlockToken[];
    /** 历史字段：负面提示词（NAI）。通用生图已移除该输入，仅为兼容老记录保留。 */
    negativePrompt?: string;
    negativePromptTokens?: PromptBlockToken[];
    time: string;
    model: string;
    config: GenerationLogConfig;
    references: ReferenceImage[];
    durationMs: number;
    successCount: number;
    failCount: number;
    imageCount: number;
    size: string;
    quality: string;
    status: "成功" | "失败";
    images: GeneratedImage[];
    thumbnails: string[];
};

type GenerationLogConfig = Pick<AiConfig, "model" | "imageModel" | "quality" | "size" | "count"> & NovelAISettings;

const LOG_STORE_KEY = "infinite-canvas:image_generation_logs";
const RESULT_ACTION_BUTTON_CLASS = "min-w-0 px-1.5 [&_.ant-btn-icon]:shrink-0 [&>span:last-child]:min-w-0 [&>span:last-child]:truncate";
const WORKBENCH_TABS = [
    { value: "general", label: "通用生图" },
    { value: "novelai", label: "novelai生图" },
] as const;
const logStore = localforage.createInstance({ name: "infinite-canvas", storeName: "image_generation_logs" });

export default function ImagePage() {
    const { message } = App.useApp();
    const fileInputRef = useRef<HTMLInputElement>(null);
    const config = useConfigStore((state) => state.config);
    const effectiveConfig = useEffectiveConfig();
    const updateConfig = useConfigStore((state) => state.updateConfig);
    const isAiConfigReady = useConfigStore((state) => state.isAiConfigReady);
    const openConfigDialog = useConfigStore((state) => state.openConfigDialog);
    const addAsset = useAssetStore((state) => state.addAsset);
    const [activeTab, setActiveTab] = useState<GenerationMode>("general");
    const [novelAIField, setNovelAIField] = useState<"positive" | "negative">("positive");
    const novelAI = useNovelAIWorkbenchStore();
    const [prompt, setPrompt] = useState("");
    const [promptTranslation, setPromptTranslation] = useState("");
    const [references, setReferences] = useState<ReferenceImage[]>([]);
    const [results, setResults] = useState<GenerationResult[]>([]);
    // NovelAI SSE 排队进度，按结果槽位索引存。只有 NovelAI 流式路径会填充。
    const [queueProgress, setQueueProgress] = useState<Record<number, ImageQueueProgress>>({});
    const [logs, setLogs] = useState<GenerationLog[]>([]);
    const [running, setRunning] = useState(false);
    const [logsOpen, setLogsOpen] = useState(false);
    const [settingsOpen, setSettingsOpen] = useState(false);
    const [promptDialogOpen, setPromptDialogOpen] = useState(false);
    const [assetPickerOpen, setAssetPickerOpen] = useState(false);
    const [startedAt, setStartedAt] = useState(0);
    const [elapsedMs, setElapsedMs] = useState(0);
    const [selectedLogIds, setSelectedLogIds] = useState<string[]>([]);
    const [previewLog, setPreviewLog] = useState<GenerationLog | null>(null);
    const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);

    const isNovelAITab = activeTab === "novelai";
    // NovelAI 标签页用自己的模型与张数（store），通用页仍走全局 config。
    const model = isNovelAITab ? novelAI.model || effectiveConfig.imageModel || effectiveConfig.model : effectiveConfig.imageModel || effectiveConfig.model;
    const canGenerate = Boolean((isNovelAITab ? novelAI.positivePrompt : prompt).trim());
    const generationCount = isNovelAITab ? Math.max(1, Math.min(15, Number(novelAI.count) || 1)) : Math.max(1, Math.min(10, Number(config.count) || 1));

    useEffect(() => {
        if (!running || !startedAt) return;
        const timer = window.setInterval(() => setElapsedMs(performance.now() - startedAt), 1000);
        return () => window.clearInterval(timer);
    }, [running, startedAt]);

    useEffect(() => {
        void refreshLogs();
    }, []);

    const addReferences = async (files?: FileList | null) => {
        const imageFiles = Array.from(files || []).filter((file) => file.type.startsWith("image/"));
        const nextReferences = await Promise.all(
            imageFiles.map(async (file) => {
                const image = await uploadImage(file);
                return { id: nanoid(), name: file.name, type: image.mimeType, dataUrl: image.url, storageKey: image.storageKey };
            }),
        );
        setReferences((value) => [...value, ...nextReferences]);
    };

    const addReferencesFromClipboard = async () => {
        try {
            const items = await navigator.clipboard.read();
            const blobs = await Promise.all(items.flatMap((item) => item.types.filter((type) => type.startsWith("image/")).map((type) => item.getType(type))));
            if (!blobs.length) {
                message.error("剪切板里没有可读取的图片");
                return;
            }
            const nextReferences = await Promise.all(
                blobs.map(async (blob, index) => {
                    const image = await uploadImage(blob);
                    return { id: nanoid(), name: `clipboard-${index + 1}.png`, type: image.mimeType, dataUrl: image.url, storageKey: image.storageKey };
                }),
            );
            setReferences((value) => [...value, ...nextReferences]);
            message.success(`已读取 ${nextReferences.length} 张参考图`);
        } catch {
            message.error("剪切板里没有可读取的图片");
        }
    };

    /** 与提示词框互换原文/译文。译文只是查看用，交换后进模型的永远是提示词框里的内容。 */
    const swapPromptTranslation = () => {
        setPrompt(promptTranslation);
        setPromptTranslation(prompt);
    };

    const generate = async () => {
        const text = (isNovelAITab ? novelAI.positivePrompt : prompt).trim();
        if (!text) {
            message.error("请输入生图提示词");
            return;
        }
        if (!isAiConfigReady(effectiveConfig, model)) {
            message.warning("请先完成配置");
            openConfigDialog(true);
            return;
        }

        const snapshot = buildRequestSnapshot();
        if (!snapshot) return;

        setElapsedMs(0);
        setRunning(true);
        setPreviewLog(null);
        setResults(Array.from({ length: generationCount }, () => ({ id: nanoid(), status: "pending" })));
        const batchStartedAt = performance.now();
        setStartedAt(batchStartedAt);

        const result: PromiseSettledResult<GeneratedImage>[] = [];
        for (let index = 0; index < generationCount; index++) {
            try {
                result.push({ status: "fulfilled", value: await runGenerationSlot(index, snapshot) });
            } catch (reason) {
                result.push({ status: "rejected", reason });
            }
        }
        const successImages = result.filter((item): item is PromiseFulfilledResult<GeneratedImage> => item.status === "fulfilled").map((item) => item.value);
        const successCount = successImages.length;
        const failCount = generationCount - successCount;
        const failed = result.find((item): item is PromiseRejectedResult => item.status === "rejected");

        try {
            const logImages = await Promise.all(
                successImages.map(async (image) => {
                    const stored = await uploadImage(image.dataUrl);
                    return { ...image, dataUrl: stored.url, storageKey: stored.storageKey, width: stored.width, height: stored.height, bytes: stored.bytes, mimeType: stored.mimeType };
                }),
            );
            saveLog(
                buildLog({
                    mode: activeTab,
                    prompt: snapshot.text,
                    promptTranslation: isNovelAITab ? "" : promptTranslation,
                    negativePrompt: snapshot.negativePrompt,
                    model,
                    config: { ...snapshot.config, count: String(generationCount) },
                    references: snapshot.references,
                    durationMs: performance.now() - batchStartedAt,
                    successCount,
                    failCount,
                    status: successCount ? "成功" : "失败",
                    images: logImages,
                }),
            );
            successCount ? message.success("图片已生成") : message.error(failed?.reason instanceof Error ? failed.reason.message : "生成失败");
        } finally {
            setRunning(false);
        }
    };

    const downloadImage = (image: GeneratedImage, index: number) => {
        saveAs(image.dataUrl, `image-${index + 1}.png`);
    };

    const addResultToReferences = async (image: GeneratedImage, index: number) => {
        const stored = await uploadImage(image.dataUrl);
        setReferences((value) => [...value, { id: nanoid(), name: `result-${index + 1}.png`, type: stored.mimeType, dataUrl: stored.url, storageKey: stored.storageKey }]);
        message.success("已加入参考图");
    };

    const saveResultToAssets = async (image: GeneratedImage, index: number) => {
        const stored = await uploadImage(image.dataUrl);
        addAsset({
            kind: "image",
            title: `生成结果 ${index + 1}`,
            coverUrl: stored.url,
            tags: [],
            source: "生图工作台",
            data: { dataUrl: stored.url, storageKey: stored.storageKey, width: stored.width, height: stored.height, bytes: stored.bytes, mimeType: stored.mimeType },
            metadata: { source: "image-page", prompt },
        });
        message.success("已加入我的素材");
    };

    const insertPickedAsset = async (payload: InsertAssetPayload) => {
        if (payload.kind === "text") {
            setPrompt(payload.content);
        } else if (payload.kind === "image") {
            const stored = await uploadImage(payload.dataUrl);
            setReferences((value) => [...value, { id: nanoid(), name: payload.title, type: stored.mimeType, dataUrl: stored.url, storageKey: stored.storageKey }]);
        } else {
            message.warning("生图工作台只能使用文本或图片素材");
        }
        setAssetPickerOpen(false);
    };

    const createSession = () => {
        setPrompt("");
        setPromptTranslation("");
        novelAI.reset();
        setReferences([]);
        setResults([]);
        setElapsedMs(0);
        setStartedAt(0);
        setSelectedLogIds([]);
        setPreviewLog(null);
    };

    const deleteSelectedLogs = () => {
        const imageKeys = logs.filter((log) => selectedLogIds.includes(log.id)).flatMap((log) => log.images.map((image) => image.storageKey).filter((key): key is string => Boolean(key)));
        void Promise.all([deleteStoredImages(imageKeys), ...selectedLogIds.map((id) => logStore.removeItem(id))]).then(refreshLogs);
        if (previewLog && selectedLogIds.includes(previewLog.id)) {
            setPreviewLog(null);
            setResults([]);
        }
        setSelectedLogIds([]);
        setDeleteConfirmOpen(false);
    };

    const saveLog = (log: GenerationLog) => {
        void logStore.setItem(log.id, serializeLog(log)).then(refreshLogs);
    };

    const refreshLogs = async () => setLogs(await readStoredLogs());

    const previewGenerationLog = async (log: GenerationLog) => {
        setPreviewLog(log);
        setLogsOpen(false);
        setActiveTab(log.mode);
        setReferences(log.references || []);
        // 通用记录才回写全局 config：NovelAI 的模型/尺寸/张数属于 NAI 标签页私有状态，
        // 写进全局会把通用页的尺寸洗成 832x1216 这类 NAI 专用值。
        if (log.mode === "general") {
            setPrompt(log.prompt);
            setPromptTranslation(log.promptTranslation || "");
            if (log.config.imageModel || log.model) updateConfig("imageModel", log.config.imageModel || log.model);
            if (log.config.quality) updateConfig("quality", log.config.quality);
            if (log.config.size) updateConfig("size", log.config.size);
            if (log.config.count) updateConfig("count", log.config.count);
        }
        // NovelAI 记录恢复到 NAI 标签页自己的 store。
        if (log.mode === "novelai") {
            novelAI.patch({
                model: log.config.imageModel || log.model || "",
                size: log.config.size || novelAI.size,
                count: Math.max(1, Math.min(15, Number(log.config.count) || 1)),
                novelAISampler: log.config.novelAISampler,
                novelAISteps: log.config.novelAISteps,
                novelAICfgScale: log.config.novelAICfgScale,
                novelAICfgRescale: log.config.novelAICfgRescale,
                novelAINoiseSchedule: log.config.novelAINoiseSchedule,
                novelAISeed: log.config.novelAISeed,
                novelAIVarietyPlus: log.config.novelAIVarietyPlus,
                naQualityToggle: log.config.novelAIQualityToggle,
                naAddOriginalImage: log.config.novelAIAddOriginalImage,
                // 提示词里已经含注入好的质量词，直接回填即可；tokens 交给编辑器重新解析。
                positivePrompt: log.prompt,
                positiveTokens: [],
                negativePrompt: log.negativePrompt || "",
                negativeTokens: [],
                // 角色快照一并恢复。老记录（阶段4 之前）的 config 里没有角色字段，
                // normalizeLogConfig 会补成空数组，恢复后角色区为空 —— 与旧行为一致。
                // 回写前过一遍归一化：老存档的角色可能缺 id / center / enabled，
                // 直接回填会让 React key 退化成下标、位置画布读不到坐标。
                characters: normalizeNovelAICharacterPrompts(log.config.novelAICharacterPrompts),
                charactersAiChoice: log.config.novelAIUseAutoPositioning,
            });
        }
        setResults(log.images.map((image) => ({ id: image.id, status: "success", image })));
    };

    const buildRequestSnapshot = () => {
        const rawPrompt = (isNovelAITab ? novelAI.positivePrompt : prompt).trim();
        if (!rawPrompt) {
            message.error("请输入生图提示词");
            return null;
        }
        if (!isAiConfigReady(effectiveConfig, model)) {
            message.warning("请先完成配置");
            openConfigDialog(true);
            return null;
        }
        if (isNovelAITab) return buildNovelAISnapshot(rawPrompt);
        // 通用生图必须显式关掉 NovelAI：全局 config 里可能残留 novelAIEnabled=true，
        // 那会让 requestGeneration/requestEdit 走 NovelAI 分支（尺寸规则与参数体完全不同）。
        return { text: rawPrompt, negativePrompt: "", config: { ...effectiveConfig, model, count: "1", novelAIEnabled: false }, references: [...references] };
    };

    /**
     * NovelAI 快照。
     *
     * 与画布 NovelAI 节点走的是同一套注入规则（canvas-client-page 里那段），改一处必须同步另一处：
     *  - 质量词按当前模型追加到正面提示词末尾（applyNovelAIQualityTags）。
     *    角色提示词**不**注入质量词：参考项目也只给主提示词加，每个角色再各加一份
     *    会让质量词在 v4_prompt 里重复出现，稀释角色描述本身的权重；
     *  - 负面预设词拼在用户负面词前面（applyNovelAIUcPreset）
     *  - uc_preset 必须跟随「负面质量词」下拉，否则上游还会按默认 Heavy 再叠一份负面词
     *  - novelai_model 交给 buildNovelAIRequestParameters 里的 modelOptionName 剥渠道前缀
     */
    const buildNovelAISnapshot = (rawPrompt: string) => {
        const presetModel = resolveNovelAIModelId(model);
        const text = applyNovelAIQualityTags(rawPrompt, presetModel, normalizeNovelAIQualityPreset(novelAI.naQualityPreset));
        const negativePrompt = applyNovelAIUcPreset(novelAI.negativePrompt, presetModel, normalizeNovelAIUcPreset(novelAI.naUcPreset));
        // 多角色字段只在 V4+ 模型下发。V3 没有结构化 prompt（v4_prompt / char_captions），
        // 发 divide_roles 是无意义参数，还会让后端误入 DivideRoles 分支。
        // supportsMultiCharacter 内部走 resolveNovelAIModelId，天然处理 `渠道id::` 前缀。
        const multiCharacter = supportsMultiCharacter(model);
        // 有效角色数复用 novelai-config 的口径（countEffectiveNovelAICharacters）：
        // buildNovelAICharacterPromptPayload 过滤的就是「enabled !== false 且正负提示词非空」的角色，
        // divide_roles 的判定必须与它完全一致 —— 否则会出现「声称分离角色、char_captions 却为空」
        // 的自相矛盾请求（后端虽有 len(charCaptions) > 0 兜底，前端不该发这种参数）。
        // 这里故意不复制角色列表：把整个 novelAI.characters 原样放进快照，
        // 过滤交给请求层与历史恢复各自做，避免快照里存两份口径不同的副本。
        const effectiveCharacters = multiCharacter ? countEffectiveNovelAICharacters(novelAI.characters) : 0;
        const config: AiConfig = {
            ...effectiveConfig,
            model,
            imageModel: model,
            size: novelAI.size,
            count: "1",
            novelAIEnabled: true,
            novelAIModel: model,
            novelAISampler: novelAI.novelAISampler,
            novelAISteps: novelAI.novelAISteps,
            novelAICfgScale: novelAI.novelAICfgScale,
            novelAICfgRescale: novelAI.novelAICfgRescale,
            novelAINoiseSchedule: novelAI.novelAINoiseSchedule,
            novelAISeed: novelAI.novelAISeed,
            novelAIVarietyPlus: novelAI.novelAIVarietyPlus,
            novelAIUcPreset: novelAIUcPresetApiName(normalizeNovelAIUcPreset(novelAI.naUcPreset)),
            novelAIQualityToggle: novelAI.naQualityToggle,
            novelAIAddOriginalImage: novelAI.naAddOriginalImage,
            // 角色三件套：完整列表（含禁用/空占位，供历史记录恢复 UI）、分离开关、位置模式。
            // novelAIDivideRoles 用「有效角色数 ≥ 1」而不是 characters.length ≥ 1：
            // 列表里可能全是 enabled=false 或空提示词的占位角色，那种情况下 payload 会是空的，
            // 开着 divide_roles 只会让后端进「分离但无 char_caption」的分支。
            novelAICharacterPrompts: multiCharacter ? novelAI.characters : [],
            novelAIDivideRoles: multiCharacter && effectiveCharacters > 0,
            // charactersAiChoice=true 是「AI 选择位置」模式，直传 use_auto_positioning 语义。
            // 是否存在有效角色只影响 divide_roles；位置模式是用户选择，历史记录也要原样保存。
            novelAIUseAutoPositioning: multiCharacter ? novelAI.charactersAiChoice : false,
        };
        return { text, negativePrompt, config, references: [...references] };
    };

    const runGenerationSlot = async (index: number, snapshot: { text: string; negativePrompt: string; config: AiConfig; references: ReferenceImage[] }) => {
        const itemStartedAt = performance.now();
        try {
            // 通用生图的 negativePrompt 恒为空串（译文区只作展示）；NovelAI 标签页才会带负面词。
            // 有参考图就走 requestEdit（img2img），此时「附加原图」开关才真正生效。
            // onQueueProgress 只有 NovelAI 的 SSE 路径会回调，用于显示「前方还有 N 张图」。
            const requestOptions = {
                ...(snapshot.negativePrompt ? { negativePrompt: snapshot.negativePrompt } : {}),
                onQueueProgress: (progress: ImageQueueProgress) => setQueueProgress((value) => ({ ...value, [index]: progress })),
            };
            const result = snapshot.references.length ? await requestEdit(snapshot.config, snapshot.text, snapshot.references, undefined, requestOptions) : await requestGeneration(snapshot.config, snapshot.text, requestOptions);
            const image = result[0];
            if (!image) throw new Error("接口没有返回图片");
            const meta = await readImageMeta(image.dataUrl);
            const nextImage = { id: image.id, dataUrl: image.dataUrl, durationMs: performance.now() - itemStartedAt, width: meta.width, height: meta.height, bytes: getDataUrlByteSize(image.dataUrl) };
            setResults((value) => updateResultAt(value, index, { status: "success", image: nextImage }));
            return nextImage;
        } catch (error) {
            setResults((value) => updateResultAt(value, index, { status: "failed", error: error instanceof Error ? error.message : "生成失败" }));
            throw error;
        } finally {
            // 无论成败都要清掉，否则重试时会残留上一轮的排队文案。
            setQueueProgress((value) => {
                const next = { ...value };
                delete next[index];
                return next;
            });
        }
    };

    const retryResult = (index: number) => {
        const snapshot = buildRequestSnapshot();
        if (!snapshot) return;
        setPreviewLog(null);
        setResults((value) => updateResultAt(value, index, { status: "pending", error: undefined, image: undefined }));
        void runGenerationSlot(index, snapshot).catch(() => {});
    };

    return (
        <div className="flex h-full flex-col overflow-hidden bg-stone-50 text-stone-900 dark:bg-stone-950 dark:text-stone-100">
            <main className="grid min-h-0 flex-1 grid-cols-1 gap-3 overflow-y-auto p-3 lg:grid-cols-[300px_minmax(0,1fr)] lg:overflow-hidden xl:grid-cols-[320px_minmax(0,1fr)]">
                <aside className="thin-scrollbar hidden min-h-0 overflow-y-auto rounded-lg border border-stone-200 bg-card p-4 shadow-sm dark:border-stone-800 lg:block">
                    <LogPanel
                        logs={logs}
                        selectedLogIds={selectedLogIds}
                        activeLogId={previewLog?.id}
                        onSelectedLogIdsChange={setSelectedLogIds}
                        onCreateSession={createSession}
                        onDeleteSelected={() => setDeleteConfirmOpen(true)}
                        onPreviewLog={(log) => void previewGenerationLog(log)}
                    />
                </aside>

                <section className="grid gap-3 lg:min-h-0 lg:overflow-hidden xl:grid-cols-[420px_minmax(0,1fr)]">
                    <div className="thin-scrollbar flex flex-col rounded-lg border border-stone-200 bg-card p-4 shadow-sm dark:border-stone-800 lg:min-h-0 lg:overflow-y-auto">
                        <div>
                            <div className="flex items-start justify-between gap-3">
                                <div className="min-w-0">
                                    <h1 className="text-2xl font-semibold text-stone-950 dark:text-stone-100">生图工作台</h1>
                                </div>
                                <div className="flex shrink-0 gap-2 lg:hidden">
                                    <Button icon={<History className="size-4" />} onClick={() => setLogsOpen(true)}>
                                        记录
                                    </Button>
                                    <Button icon={<SlidersHorizontal className="size-4" />} onClick={() => setSettingsOpen(true)}>
                                        参数
                                    </Button>
                                </div>
                            </div>
                            <Segmented
                                className="mt-3 w-full [&_.ant-segmented-group]:!flex [&_.ant-segmented-item]:!flex-1"
                                value={activeTab}
                                options={WORKBENCH_TABS.map((item) => ({ value: item.value, label: item.label }))}
                                onChange={(value) => setActiveTab(value as GenerationMode)}
                            />
                        </div>

                        {activeTab === "general" ? (
                            <GeneralGenerationPanel
                                prompt={prompt}
                                onPromptChange={setPrompt}
                                promptTranslation={promptTranslation}
                                onPromptTranslationChange={setPromptTranslation}
                                onSwapTranslation={swapPromptTranslation}
                                references={references}
                                onReferencesChange={(updater) => setReferences(updater)}
                                onPasteReferences={() => void addReferencesFromClipboard()}
                                onUploadReferences={() => fileInputRef.current?.click()}
                                config={effectiveConfig}
                                model={model}
                                updateConfig={updateConfig}
                                openConfigDialog={openConfigDialog}
                                onOpenPromptLibrary={() => setPromptDialogOpen(true)}
                                onOpenAssetPicker={() => setAssetPickerOpen(true)}
                                onOpenSettingsDrawer={() => setSettingsOpen(true)}
                                running={running}
                                canGenerate={canGenerate}
                                onGenerate={() => void generate()}
                            />
                        ) : (
                            <NovelAIGenerationPanel
                                activeField={novelAIField}
                                onActiveFieldChange={setNovelAIField}
                                references={references}
                                onReferencesChange={(updater) => setReferences(updater)}
                                onPasteReferences={() => void addReferencesFromClipboard()}
                                onUploadReferences={() => fileInputRef.current?.click()}
                                config={effectiveConfig}
                                openConfigDialog={openConfigDialog}
                                running={running}
                                canGenerate={canGenerate}
                                onGenerate={() => void generate()}
                            />
                        )}
                    </div>

                    <div className="thin-scrollbar rounded-lg border border-stone-200 bg-card p-4 shadow-sm dark:border-stone-800 lg:min-h-0 lg:overflow-y-auto lg:p-5">
                        <div className="mb-4 flex items-center justify-between gap-3">
                            <div>
                                <h2 className="text-xl font-semibold">生成结果</h2>
                            </div>
                            {running ? <Tag className="m-0 px-2 py-1">等待 {formatDuration(elapsedMs)}</Tag> : null}
                        </div>
                        {results.length ? (
                            <div className="grid gap-4 sm:grid-cols-2 2xl:grid-cols-3">
                                {results.map((result, index) =>
                                    result.status === "success" && result.image ? (
                                        <ResultImageCard key={result.id} image={result.image} index={index} onEdit={addResultToReferences} onDownload={downloadImage} onSaveAsset={saveResultToAssets} />
                                    ) : result.status === "failed" ? (
                                        <FailedImageCard key={result.id} error={result.error || "生成失败"} onRetry={() => retryResult(index)} />
                                    ) : (
                                        <PendingImageCard key={result.id} progress={queueProgress[index]} />
                                    ),
                                )}
                            </div>
                        ) : (
                            <div className="flex min-h-[320px] flex-col items-center justify-center rounded-lg border border-dashed border-stone-300 text-center dark:border-stone-700 lg:min-h-[560px]">
                                <ImagePlus className="mb-4 size-11 text-stone-400" />
                                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没有生成图片" />
                            </div>
                        )}
                    </div>
                </section>
            </main>
            <input
                ref={fileInputRef}
                type="file"
                accept="image/*"
                multiple
                className="hidden"
                onChange={(event) => {
                    void addReferences(event.target.files);
                    event.target.value = "";
                }}
            />
            <Drawer title="生成记录" placement="bottom" size="large" open={logsOpen} onClose={() => setLogsOpen(false)}>
                <LogPanel
                    logs={logs}
                    selectedLogIds={selectedLogIds}
                    activeLogId={previewLog?.id}
                    onSelectedLogIdsChange={setSelectedLogIds}
                    onCreateSession={createSession}
                    onDeleteSelected={() => setDeleteConfirmOpen(true)}
                    onPreviewLog={(log) => void previewGenerationLog(log)}
                />
            </Drawer>
            <Drawer title="参数" placement="bottom" size="82vh" open={settingsOpen} onClose={() => setSettingsOpen(false)}>
                <div className="grid grid-cols-2 gap-3 pb-4">
                    <GenerationSettings config={effectiveConfig} model={model} updateConfig={updateConfig} openConfigDialog={openConfigDialog} showNovelAI={false} />
                </div>
            </Drawer>
            <PromptSelectDialog open={promptDialogOpen} onOpenChange={setPromptDialogOpen} onSelect={(value) => setPrompt(value)} />
            <AssetPickerModal open={assetPickerOpen} defaultTab="my-assets" onInsert={(payload) => void insertPickedAsset(payload)} onClose={() => setAssetPickerOpen(false)} />
            <Modal title="删除生成记录" open={deleteConfirmOpen} onCancel={() => setDeleteConfirmOpen(false)} onOk={deleteSelectedLogs} okText="删除" okButtonProps={{ danger: true }} cancelText="取消">
                确定删除选中的 {selectedLogIds.length} 条生成记录吗？
            </Modal>
        </div>
    );
}

function ResultImageCard({
    image,
    index,
    onEdit,
    onDownload,
    onSaveAsset,
}: {
    image: GeneratedImage;
    index: number;
    onEdit: (image: GeneratedImage, index: number) => void;
    onDownload: (image: GeneratedImage, index: number) => void;
    onSaveAsset: (image: GeneratedImage, index: number) => void;
}) {
    return (
        <div className="overflow-hidden rounded-lg border border-stone-200 bg-background dark:border-stone-800">
            <Image src={image.dataUrl} alt={`生成结果 ${index + 1}`} className="aspect-square object-cover" />
            <div className="space-y-2 border-t border-stone-200 px-3 py-2.5 dark:border-stone-800">
                <div className="flex min-w-0 gap-x-2 gap-y-1 text-xs text-stone-500 dark:text-stone-400">
                    <span>
                        {image.width}x{image.height}
                    </span>
                    <span>{formatBytes(image.bytes)}</span>
                    <span>{formatDuration(image.durationMs)}</span>
                </div>
                <div className="grid min-w-0 grid-cols-3 gap-2">
                    <Tooltip title="添加到素材">
                        <Button className={RESULT_ACTION_BUTTON_CLASS} size="small" icon={<FolderPlus className="size-3.5" />} onClick={() => void onSaveAsset(image, index)}>
                            添加到素材
                        </Button>
                    </Tooltip>
                    <Tooltip title="加入参考图">
                        <Button className={RESULT_ACTION_BUTTON_CLASS} size="small" icon={<PenLine className="size-3.5" />} onClick={() => void onEdit(image, index)}>
                            加入参考图
                        </Button>
                    </Tooltip>
                    <Tooltip title="下载">
                        <Button className={RESULT_ACTION_BUTTON_CLASS} size="small" icon={<Download className="size-3.5" />} onClick={() => onDownload(image, index)}>
                            下载
                        </Button>
                    </Tooltip>
                </div>
            </div>
        </div>
    );
}

function PendingImageCard({ progress }: { progress?: ImageQueueProgress }) {
    return (
        <div className="relative aspect-square overflow-hidden rounded-lg border border-dashed border-stone-300 bg-stone-50 dark:border-stone-700 dark:bg-stone-900">
            <div
                className="absolute inset-0 opacity-60"
                style={{
                    backgroundImage: "radial-gradient(circle, rgba(120,113,108,0.35) 1.4px, transparent 1.6px)",
                    backgroundSize: "16px 16px",
                }}
            />
            <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 px-4 text-center text-sm text-stone-500 dark:text-stone-400">
                <LoaderCircle className="size-6 animate-spin" />
                <span>{formatQueueProgress(progress)}</span>
                {progress?.status === "queued" ? <span className="text-xs text-stone-400 dark:text-stone-500">NovelAI 免费生图需排队，请勿关闭页面</span> : null}
            </div>
        </div>
    );
}

/**
 * 把排队进度转成人话。
 *
 * NovelAI 免费生图不支持并发，全站串行，所以「前方还有几张」是用户最关心的信息 ——
 * 有了它，长时间等待就从「卡住了？」变成「还有 3 张，约 36 秒」。
 */
function formatQueueProgress(progress?: ImageQueueProgress) {
    if (!progress) return "生成中";
    if (progress.status === "queued") {
        const ahead = progress.imagesAhead ?? 0;
        if (ahead <= 0) return "排队中，即将开始";
        const eta = formatQueueEta(progress.estimatedSeconds);
        return `排队中，前方还有 ${ahead} 张${eta ? `，${eta}` : ""}`;
    }
    if (progress.total && progress.total > 1 && progress.current) {
        return `生成中 ${progress.current}/${progress.total}`;
    }
    return "生成中";
}

function formatQueueEta(seconds?: number) {
    if (!seconds || seconds <= 0) return "";
    if (seconds < 60) return `约 ${seconds} 秒`;
    return `约 ${Math.ceil(seconds / 60)} 分钟`;
}

function FailedImageCard({ error, onRetry }: { error: string; onRetry: () => void }) {
    return (
        <div className="overflow-hidden rounded-lg border border-red-200 bg-red-50 dark:border-red-950 dark:bg-red-950/20">
            <div className="flex aspect-square flex-col items-center justify-center gap-3 p-5 text-center">
                <div className="text-sm font-medium text-red-600 dark:text-red-300">生成失败</div>
                <Typography.Paragraph ellipsis={{ rows: 4 }} className="!mb-0 !text-xs !text-red-500 dark:!text-red-300">
                    {error}
                </Typography.Paragraph>
            </div>
            <div className="flex justify-end border-t border-red-200 p-3 dark:border-red-950">
                <Button size="small" danger onClick={onRetry}>
                    重试
                </Button>
            </div>
        </div>
    );
}

function updateResultAt(results: GenerationResult[], index: number, next: Partial<GenerationResult>) {
    return results.map((item, itemIndex) => (itemIndex === index ? { ...item, ...next } : item));
}

function LogPanel({
    logs,
    selectedLogIds,
    activeLogId,
    onSelectedLogIdsChange,
    onCreateSession,
    onDeleteSelected,
    onPreviewLog,
}: {
    logs: GenerationLog[];
    selectedLogIds: string[];
    activeLogId?: string;
    onSelectedLogIdsChange: (ids: string[]) => void;
    onCreateSession: () => void;
    onDeleteSelected: () => void;
    onPreviewLog: (log: GenerationLog) => void;
}) {
    const allSelected = Boolean(logs.length) && selectedLogIds.length === logs.length;
    const toggleAll = () => onSelectedLogIdsChange(allSelected ? [] : logs.map((log) => log.id));

    return (
        <>
            <div className="mb-3 flex items-center justify-between gap-3">
                <div>
                    <h2 className="text-base font-semibold">生成记录</h2>
                </div>
                <Tag className="m-0">{logs.length}</Tag>
            </div>
            <div className="mb-4 flex flex-wrap gap-2">
                <Button size="small" icon={<Plus className="size-3.5" />} onClick={onCreateSession}>
                    新建
                </Button>
                <Button size="small" icon={<CheckSquare className="size-3.5" />} disabled={!logs.length} onClick={toggleAll}>
                    {allSelected ? "取消" : "全选"}
                </Button>
                <Button size="small" danger icon={<Trash2 className="size-3.5" />} disabled={!selectedLogIds.length} onClick={onDeleteSelected}>
                    删除
                </Button>
            </div>
            <div className="space-y-3">
                {logs.map((log) => (
                    <LogCard
                        key={log.id}
                        log={log}
                        selected={selectedLogIds.includes(log.id)}
                        active={activeLogId === log.id}
                        onSelectedChange={(checked) => onSelectedLogIdsChange(checked ? [...selectedLogIds, log.id] : selectedLogIds.filter((id) => id !== log.id))}
                        onClick={() => onPreviewLog(log)}
                    />
                ))}
                {!logs.length ? <div className="flex min-h-48 items-center justify-center rounded-lg border border-dashed border-stone-300 text-center text-sm text-stone-500 dark:border-stone-700">暂无生成记录</div> : null}
            </div>
        </>
    );
}

function LogCard({ log, selected, active, onSelectedChange, onClick }: { log: GenerationLog; selected: boolean; active: boolean; onSelectedChange: (checked: boolean) => void; onClick: () => void }) {
    const thumbnails = (log.thumbnails || []).filter(Boolean).slice(0, 4);

    return (
        <button
            type="button"
            className={`block w-full rounded-lg border p-2 text-left transition ${
                active ? "border-stone-900 bg-blue-50 dark:border-stone-100 dark:bg-blue-950/20" : "border-stone-200 bg-background hover:bg-stone-50 dark:border-stone-800 dark:hover:bg-stone-900"
            }`}
            onClick={onClick}
        >
            <div className="grid grid-cols-[minmax(128px,1fr)_auto] gap-2">
                <div className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)] items-start gap-2">
                    <Checkbox className="mt-0.5" checked={selected} onClick={(event) => event.stopPropagation()} onChange={(event) => onSelectedChange(event.target.checked)} />
                    <div className="min-w-0">
                        <div className="truncate text-sm font-semibold leading-5">{log.title}</div>
                        {thumbnails.length ? (
                            <div className="mt-2 flex gap-1 overflow-hidden">
                                {thumbnails.map((image, index) => (
                                    <img key={`${log.id}-${index}`} src={image} alt="" className="size-8 shrink-0 rounded-md object-cover" />
                                ))}
                            </div>
                        ) : null}
                    </div>
                </div>
                <div className="grid justify-items-end gap-2">
                    <div className="flex gap-1">
                        <Tag className="m-0 flex h-6 items-center rounded-md px-1.5 text-xs leading-none" color="blue">
                            成功 {log.successCount ?? log.imageCount}
                        </Tag>
                        {log.failCount ? (
                            <Tag className="m-0 flex h-6 items-center rounded-md px-1.5 text-xs leading-none" color="red">
                                失败 {log.failCount}
                            </Tag>
                        ) : null}
                    </div>
                    <div className="flex flex-wrap justify-end gap-1">
                        <Tag className="m-0 flex h-6 items-center rounded-md px-1.5 text-xs leading-none">{log.imageCount} 张</Tag>
                        <Tag className="m-0 flex h-6 items-center rounded-md px-1.5 text-xs leading-none" color="green">
                            {formatDuration(log.durationMs)}
                        </Tag>
                    </div>
                    <div className="flex justify-end">
                        <Tag className="m-0 flex h-6 items-center rounded-md px-1.5 text-xs leading-none">{log.time}</Tag>
                    </div>
                </div>
            </div>
        </button>
    );
}

async function readStoredLogs() {
    if (typeof window === "undefined") return [];
    try {
        const values: GenerationLog[] = [];
        await logStore.iterate<GenerationLog, void>((value) => {
            values.push(value);
        });
        const logs = await Promise.all(values.map(normalizeLog));
        return logs.sort((a, b) => (b.createdAt || 0) - (a.createdAt || 0));
    } catch {
        return [];
    }
}

async function normalizeLog(log: Partial<GenerationLog>): Promise<GenerationLog> {
    const references = await Promise.all(
        (log.references || []).map(async (item) => ({
            ...item,
            dataUrl: await resolveImageUrl(item.storageKey, item.dataUrl),
        })),
    );
    const images = await Promise.all(
        (log.images || []).map(async (item) => ({
            ...item,
            dataUrl: await resolveImageUrl(item.storageKey, item.dataUrl),
        })),
    );
    const config = normalizeLogConfig(log);
    return {
        id: log.id || nanoid(),
        createdAt: log.createdAt || Date.now(),
        // 老记录没有 mode。它们都产生于「通用生图 + 可选 NAI 参数」的旧工作台，
        // 统一按 general 处理，避免恢复时把 NovelAI 参数刷回全局配置。
        mode: log.mode === "novelai" ? "novelai" : "general",
        title: log.title || log.model || "未命名",
        prompt: log.prompt || log.title || "",
        promptTranslation: log.promptTranslation || "",
        promptTokens: log.promptTokens,
        negativePrompt: log.negativePrompt || "",
        negativePromptTokens: log.negativePromptTokens,
        time: log.time || new Date().toLocaleString("zh-CN", { hour12: false }),
        model: log.model || config.imageModel || "",
        config,
        references,
        durationMs: log.durationMs || 0,
        successCount: log.successCount ?? log.imageCount ?? 0,
        failCount: log.failCount || 0,
        imageCount: log.imageCount || log.successCount || 0,
        size: log.size || config.size || "",
        quality: log.quality || config.quality || "",
        status: log.status || "成功",
        images,
        thumbnails: images.map((image) => image.dataUrl).filter(Boolean),
    };
}

function serializeLog(log: GenerationLog): GenerationLog {
    return {
        ...log,
        references: log.references.map((item) => ({ ...item, dataUrl: item.storageKey ? "" : item.dataUrl })),
        images: log.images.map((image) => ({ ...image, dataUrl: image.storageKey ? "" : image.dataUrl })),
        thumbnails: [],
    };
}

function normalizeLogConfig(log: Partial<GenerationLog>): GenerationLogConfig {
    return {
        model: log.config?.model || log.model || "",
        imageModel: log.config?.imageModel || log.model || "",
        quality: log.config?.quality || log.quality || "",
        size: log.config?.size || log.size || "",
        count: log.config?.count || String(log.imageCount || log.successCount || 1),
        ...normalizeNovelAISettings(log.config || {}),
    };
}

function buildLog({
    mode,
    prompt,
    promptTranslation,
    negativePrompt,
    model,
    config,
    references,
    durationMs,
    successCount,
    failCount,
    status,
    images,
}: {
    mode: GenerationMode;
    prompt: string;
    promptTranslation?: string;
    negativePrompt?: string;
    model: string;
    config: GenerationLogConfig;
    references: ReferenceImage[];
    durationMs: number;
    successCount: number;
    failCount: number;
    status: GenerationLog["status"];
    images: GeneratedImage[];
}): GenerationLog {
    const logConfig: GenerationLogConfig = {
        model: config.model,
        imageModel: config.imageModel,
        quality: config.quality,
        size: config.size,
        count: config.count,
        ...normalizeNovelAISettings(config),
    };
    return {
        id: nanoid(),
        createdAt: Date.now(),
        mode,
        title: prompt.slice(0, 12) || "未命名",
        prompt,
        promptTranslation: promptTranslation?.trim() || "",
        negativePrompt: negativePrompt?.trim() || "",
        time: new Date().toLocaleString("zh-CN", { hour12: false }),
        model,
        config: logConfig,
        references,
        durationMs,
        successCount,
        failCount,
        imageCount: Number(logConfig.count) || successCount,
        size: logConfig.size,
        quality: logConfig.quality,
        status,
        images,
        thumbnails: images.map((image) => image.dataUrl).filter(Boolean),
    };
}
