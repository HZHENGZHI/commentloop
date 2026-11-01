import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type PointerEvent as ReactPointerEvent,
} from "react";
import {
  addRemoteRoot,
  addRoot,
  createComment,
  deleteComment,
  deleteDoc,
  createAgentSession,
  setActiveAgentSession,
  fetchDoc,
  removeRoot,
  renderPreview,
  replyComment,
  groupComments,
  rollbackComment,
  resolveComment,
  triggerCommentAgent,
  reactComment,
  requestDocReview,
  resolveSuggestion,
  saveDoc,
  setDocReview,
  ApiError,
} from "./api";
import { Editor, type EditorDocument, type EditorHandle } from "./components/Editor";
import { Preview } from "./components/Preview";
import { CommentsPanel } from "./components/CommentsPanel";
import { CommentModal } from "./components/CommentModal";
import { CodeRepoModal } from "./components/CodeRepoModal";
import { CodeBrowserModal } from "./components/CodeBrowserModal";
import { SettingsModal } from "./components/SettingsModal";
import { DiscussionModal } from "./components/DiscussionModal";
import { InboxModal } from "./components/InboxModal";
import { HistoryModal } from "./components/HistoryModal";
import { SearchModal } from "./components/SearchModal";
import { ReviewStatusSelect } from "./components/ReviewStatus";
import { AgentSessionPicker } from "./components/AgentSessionPicker";
import { Sidebar } from "./components/Sidebar";
import { NewDocModal } from "./components/NewDocModal";
import { AddRootModal } from "./components/AddRootModal";
import { Toasts } from "./components/Toasts";
import {
  IconAlert,
  IconCheck,
  IconColumns,
  IconEye,
  IconFileText,
  IconFolder,
  IconFolderPlus,
  IconGitBranch,
  IconHistory,
  IconInbox,
  IconMessage,
  IconMoon,
  IconPanelLeft,
  IconPen,
  IconPlus,
  IconSearch,
  IconSettings,
  IconSun,
} from "./components/Icons";
import { Badge, Button, ConfigProvider, Dropdown, Segmented, Tag, Tooltip, theme as antdTheme } from "antd";
import enUS from "antd/locale/en_US";
import zhCN from "antd/locale/zh_CN";
import { useTranslation } from "react-i18next";
import i18n from "./i18n";
import { localizeServerMessage } from "./i18n/serverMessages";
import { applyTheme, currentTheme } from "./theme";
import { useWebSocket } from "./useWebSocket";
import { useQuery } from "@tanstack/react-query";
import {
  agentSessionsQuery,
  commentsQuery,
  configQuery,
  discussionsQuery,
  docCodeReposQuery,
  docReviewQuery,
  inboxQuery,
  invalidate,
  promptTemplatesQuery,
  queryClient,
  treeQuery,
} from "./queries";
import type {
  AddRemoteRootRequest,
  AgentSession,
  Comment,
  CommentMessage,
  CodeRepo,
  ConfigResponse,
  ConsensusActivity,
  Discussion,
  DocReview,
  DocStats,
  InboxCounts,
  InboxDoc,
  PromptTemplate,
  ReviewStatus,
  RootInfo,
  SaveStatus,
  Theme,
  ToastItem,
  TreeResponse,
  ViewMode,
  WsServerMessage,
} from "./types";
import {
  baseName,
  clamp,
  computeStats,
  dirName,
  formatBytes,
  isMarkdownPath,
  parentDir,
  expandQuoteToBlock,
  parseMarkdownHeadings,
  scrollToQuote,
} from "./utils";

const PREVIEW_DELAY = 80;
const SAVE_DELAY = 850;
const LAST_FILE_KEY = "commentloop:last";
const SIDEBAR_KEY = "commentloop:sidebar";
const VIEW_KEY = "commentloop:view";
const SPLIT_KEY = "commentloop:split";
const SEEN_KEY = "commentloop:seen";

function loadSeen(): Record<string, number> {
  try {
    const raw = localStorage.getItem(SEEN_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

const EMPTY_STATS: DocStats = { words: 0, chars: 0, lines: 1 };
const NO_COMMENTS: Comment[] = [];
const NO_CODE_REPOS: CodeRepo[] = [];
const NO_DISCUSSIONS: Discussion[] = [];
const NO_PROMPT_TEMPLATES: PromptTemplate[] = [];
const NO_AGENT_SESSIONS: AgentSession[] = [];
const NO_INBOX_DOCS: InboxDoc[] = [];
const EMPTY_INBOX_COUNTS: InboxCounts = {
  awaiting: 0,
  pendingAgent: 0,
  agentWorking: 0,
  draft: 0,
  inReview: 0,
  changesRequested: 0,
  approved: 0,
};

const VIEW_OPTIONS = [
  { value: "split" as const, label: "分栏", icon: <IconColumns size={15} /> },
  { value: "editor" as const, label: "编辑", icon: <IconPen size={15} /> },
  { value: "preview" as const, label: "预览", icon: <IconEye size={15} /> },
];

function readLocal(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : i18n.t("未知错误");
}

function loadViewMode(): ViewMode {
  const stored = readLocal(VIEW_KEY);
  return stored === "editor" || stored === "preview" || stored === "split"
    ? stored
    : "split";
}

function loadSplit(): number {
  const raw = readLocal(SPLIT_KEY);
  const stored = raw === null ? Number.NaN : Number(raw);
  return Number.isFinite(stored) ? clamp(stored, 20, 80) : 50;
}

function loadSidebar(): boolean {
  const stored = readLocal(SIDEBAR_KEY);
  if (stored === "0") return false;
  if (stored === "1") return true;
  return window.innerWidth >= 900;
}

function suffixedName(name: string, n: number): string {
  const dot = name.lastIndexOf(".");
  if (dot <= 0) return `${name}-${n}`;
  return `${name.slice(0, dot)}-${n}${name.slice(dot)}`;
}

export default function App() {
  const { t, i18n: i18nHook } = useTranslation();
  const configResult = useQuery(configQuery);
  const config = configResult.data ?? null;
  const configLoaded = !configResult.isPending;
  const tree = useQuery(treeQuery).data ?? null;
  const [currentPath, setCurrentPath] = useState<string | null>(null);
  const [doc, setDoc] = useState<EditorDocument>({ content: "", version: 0 });
  const [html, setHtml] = useState("");
  const [meta, setMeta] = useState<{ size: number; modTime: string } | null>(
    null,
  );
  const [status, setStatus] = useState<SaveStatus>("saved");
  const [stats, setStats] = useState<DocStats>(EMPTY_STATS);
  const [loading, setLoading] = useState(false);
  const [newModal, setNewModal] = useState(false);
  const [addRootOpen, setAddRootOpen] = useState(false);
  const [removingRoot, setRemovingRoot] = useState<{ id: string; name: string } | null>(null);
  const [commentsOpen, setCommentsOpen] = useState(false);
  const [inboxOpen, setInboxOpen] = useState(false);
  const [reviewRequestPath, setReviewRequestPath] = useState<string | null>(null);
  const [seenPaths, setSeenPaths] = useState<Record<string, number>>(loadSeen);
  const [notificationPermission, setNotificationPermission] = useState<string>(() =>
    typeof Notification === "undefined" ? "unsupported" : Notification.permission,
  );
  const [commentQuote, setCommentQuote] = useState<string | null>(null);
  const [codeRepoModalOpen, setCodeRepoModalOpen] = useState(false);
  const [codeBrowserOpen, setCodeBrowserOpen] = useState(false);
  const [discussionModalOpen, setDiscussionModalOpen] = useState(false);
  const [consensusActivity, setConsensusActivity] =
    useState<ConsensusActivity | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [documentCommentOpen, setDocumentCommentOpen] = useState(false);
  const [searchOpen, setSearchOpen] = useState(false);
  const [agentActivity, setAgentActivity] = useState<{
    status: "queued" | "working" | "done" | "error";
    message: string;
  } | null>(null);
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const docKey = currentPath ?? "";
  const inboxResult = useQuery(inboxQuery);
  const inboxDocs = inboxResult.data?.docs ?? NO_INBOX_DOCS;
  const inboxCounts = inboxResult.data?.counts ?? EMPTY_INBOX_COUNTS;
  const inboxError = inboxResult.isError;
  const docReview = useQuery(docReviewQuery(docKey)).data;
  const reviewStatus: ReviewStatus = docReview?.status ?? "draft";
  const inboxByPath = useMemo(
    () => new Map(inboxDocs.map((doc) => [doc.path, doc])),
    [inboxDocs],
  );
  const inboxAttention = inboxDocs.filter((doc) => doc.awaiting > 0).length;
  const headings = useMemo(
    () => parseMarkdownHeadings(doc.content),
    [doc.content],
  );
  const unreadPaths = useMemo(() => {
    const out = new Set<string>();
    for (const doc of inboxDocs) {
      if (doc.path === currentPath) continue;
      const seen = seenPaths[doc.path] ?? 0;
      if (doc.comments > 0 && Date.parse(doc.lastCommentAt) > seen) {
        out.add(doc.path);
      }
    }
    return out;
  }, [inboxDocs, seenPaths, currentPath]);
  const commentsResult = useQuery(commentsQuery(docKey));
  const comments = commentsResult.data?.comments ?? NO_COMMENTS;
  const commentsLoaded = commentsResult.isSuccess;
  const codeRepos = useQuery(docCodeReposQuery(docKey)).data?.repos ?? NO_CODE_REPOS;
  const discussionsResult = useQuery(discussionsQuery(docKey));
  const discussions = discussionsResult.data?.discussions ?? NO_DISCUSSIONS;
  const promptTemplates =
    useQuery(promptTemplatesQuery(i18nHook.language)).data?.templates ?? NO_PROMPT_TEMPLATES;
  const agentSessionsData = useQuery(agentSessionsQuery(docKey)).data;
  const agentSessions = agentSessionsData?.sessions ?? NO_AGENT_SESSIONS;
  const activeAgentSessionId = agentSessionsData?.activeId ?? "";
  const [theme, setTheme] = useState<Theme>(() => currentTheme());
  const [viewMode, setViewMode] = useState<ViewMode>(loadViewMode);
  const [sidebarOpen, setSidebarOpen] = useState(loadSidebar);
  const [split, setSplit] = useState(loadSplit);

  const previewOnly = config?.previewOnly ?? false;
  const debugMode = config?.debug ?? false;
  const effectiveViewMode: ViewMode = previewOnly ? "preview" : viewMode;
  const roots = tree?.roots ?? [];
  const currentFile = tree?.files.find((file) => file.path === currentPath);
  const primaryRoot = roots.find((root) => root.primary) ?? roots[0];
  const currentRoot = currentFile
    ? roots.find((root) => root.id === currentFile.rootId)
    : undefined;
  const defaultRoot = currentRoot ?? primaryRoot;
  const defaultDir = currentFile
    ? dirName(currentPath ?? "")
    : (defaultRoot?.id ?? "");
  const displayDir = currentFile
    ? [currentFile.rootName, currentFile.dir].filter(Boolean).join("/")
    : (defaultRoot?.name ?? "");
  const currentLocation = currentFile
    ? [currentFile.rootName, currentFile.dir].filter(Boolean).join(" / ")
    : t("根目录");
  const currentRootLocation = currentRoot
    ? currentRoot.kind === "remote"
      ? `${currentRoot.remote}:${currentRoot.path}`
      : currentRoot.path
    : "";

  const contentRef = useRef("");
  const savedContentRef = useRef("");
  const currentPathRef = useRef<string | null>(null);
  const statusRef = useRef<SaveStatus>("saved");
  const docVersionRef = useRef(0);
  const saveSeqRef = useRef(0);
  const openSeqRef = useRef(0);
  const initialPickRef = useRef(false);

  const previewTimer = useRef<number | undefined>(undefined);
  const saveTimer = useRef<number | undefined>(undefined);
  const statsTimer = useRef<number | undefined>(undefined);
  const treeTimer = useRef<number | undefined>(undefined);
  const scrollReleaseTimer = useRef<number | undefined>(undefined);
  const scrollOwner = useRef<"editor" | "preview" | null>(null);

  const previewRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<EditorHandle>(null);
  const splitRef = useRef<HTMLDivElement>(null);
  const toastIdRef = useRef(0);
  const messageHandlerRef = useRef<(message: WsServerMessage) => void>(
    () => {},
  );

  const { state: connection, send } = useWebSocket((message) =>
    messageHandlerRef.current(message),
  );

  const toast = useCallback(
    (message: string, kind: ToastItem["kind"] = "info") => {
      const id = ++toastIdRef.current;
      setToasts((list) => [...list.slice(-3), { id, message, kind }]);
      window.setTimeout(() => {
        setToasts((list) => list.filter((item) => item.id !== id));
      }, 4600);
    },
    [],
  );

  const dismissToast = useCallback((id: number) => {
    setToasts((list) => list.filter((item) => item.id !== id));
  }, []);

  const cancelTimers = useCallback(() => {
    if (previewTimer.current !== undefined) {
      window.clearTimeout(previewTimer.current);
      previewTimer.current = undefined;
    }
    if (saveTimer.current !== undefined) {
      window.clearTimeout(saveTimer.current);
      saveTimer.current = undefined;
    }
    if (statsTimer.current !== undefined) {
      window.clearTimeout(statsTimer.current);
      statsTimer.current = undefined;
    }
  }, []);

  const refreshTree = useCallback(async (): Promise<TreeResponse | null> => {
    try {
      return await queryClient.fetchQuery({ ...treeQuery, staleTime: 0 });
    } catch (error) {
      toast(i18n.t("读取文件列表失败：{{error}}", { error: errorMessage(error) }), "error");
      return null;
    }
  }, [toast]);

  const scheduleTreeRefresh = useCallback(() => {
    if (treeTimer.current !== undefined) window.clearTimeout(treeTimer.current);
    treeTimer.current = window.setTimeout(() => {
      treeTimer.current = undefined;
      void refreshTree();
    }, 250);
  }, [refreshTree]);

  const applySavedDoc = useCallback(
    (content: string, nextHtml: string) => {
      // Any save started before this authoritative content must not be able to
      // land afterwards and overwrite it (for example during a version restore).
      saveSeqRef.current += 1;
      cancelTimers();
      contentRef.current = content;
      savedContentRef.current = content;
      docVersionRef.current += 1;
      setDoc({ content, version: docVersionRef.current });
      setHtml(nextHtml);
      setStats(computeStats(content));
      setStatus("saved");
      statusRef.current = "saved";
    },
    [cancelTimers],
  );

  const clearCurrent = useCallback(() => {
    cancelTimers();
    saveSeqRef.current += 1;
    currentPathRef.current = null;
    contentRef.current = "";
    savedContentRef.current = "";
    docVersionRef.current += 1;
    setCurrentPath(null);
    setDoc({ content: "", version: docVersionRef.current });
    setHtml("");
    setMeta(null);
    setStats(EMPTY_STATS);
    setStatus("saved");
    statusRef.current = "saved";
    const url = new URL(window.location.href);
    url.searchParams.delete("file");
    window.history.replaceState(null, "", url);
  }, [cancelTimers]);

  const persist = useCallback(
    async (options?: { manual?: boolean }) => {
      if (previewOnly) return;
      const path = currentPathRef.current;
      if (!path) return;
      const content = contentRef.current;
      if (!options?.manual && content === savedContentRef.current) {
        setStatus("saved");
        statusRef.current = "saved";
        return;
      }
      const seq = ++saveSeqRef.current;
      setStatus("saving");
      statusRef.current = "saving";
      try {
        const result = await saveDoc(path, content);
        // A slow save may finish after the user switched documents; applying
        // its stats/content baseline to the new document would show a wrong
        // dirty state.
        if (seq !== saveSeqRef.current || currentPathRef.current !== path) return;
        savedContentRef.current = content;
        void invalidate("comments");
        setMeta({ size: result.size, modTime: result.modTime });
        if (contentRef.current === content) {
          setHtml(result.html);
          setStatus("saved");
          statusRef.current = "saved";
          if (options?.manual) toast(i18n.t("已保存"), "success");
        } else {
          setStatus("dirty");
          statusRef.current = "dirty";
        }
      } catch (error) {
        if (seq !== saveSeqRef.current || currentPathRef.current !== path) return;
        setStatus("dirty");
        statusRef.current = "dirty";
        toast(i18n.t("保存失败：{{error}}", { error: errorMessage(error) }), "error");
      }
    },
    [previewOnly, toast],
  );

  const scheduleSave = useCallback(() => {
    if (saveTimer.current !== undefined) window.clearTimeout(saveTimer.current);
    saveTimer.current = window.setTimeout(() => {
      saveTimer.current = undefined;
      void persist();
    }, SAVE_DELAY);
  }, [persist]);

  const schedulePreview = useCallback(
    (content: string) => {
      const path = currentPathRef.current;
      if (!path) return;
      if (previewTimer.current !== undefined) {
        window.clearTimeout(previewTimer.current);
      }
      previewTimer.current = window.setTimeout(() => {
        previewTimer.current = undefined;
        if (currentPathRef.current !== path) return;
        if (send({ type: "edit", path, content })) return;
        // WebSocket is down: keep the preview alive through REST.
        void renderPreview(path, content)
          .then((result) => {
            if (
              currentPathRef.current === path &&
              contentRef.current === content
            ) {
              setHtml(result.html);
            }
          })
          .catch(() => {});
      }, PREVIEW_DELAY);
    },
    [send],
  );

  const handleChange = useCallback(
    (value: string) => {
      if (previewOnly || !currentPathRef.current) return;
      contentRef.current = value;
      if (statusRef.current !== "dirty") {
        statusRef.current = "dirty";
        setStatus("dirty");
      }
      schedulePreview(value);
      scheduleSave();
      if (statsTimer.current !== undefined) {
        window.clearTimeout(statsTimer.current);
      }
      statsTimer.current = window.setTimeout(() => {
        statsTimer.current = undefined;
        setStats(computeStats(contentRef.current));
      }, 220);
    },
    [previewOnly, schedulePreview, scheduleSave],
  );

  const openDoc = useCallback(
    async (path: string) => {
      const cleanPath = path.replace(/^\/+/, "");
      if (!isMarkdownPath(cleanPath)) {
        toast(i18n.t("只能打开 Markdown 文件"), "warn");
        return;
      }
      if (currentPathRef.current === cleanPath) return;
      const seq = ++openSeqRef.current;

      const previousPath = currentPathRef.current;
      if (previousPath && !previewOnly) {
        if (saveTimer.current !== undefined) {
          window.clearTimeout(saveTimer.current);
          saveTimer.current = undefined;
        }
        if (contentRef.current !== savedContentRef.current) {
          try {
            const result = await saveDoc(previousPath, contentRef.current);
            savedContentRef.current = result.content;
          } catch (error) {
            toast(
              i18n.t("当前文档尚未保存，已取消切换：{{error}}", { error: errorMessage(error) }),
              "error",
            );
            return;
          }
        }
      }

      cancelTimers();
      setLoading(true);
      try {
        const result = await fetchDoc(cleanPath);
        if (seq !== openSeqRef.current) return;
        currentPathRef.current = result.path;
        contentRef.current = result.content;
        savedContentRef.current = result.content;
        docVersionRef.current += 1;
        scrollOwner.current = null;
        setCurrentPath(result.path);
        setDoc({ content: result.content, version: docVersionRef.current });
        setHtml(result.html);
        setMeta({ size: result.size, modTime: result.modTime });
        setStats(computeStats(result.content));
        setStatus("saved");
        statusRef.current = "saved";
        try {
          localStorage.setItem(LAST_FILE_KEY, result.path);
        } catch {
          // Ignore storage restrictions.
        }
        const url = new URL(window.location.href);
        url.searchParams.set("file", result.path);
        window.history.replaceState(null, "", url);
        window.setTimeout(() => {
          if (previewRef.current) previewRef.current.scrollTop = 0;
          // A new document must start at the top; Editor preserves its scroll
          // position across content updates so same-document edits stay put.
          editorRef.current?.scrollToRatio(0);
        }, 0);
      } catch (error) {
        if (seq !== openSeqRef.current) return;
        toast(i18n.t("打开失败：{{error}}", { error: errorMessage(error) }), "error");
      } finally {
        if (seq === openSeqRef.current) setLoading(false);
      }
    },
    [cancelTimers, previewOnly, toast],
  );

  const handleManualSave = useCallback(() => {
    if (previewOnly) return;
    void persist({ manual: true });
  }, [persist, previewOnly]);

  const openFromSidebar = useCallback(
    (path: string) => {
      if (window.matchMedia("(max-width: 820px)").matches) {
        setSidebarOpen(false);
      }
      void openDoc(path);
    },
    [openDoc],
  );

  const handleCreateComment = useCallback(
    async (body: string, discussionIds: string[] = [], promptTemplateId = "") => {
      const path = currentPathRef.current;
      const quote = commentQuote;
      if (!path || !quote) throw new Error(i18n.t("当前没有打开的文档"));
      await createComment(path, quote, body, discussionIds, promptTemplateId);
      setCommentQuote(null);
      setCommentsOpen(true);
      void invalidate("comments");
      void invalidate("review");
      void invalidate("inbox");
      toast(i18n.t("评论已添加"), "success");
    },
    [commentQuote, toast],
  );

  const handleCreateDocumentComment = useCallback(
    async (body: string, discussionIds: string[] = [], promptTemplateId = "") => {
      const path = currentPathRef.current;
      if (!path) throw new Error(i18n.t("当前没有打开的文档"));
      await createComment(path, "", body, discussionIds, promptTemplateId, "document");
      setDocumentCommentOpen(false);
      setCommentsOpen(true);
      void invalidate("comments");
      void invalidate("review");
      void invalidate("inbox");
      toast(i18n.t("评论已添加"), "success");
    },
    [toast],
  );

  const handleJumpHeading = useCallback((index: number) => {
    const container = previewRef.current;
    if (!container) return;
    const nodes = container.querySelectorAll(
      ".markdown-body h1, .markdown-body h2, .markdown-body h3, .markdown-body h4, .markdown-body h5, .markdown-body h6",
    );
    nodes[index]?.scrollIntoView({ behavior: "smooth", block: "start" });
  }, []);

  const handleCopyCommentLink = useCallback(
    async (comment: Comment) => {
      const path = currentPathRef.current;
      if (!path) return;
      const url = new URL(window.location.href);
      url.searchParams.set("file", path);
      url.searchParams.set("comment", comment.id);
      try {
        await navigator.clipboard.writeText(url.toString());
        toast(i18n.t("链接已复制"), "success");
      } catch (error) {
        toast(
          i18n.t("复制链接失败：{{error}}", { error: errorMessage(error) }),
          "error",
        );
      }
    },
    [toast],
  );

  const handleExportComments = useCallback(() => {
    const path = currentPathRef.current;
    if (!path) return;
    const lines = [
      `# ${baseName(path)} — comments`,
      "",
      `Path: \`${path}\``,
      "",
    ];
    for (const comment of comments) {
      lines.push(
        `## ${comment.scope === "document" ? "Full document" : `Line ${comment.line}`} · ${comment.resolved ? "resolved" : "open"}${comment.severity ? ` · ${comment.severity}` : ""}`,
        "",
      );
      if (comment.quote) {
        lines.push(`> ${comment.quote.replace(/\n/g, "\n> ")}`, "");
      }
      for (const message of comment.messages) {
        lines.push(
          `**${message.author}** (${new Date(message.createdAt).toLocaleString()}):`,
          "",
          message.body,
          "",
        );
      }
    }
    const blob = new Blob([lines.join("\n")], { type: "text/markdown" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `${baseName(path)}.comments.md`;
    anchor.click();
    URL.revokeObjectURL(url);
    toast(i18n.t("评论已导出"), "success");
  }, [comments, toast]);

  const handleRequestBlockComment = useCallback(
    (quote: string) => {
      setCommentQuote(expandQuoteToBlock(doc.content, quote));
    },
    [doc.content],
  );

  const handleResolveComment = useCallback(
    async (comment: Comment, resolved: boolean) => {
      const path = currentPathRef.current;
      if (!path) return;
      try {
        await resolveComment(path, comment.id, resolved);
        void invalidate("comments");
      } catch (error) {
        toast(i18n.t("更新评论失败：{{error}}", { error: errorMessage(error) }), "error");
      }
    },
    [toast],
  );

  const handleSetReviewStatus = useCallback(
    async (next: ReviewStatus) => {
      const path = currentPathRef.current;
      if (!path) return;
      if (next === "approved") {
        if (!commentsLoaded) {
          if (
            !window.confirm(
              i18n.t("评论还没有加载完成，可能还有未解决项。仍要标记为“已通过”吗？"),
            )
          ) {
            return;
          }
        } else {
          const unresolved = comments.filter(
            (comment) => !comment.resolved,
          ).length;
          if (
            unresolved > 0 &&
            !window.confirm(
              i18n.t("还有 {{count}} 条评论未解决，仍要标记为“已通过”吗？", {
                count: unresolved,
              }),
            )
          ) {
            return;
          }
        }
      }
      try {
        const review = await setDocReview(path, next);
        queryClient.setQueryData(["review", path], review);
        void invalidate("inbox");
        toast(i18n.t("审阅状态已更新"), "success");
      } catch (error) {
        toast(
          i18n.t("更新审阅状态失败：{{error}}", { error: errorMessage(error) }),
          "error",
        );
      }
    },
    [comments, commentsLoaded, toast],
  );

  const handleRequestReview = useCallback(async () => {
    const path = currentPathRef.current;
    if (!path) return;
    setReviewRequestPath(path);
    try {
      await requestDocReview(path);
      void invalidate("review");
      void invalidate("inbox");
      toast(i18n.t("已开始 Agent 审阅"), "success");
    } catch (error) {
      setReviewRequestPath(null);
      toast(
        i18n.t("发起审阅失败：{{error}}", { error: errorMessage(error) }),
        "error",
      );
    }
  }, [toast]);

  useEffect(() => {
    if (!currentPath) return;
    setSeenPaths((prev) => {
      const next = { ...prev, [currentPath]: Date.now() };
      try {
        localStorage.setItem(SEEN_KEY, JSON.stringify(next));
      } catch {
        // Ignore storage restrictions.
      }
      return next;
    });
  }, [currentPath, comments]);

  const handleEnableNotifications = useCallback(async () => {
    if (typeof Notification === "undefined") return;
    try {
      const permission = await Notification.requestPermission();
      setNotificationPermission(permission);
      if (permission === "granted") {
        toast(i18n.t("桌面通知已开启"), "success");
      } else if (permission === "denied") {
        toast(i18n.t("浏览器已阻止桌面通知"), "warn");
      }
    } catch (error) {
      toast(
        i18n.t("开启通知失败：{{error}}", { error: errorMessage(error) }),
        "error",
      );
    }
  }, [toast]);

  const handleReaction = useCallback(
    async (comment: Comment, reaction: string) => {
      const path = currentPathRef.current;
      if (!path) return;
      try {
        await reactComment(path, comment.id, reaction);
        void invalidate("comments");
      } catch (error) {
        toast(
          i18n.t("更新表情失败：{{error}}", { error: errorMessage(error) }),
          "error",
        );
      }
    },
    [toast],
  );

  const handleResolveSuggestion = useCallback(
    async (comment: Comment, accept: boolean) => {
      const path = currentPathRef.current;
      if (!path) return;
      try {
        await resolveSuggestion(path, comment.id, accept);
        void invalidate("comments");
        void invalidate("review");
        void invalidate("inbox");
        toast(accept ? i18n.t("建议已应用") : i18n.t("建议已拒绝"), "success");
      } catch (error) {
        toast(
          i18n.t("处理建议失败：{{error}}", { error: errorMessage(error) }),
          "error",
        );
      }
    },
    [toast],
  );

  const handleReplyComment = useCallback(
    async (
      comment: Comment,
      body: string,
      discussionIds: string[] | undefined,
      promptTemplateId = "",
    ) => {
      const path = currentPathRef.current;
      if (!path) throw new Error(i18n.t("当前没有打开的文档"));
      await replyComment(path, comment.id, body, {
        author: "user",
        discussionIds,
        promptTemplateId,
      });
      void invalidate("comments");
      void invalidate("review");
      void invalidate("inbox");
      toast(i18n.t("已回复，等待 Code Agent 继续修改"), "success");
    },
    [toast],
  );

  const handleGroupComments = useCallback(
    async (comment: Comment, memberIds: string[]) => {
      const path = currentPathRef.current;
      if (!path) throw new Error(i18n.t("当前没有打开的文档"));
      await groupComments(path, comment.id, memberIds);
      void invalidate("comments");
      toast(
        memberIds.length > 0
          ? i18n.t("已关联 {{count}} 条评论，Code Agent 会一起处理", {
              count: memberIds.length + 1,
            })
          : i18n.t("已取消关联"),
        "success",
      );
    },
    [toast],
  );

  const handleRollbackComment = useCallback(
    async (
      comment: Comment,
      message: CommentMessage,
      round: number,
      side: "before" | "after",
    ) => {
      const path = currentPathRef.current;
      if (!path) {
        toast(i18n.t("当前没有打开的文档"), "error");
        return;
      }
      const sideLabel = side === "before" ? i18n.t("前") : i18n.t("后");
      const confirmed = window.confirm(
        i18n.t(
          "只把这个评论对应的段落恢复为「第 {{round}} 次修改{{side}}」的内容？\n\n其他位置的修改会保留。",
          { round, side: sideLabel },
        ),
      );
      if (!confirmed) return;
      try {
        const result = await rollbackComment(path, comment.id, message.id, side);
        if (currentPathRef.current === result.doc.path) {
          applySavedDoc(result.doc.content, result.doc.html);
          setMeta({ size: result.doc.size, modTime: result.doc.modTime });
        }
        void invalidate("comments");
        void invalidate("review");
        void invalidate("inbox");
        toast(
          i18n.t("已局部回滚到第 {{round}} 次修改{{side}}的段落，等待你的确认", {
            round: result.round,
            side: sideLabel,
          }),
          "success",
        );
      } catch (error) {
        toast(i18n.t("回滚失败：{{error}}", { error: errorMessage(error) }), "error");
      }
    },
    [applySavedDoc, toast],
  );

  const handleCreateAgentSession = useCallback(async () => {
    const path = currentPathRef.current;
    if (!path) throw new Error(i18n.t("当前没有打开的文档"));
    try {
      await createAgentSession(path);
      void invalidate("agentSessions");
      toast(i18n.t("已新建 Agent 会话"), "success");
    } catch (error) {
      toast(i18n.t("新建会话失败：{{error}}", { error: errorMessage(error) }), "error");
      throw error;
    }
  }, [toast]);

  const handleSwitchAgentSession = useCallback(
    async (id: string) => {
      const path = currentPathRef.current;
      if (!path) throw new Error(i18n.t("当前没有打开的文档"));
      try {
        await setActiveAgentSession(path, id);
        queryClient.setQueryData(agentSessionsQuery(path).queryKey, (current) =>
          current ? { ...current, activeId: id } : current,
        );
        void invalidate("agentSessions");
        toast(i18n.t("已切换 Agent 会话"), "success");
      } catch (error) {
        toast(i18n.t("切换会话失败：{{error}}", { error: errorMessage(error) }), "error");
        throw error;
      }
    },
    [toast],
  );

  const handleSettingsSaved = useCallback(async () => {
    setSettingsOpen(false);
    // A failed refetch keeps the last known config.
    await invalidate("config");
    toast(i18n.t("设置已保存"), "success");
  }, [toast]);

  const handleCodeRepoChanged = useCallback(
    (message: string) => {
      toast(message, "success");
    },
    [toast],
  );

  const handleTriggerCommentAgent = useCallback(
    async (comment: Comment) => {
      const path = currentPathRef.current;
      if (!path) throw new Error(i18n.t("当前没有打开的文档"));
      try {
        await triggerCommentAgent(path, comment.id);
        void invalidate("comments");
        // The server's queued notice travels over the WebSocket. Keep a
        // local confirmation for the disconnected case, where that notice
        // never arrives and the button would otherwise look inert.
        if (connection !== "open") {
          toast(i18n.t("已通知后台 Agent 处理该评论"), "success");
        }
      } catch (error) {
        toast(i18n.t("启动 Agent 失败：{{error}}", { error: errorMessage(error) }), "error");
        throw error;
      }
    },
    [connection, toast],
  );

  const handleDeleteComment = useCallback(
    async (comment: Comment) => {
      const path = currentPathRef.current;
      if (!path) return;
      if (!window.confirm(i18n.t("删除这条评论？"))) return;
      try {
        await deleteComment(path, comment.id);
        void invalidate("comments");
        toast(i18n.t("评论已删除"), "success");
      } catch (error) {
        toast(i18n.t("删除评论失败：{{error}}", { error: errorMessage(error) }), "error");
      }
    },
    [toast],
  );

  const handleJumpToComment = useCallback(
    (comment: Comment) => {
      if (viewMode === "editor") setViewMode("split");
      window.setTimeout(() => {
        if (!scrollToQuote(previewRef.current, comment.anchor || comment.quote)) {
          toast(i18n.t("未能在预览中定位这段文字"), "warn");
        }
      }, 40);
    },
    [toast, viewMode],
  );

  const handleDelete = useCallback(
    async (path: string) => {
      const removingCurrent = currentPathRef.current === path;
      if (removingCurrent) initialPickRef.current = true;
      try {
        await deleteDoc(path);
        await refreshTree();
        toast(i18n.t("文件已删除"), "success");
        if (removingCurrent) {
          clearCurrent();
          initialPickRef.current = false;
        }
      } catch (error) {
        initialPickRef.current = false;
        toast(i18n.t("删除失败：{{error}}", { error: errorMessage(error) }), "error");
      }
    },
    [clearCurrent, refreshTree, toast],
  );

  const requestNewDoc = useCallback(() => {
    if (!roots.length) {
      toast(i18n.t("请先添加一个目录"), "info");
      setAddRootOpen(true);
      return;
    }
    setNewModal(true);
  }, [roots.length, toast]);

  const closeNewModal = useCallback(() => setNewModal(false), []);
  const closeAddRoot = useCallback(() => setAddRootOpen(false), []);

  const handleAddRoot = useCallback(
    async (path: string) => {
      const result = await addRoot(path);
      setAddRootOpen(false);
      if (!currentPathRef.current) initialPickRef.current = false;
      await refreshTree();
      toast(i18n.t("已添加目录 “{{name}}”", { name: result.root.name }), "success");
      if (result.warning) toast(localizeServerMessage(result.warning, "warning"), "warn");
    },
    [refreshTree, toast],
  );

  const handleAddRemoteRoot = useCallback(
    async (config: AddRemoteRootRequest) => {
      const result = await addRemoteRoot(config);
      setAddRootOpen(false);
      if (!currentPathRef.current) initialPickRef.current = false;
      await refreshTree();
      toast(
        i18n.t("已连接远程服务器 “{{name}}”", {
          name: result.root.remote ?? result.root.name,
        }),
        "success",
      );
      if (result.warning) toast(localizeServerMessage(result.warning, "warning"), "warn");
    },
    [refreshTree, toast],
  );

  const handleRemoveRoot = useCallback(
    async (root: RootInfo) => {
      if (removingRoot) return;
      if (
        !window.confirm(
          i18n.t("从工作区移除目录 “{{name}}”？\n不会删除磁盘上的文件。", {
            name: root.name,
          }),
        )
      ) {
        return;
      }
      setRemovingRoot({ id: root.id, name: root.name });
      const started = Date.now();
      const removingCurrent =
        currentPathRef.current?.startsWith(root.id + "/") ?? false;
      if (removingCurrent) {
        initialPickRef.current = true;
        if (
          currentPathRef.current &&
          contentRef.current !== savedContentRef.current
        ) {
          try {
            await saveDoc(currentPathRef.current, contentRef.current);
            savedContentRef.current = contentRef.current;
          } catch (error) {
            initialPickRef.current = false;
            setRemovingRoot(null);
            toast(
              i18n.t("当前文档保存失败，已取消移除目录：{{error}}", {
                error: errorMessage(error),
              }),
              "error",
            );
            return;
          }
        }
      }
      let removed = false;
      try {
        await removeRoot(root.id);
        await refreshTree();
        removed = true;
        if (removingCurrent) {
          clearCurrent();
          initialPickRef.current = false;
        }
      } catch (error) {
        if (removingCurrent) initialPickRef.current = false;
        toast(i18n.t("移除目录失败：{{error}}", { error: errorMessage(error) }), "error");
      } finally {
        // Keep the spinner visible long enough to be perceivable even when the
        // server responds immediately.
        const remaining = 420 - (Date.now() - started);
        if (remaining > 0) {
          await new Promise((resolve) => window.setTimeout(resolve, remaining));
        }
        setRemovingRoot(null);
        if (removed) {
          toast(i18n.t("已移除目录 “{{name}}”", { name: root.name }), "success");
        }
      }
    },
    [clearCurrent, refreshTree, removingRoot, toast],
  );

  const handleCreate = useCallback(
    async (path: string, content = "") => {
      const result = await saveDoc(path, content, true);
      setNewModal(false);
      await refreshTree();
      await openDoc(result.path);
      toast(
        i18n.t(
          previewOnly
            ? "文档已创建；审阅模式下请通过 Agent 或导入写入内容。"
            : "文档已创建",
        ),
        "success",
      );
    },
    [openDoc, previewOnly, refreshTree, toast],
  );

  const handleImport = useCallback(
    async (files: FileList) => {
      const candidates = Array.from(files).filter((file) =>
        isMarkdownPath(file.name),
      );
      if (!candidates.length) {
        toast(i18n.t("请选择 Markdown 文件"), "warn");
        return;
      }
      const targetRoot = currentFile
        ? roots.find((root) => root.id === currentFile.rootId)
        : primaryRoot;
      if (!targetRoot) {
        toast(i18n.t("请先添加一个目录"), "warn");
        return;
      }
      let firstPath: string | null = null;
      let imported = 0;
      for (const file of candidates) {
        const content = await file.text();
        const base = file.name.replace(/[\\/]/g, "-");
        let attempt = 0;
        let target = `${targetRoot.id}/${base}`;
        for (let guard = 0; guard < 60; guard += 1) {
          try {
            const result = await saveDoc(target, content, true);
            firstPath ??= result.path;
            imported += 1;
            break;
          } catch (error) {
            if (
              error instanceof ApiError &&
              (error.code === "file_name_already_exists" ||
                error.code === "file_already_exists") &&
              attempt < 50
            ) {
              attempt += 1;
              target = `${targetRoot.id}/${suffixedName(base, attempt)}`;
              continue;
            }
            toast(i18n.t("导入 {{file}} 失败：{{error}}", { file: file.name, error: errorMessage(error) }), "error");
            break;
          }
        }
      }
      await refreshTree();
      if (firstPath) await openDoc(firstPath);
      if (imported > 0) {
        toast(i18n.t("已导入 {{count}} 个文件", { count: imported }), "success");
      }
    },
    [currentFile, openDoc, primaryRoot, refreshTree, roots, toast],
  );

  // Stable props let the memoized Sidebar/Preview skip unrelated re-renders.
  const handleSidebarDelete = useCallback(
    (path: string) => void handleDelete(path),
    [handleDelete],
  );
  const handleSidebarImport = useCallback(
    (files: FileList) => void handleImport(files),
    [handleImport],
  );
  const handleSidebarRemoveRoot = useCallback(
    (root: RootInfo) => void handleRemoveRoot(root),
    [handleRemoveRoot],
  );
  const openAddRoot = useCallback(() => setAddRootOpen(true), []);
  const handlePreviewOpenDoc = useCallback(
    (path: string) => void openDoc(path),
    [openDoc],
  );

  const releaseScrollOwner = useCallback(() => {
    if (scrollReleaseTimer.current !== undefined) {
      window.clearTimeout(scrollReleaseTimer.current);
    }
    scrollReleaseTimer.current = window.setTimeout(() => {
      scrollOwner.current = null;
      scrollReleaseTimer.current = undefined;
    }, 160);
  }, []);

  const handleEditorScroll = useCallback(
    (ratio: number) => {
      if (scrollOwner.current === "preview") return;
      scrollOwner.current = "editor";
      const preview = previewRef.current;
      if (preview) {
        preview.scrollTop =
          ratio * Math.max(0, preview.scrollHeight - preview.clientHeight);
      }
      releaseScrollOwner();
    },
    [releaseScrollOwner],
  );

  const handlePreviewScroll = useCallback(
    (ratio: number) => {
      if (scrollOwner.current === "editor") return;
      scrollOwner.current = "preview";
      editorRef.current?.scrollToRatio(ratio);
      releaseScrollOwner();
    },
    [releaseScrollOwner],
  );

  const startSplitDrag = (event: ReactPointerEvent<HTMLDivElement>) => {
    event.preventDefault();
    const container = splitRef.current;
    if (!container) return;
    const rect = container.getBoundingClientRect();
    const vertical = window.matchMedia("(max-width: 820px)").matches;
    const move = (moveEvent: PointerEvent) => {
      const percent = vertical
        ? ((moveEvent.clientY - rect.top) / rect.height) * 100
        : ((moveEvent.clientX - rect.left) / rect.width) * 100;
      setSplit(clamp(percent, 20, 80));
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", up);
      window.removeEventListener("blur", up);
      document.body.classList.remove("dragging-split");
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    window.addEventListener("pointercancel", up);
    window.addEventListener("blur", up);
    document.body.classList.add("dragging-split");
  };

  const handleMessage = (message: WsServerMessage) => {
    switch (message.type) {
      case "preview":
        if (message.path === currentPathRef.current) {
          setHtml(message.html ?? "");
        }
        break;
      case "doc": {
        if (message.path !== currentPathRef.current) break;
        const incomingContent = message.content ?? "";
        const incomingHtml = message.html ?? "";
        const dirty = contentRef.current !== savedContentRef.current;
        if (message.origin === "save") {
          if (incomingContent === contentRef.current) {
            savedContentRef.current = incomingContent;
            setHtml(incomingHtml);
            setStatus("saved");
            statusRef.current = "saved";
          } else if (!dirty) {
            applySavedDoc(incomingContent, incomingHtml);
          } else if (incomingContent !== savedContentRef.current) {
            toast(
              i18n.t("另一个窗口保存了不同内容；本地改动会在自动保存后覆盖"),
              "warn",
            );
          }
          break;
        }
        if (message.origin === "init") {
          if (incomingContent === contentRef.current) {
            setHtml(incomingHtml);
          } else if (!dirty) {
            applySavedDoc(incomingContent, incomingHtml);
          }
          break;
        }
        if (message.origin === "suggestion") {
          if (incomingContent !== contentRef.current && dirty) {
            toast(
              i18n.t("已接受建议，未保存的本地改动已被覆盖"),
              "warn",
            );
          }
          applySavedDoc(incomingContent, incomingHtml);
          break;
        }
        if (message.origin === "restore") {
          if (incomingContent !== contentRef.current && dirty) {
            toast(
              i18n.t("已恢复版本，未保存的本地改动已被覆盖"),
              "warn",
            );
          }
          applySavedDoc(incomingContent, incomingHtml);
          break;
        }
        if (message.origin === "rollback") {
          if (incomingContent !== contentRef.current && dirty) {
            toast(i18n.t("评论段落已局部回滚，未保存的本地改动已被覆盖"), "warn");
          }
          applySavedDoc(incomingContent, incomingHtml);
          break;
        }
        if (message.origin === "external") {
          if (incomingContent === contentRef.current) {
            savedContentRef.current = incomingContent;
            setHtml(incomingHtml);
            setStatus("saved");
            statusRef.current = "saved";
            break;
          }
          if (dirty) {
            toast(i18n.t("文件已被外部修改，已载入磁盘上的新内容"), "warn");
          }
          applySavedDoc(incomingContent, incomingHtml);
          break;
        }
        // origin === "remote": another commentloop window is typing.
        if (previewOnly) {
          setHtml(incomingHtml);
          break;
        }
        if (incomingContent === contentRef.current) {
          setHtml(incomingHtml);
          break;
        }
        cancelTimers();
        contentRef.current = incomingContent;
        docVersionRef.current += 1;
        setDoc({ content: incomingContent, version: docVersionRef.current });
        setHtml(incomingHtml);
        setStats(computeStats(incomingContent));
        setStatus("dirty");
        statusRef.current = "dirty";
        scheduleSave();
        break;
      }
      case "tree":
        scheduleTreeRefresh();
        void invalidate("inbox");
        break;
      case "comments":
        void invalidate("comments");
        void invalidate("review");
        void invalidate("inbox");
        break;
      case "reviews":
        if (message.status) {
          queryClient.setQueryData(
            ["review", message.path],
            (current: DocReview | undefined) => ({
              ...(current ?? { path: message.path }),
              path: message.path,
              status: message.status as DocReview["status"],
            }),
          );
        }
        void invalidate("review");
        void invalidate("inbox");
        break;
      case "discussions":
        void invalidate("discussions");
        break;
      case "consensus":
        if (message.path === currentPathRef.current && message.status) {
          setConsensusActivity({
            path: message.path,
            status: message.status,
            message: message.message,
          });
        }
        if (
          !message.status ||
          message.status === "done" ||
          message.status === "error"
        ) {
          void invalidate("discussions");
        }
        break;
      case "agent":
        void invalidate("inbox");
        if (
          message.status === "done" &&
          notificationPermission === "granted" &&
          document.hidden
        ) {
          try {
            const suffix = message.path ? ` · ${baseName(message.path)}` : "";
            const text =
              localizeServerMessage(message.message || "", "done") ||
              t("Agent 已完成任务");
            new Notification("Commentloop", { body: text + suffix });
          } catch {
            // Notifications can be unavailable despite permission.
          }
        }
        if (!message.path || message.path === currentPathRef.current) {
          const status =
            message.status === "error"
              ? "error"
              : message.status === "done"
                ? "done"
                : message.status === "queued"
                  ? "queued"
                  : "working";
          setAgentActivity({
            status,
            message: localizeServerMessage(message.message || "", status as "queued" | "working" | "done" | "error") || i18n.t("后台 Agent 正在处理…"),
          });
          if (status === "done" || status === "error") {
            setReviewRequestPath((current) =>
              current && (!message.path || message.path === current)
                ? null
                : current,
            );
          }
          if (status === "queued" || status === "done" || status === "error") {
            toast(
              localizeServerMessage(message.message || "", status as "queued" | "working" | "done" | "error") || i18n.t("后台 Agent 状态更新"),
              status === "error" ? "error" : status === "done" ? "success" : "info",
            );
          }
        }
        break;
      case "reload":
        window.location.reload();
        break;
      case "gone":
        if (message.path === currentPathRef.current) {
          toast(i18n.t("文件已被删除或移动"), "warn");
          initialPickRef.current = true;
          clearCurrent();
          void refreshTree().finally(() => {
            initialPickRef.current = false;
            void invalidate("inbox");
          });
        }
        break;
      case "error":
        toast(localizeServerMessage(message.message || "", "error"), "error");
        break;
      case "subscribed":
      case "unsubscribed":
        break;
    }
  };
  messageHandlerRef.current = handleMessage;

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  useEffect(() => {
    void refreshTree();
  }, [refreshTree]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setSearchOpen(true);
        return;
      }
      if (event.defaultPrevented || event.metaKey || event.ctrlKey || event.altKey) {
        return;
      }
      if (event.key === "Escape") {
        if (documentCommentOpen) setDocumentCommentOpen(false);
        else if (commentQuote !== null) setCommentQuote(null);
        else if (historyOpen) setHistoryOpen(false);
        else if (inboxOpen) setInboxOpen(false);
        else if (settingsOpen) setSettingsOpen(false);
        else if (commentsOpen) setCommentsOpen(false);
        else return;
        event.preventDefault();
        return;
      }
      const target = event.target as HTMLElement | null;
      if (
        target?.tagName === "INPUT" ||
        target?.tagName === "TEXTAREA" ||
        target?.tagName === "SELECT" ||
        target?.isContentEditable
      ) {
        return;
      }
      if (event.key.toLowerCase() === "c" && currentPath) {
        setCommentsOpen((value) => !value);
        event.preventDefault();
      } else if (event.key.toLowerCase() === "h" && currentPath) {
        setHistoryOpen((value) => !value);
        event.preventDefault();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [
    commentQuote,
    commentsOpen,
    currentPath,
    documentCommentOpen,
    historyOpen,
    inboxOpen,
    settingsOpen,
  ]);

  useEffect(() => {
    if (!currentPath) return;
    if (new URLSearchParams(window.location.search).get("comment")) {
      setCommentsOpen(true);
    }
  }, [currentPath]);

  const configError = configResult.data ? null : configResult.error;
  useEffect(() => {
    if (configError) {
      toast(i18n.t("无法读取配置：{{error}}", { error: errorMessage(configError) }), "error");
    }
  }, [configError, toast]);

  useEffect(() => {
    if (config?.debug && !config.hotReload) {
      toast(
        i18n.t("调试模式未找到 web/dist，前端热更新不可用；已回退到内嵌资源"),
        "warn",
      );
    }
  }, [config, toast]);

  useEffect(() => {
    if (!tree || currentPathRef.current || initialPickRef.current) return;
    const requested = new URLSearchParams(window.location.search).get("file");
    const stored = readLocal(LAST_FILE_KEY);
    const files = tree.files;
    const candidates = [requested, stored].filter(
      (value): value is string => Boolean(value),
    );
    const match = (value: string) =>
      files.find((file) => file.path === value) ??
      files.find((file) => file.relPath === value) ??
      files.find((file) => file.path.endsWith("/" + value));
    const found = candidates.map(match).find(Boolean);
    const mostRecent = [...files].sort(
      (a, b) => Date.parse(b.modTime) - Date.parse(a.modTime),
    )[0];
    const pick = found?.path ?? mostRecent?.path;
    if (!pick) return;
    initialPickRef.current = true;
    void openDoc(pick);
  }, [tree, currentPath, openDoc]);

  useEffect(() => {
    if (!tree || !currentPath || tree.truncated) return;
    if (tree.files.some((file) => file.path === currentPath)) return;
    initialPickRef.current = false;
    toast(i18n.t("当前文件已不在工作区中，正在切换"), "warn");
    clearCurrent();
  }, [tree, currentPath, clearCurrent, toast]);

  useEffect(() => {
    // Anchors and orphan state depend on the document text.
    if (currentPath) void invalidate("comments", { cancelRefetch: false });
  }, [currentPath, doc.version]);

  useEffect(() => {
    setCodeRepoModalOpen(false);
    setCodeBrowserOpen(false);
    setDiscussionModalOpen(false);
    setAgentActivity(null);
    setConsensusActivity(null);
  }, [currentPath]);

  useEffect(() => {
    if (
      !agentActivity ||
      (agentActivity.status !== "done" && agentActivity.status !== "error")
    ) {
      return;
    }
    const timer = window.setTimeout(() => setAgentActivity(null), 6000);
    return () => window.clearTimeout(timer);
  }, [agentActivity]);

  useEffect(() => {
    if (
      !consensusActivity ||
      (consensusActivity.status !== "done" &&
        consensusActivity.status !== "error")
    ) {
      return;
    }
    const timer = window.setTimeout(() => setConsensusActivity(null), 6000);
    return () => window.clearTimeout(timer);
  }, [consensusActivity]);

  useEffect(() => {
    if (connection !== "open") return;
    if (currentPath) {
      send({ type: "subscribe", path: currentPath });
    } else {
      send({ type: "unsubscribe" });
    }
  }, [connection, currentPath, send]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented) return;
      const mod = event.metaKey || event.ctrlKey;
      if (!mod) return;
      const key = event.key.toLowerCase();
      if (key === "b") {
        event.preventDefault();
        setSidebarOpen((value) => !value);
        return;
      }
      if (key === "s") {
        event.preventDefault();
        if (!previewOnly) handleManualSave();
      } else if (key === "n" && event.shiftKey) {
        event.preventDefault();
        requestNewDoc();
      } else if (key === "\\") {
        event.preventDefault();
        if (!previewOnly) {
          setViewMode((mode) =>
            mode === "split"
              ? "editor"
              : mode === "editor"
                ? "preview"
                : "split",
          );
        }
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [handleManualSave, previewOnly, requestNewDoc]);

  useEffect(() => {
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      if (!previewOnly && contentRef.current !== savedContentRef.current) {
        event.preventDefault();
        event.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [previewOnly]);

  useEffect(() => {
    try {
      localStorage.setItem(SIDEBAR_KEY, sidebarOpen ? "1" : "0");
    } catch {
      // Ignore storage restrictions.
    }
  }, [sidebarOpen]);

  useEffect(() => {
    try {
      localStorage.setItem(VIEW_KEY, viewMode);
    } catch {
      // Ignore storage restrictions.
    }
  }, [viewMode]);

  useEffect(() => {
    const timer = window.setTimeout(() => {
      try {
        localStorage.setItem(SPLIT_KEY, String(split));
      } catch {
        // Ignore storage restrictions.
      }
    }, 200);
    return () => window.clearTimeout(timer);
  }, [split]);

  useEffect(() => {
    document.title = currentPath
      ? `${baseName(currentPath)} · Commentloop`
      : t("Commentloop · AI 协作审阅");
  }, [currentPath, t]);

  useEffect(() => {
    if (previewOnly || viewMode === "preview") return;
    const timer = window.setTimeout(() => editorRef.current?.focus(), 60);
    return () => window.clearTimeout(timer);
  }, [previewOnly, viewMode]);

  if (!configLoaded) {
    return (
      <div className="boot-screen">
        <div className="boot-mark">C</div>
        <div className="boot-title">Commentloop</div>
        <span className="spinner" />
      </div>
    );
  }

  const connectionLabel =
    connection === "open"
      ? t("实时同步")
      : connection === "connecting"
        ? t("连接中…")
        : t("重新连接中…");

  const dirty = status !== "saved";

  return (
    <ConfigProvider
      locale={i18nHook.language === "zh-CN" ? zhCN : enUS}
      theme={{
        algorithm:
          theme === "dark" ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
        token: {
          colorPrimary: "#2563eb",
          borderRadius: 6,
          fontFamily:
            '-apple-system, BlinkMacSystemFont, "SF Pro Text", "Segoe UI", "Noto Sans SC", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif',
          colorBgLayout: theme === "dark" ? "#0d1117" : "#f6f7f9",
          colorBgContainer: theme === "dark" ? "#161b22" : "#ffffff",
          colorBorder: theme === "dark" ? "rgba(240,246,252,0.16)" : "rgba(15,23,42,0.16)",
          colorBorderSecondary:
            theme === "dark" ? "rgba(240,246,252,0.08)" : "rgba(15,23,42,0.09)",
          colorText: theme === "dark" ? "#e6edf3" : "#0f172a",
          colorTextSecondary: theme === "dark" ? "#9da7b3" : "#4b5563",
          colorTextTertiary: theme === "dark" ? "#6e7681" : "#9aa3af",
        },
      }}
    >
    <div className={`app${sidebarOpen ? " sidebar-visible" : ""}`}>
      <header className="topbar">
        <div className="topbar-left">
          <Tooltip
            title={sidebarOpen ? t("隐藏侧边栏 (⌘B)") : t("显示侧边栏 (⌘B)")}
            placement="bottom"
          >
            <Button
              type="text"
              onClick={() => setSidebarOpen((value) => !value)}
              aria-label={t("切换侧边栏")}
              aria-pressed={sidebarOpen}
              icon={<IconPanelLeft size={17} />}
            />
          </Tooltip>
          <div className="brand">
            <span className="brand-mark">C</span>
            <span className="brand-name">Commentloop</span>
          </div>
          {debugMode ? (
            <Tooltip
              title={
                config?.hotReload
                  ? t("调试模式：前端资源变化后自动刷新")
                  : t("调试模式：热更新目录不可用")
              }
            >
              <Tag color="purple">DEBUG</Tag>
            </Tooltip>
          ) : null}
          {previewOnly ? (
            <Tooltip title={t("审阅模式：前台只读；后台 Agent 可写入，评论与局部回滚可用")}>
              <Tag>{t("审阅模式")}</Tag>
            </Tooltip>
          ) : null}
        </div>

        <div className="doc-identity">
          {currentPath ? (
            <>
              <span className="doc-name">
                {currentFile?.name ?? baseName(currentPath)}
              </span>
              <span className="doc-subline">
                <span className="doc-parent">{currentLocation}</span>
                <ReviewStatusSelect
                  value={reviewStatus}
                  onChange={(next) => void handleSetReviewStatus(next)}
                />
                <AgentSessionPicker
                  sessions={agentSessions}
                  activeId={activeAgentSessionId}
                  onSwitch={handleSwitchAgentSession}
                  onCreate={handleCreateAgentSession}
                />
              </span>
            </>
          ) : (
            <span className="doc-name empty">{t("未打开文档")}</span>
          )}
        </div>

        <div className="topbar-actions">
          {!previewOnly && currentPath ? (
            <SaveIndicator status={status} />
          ) : null}
          {!previewOnly ? (
            <Segmented
              size="small"
              aria-label={t("视图模式")}
              value={viewMode}
              onChange={(value) => setViewMode(value as ViewMode)}
              options={VIEW_OPTIONS.map((option) => ({
                value: option.value,
                label: (
                  <span title={t(option.label)}>
                    <span className="sr-only">{t(option.label)}</span>
                    {option.icon}
                  </span>
                ),
              }))}
            />
          ) : null}
          <Tooltip title={t("全文搜索 (⌘K)")} placement="bottom">
            <Button
              type="text"
              onClick={() => setSearchOpen(true)}
              aria-label={t("全文搜索")}
              icon={<IconSearch size={17} />}
            />
          </Tooltip>
          <Tooltip title={t("收件箱")} placement="bottom">
            <Badge count={inboxAttention} size="small" color="#2563eb" offset={[-2, 2]}>
              <Button
                type={inboxOpen ? "primary" : "text"}
                onClick={() => {
                  setInboxOpen(true);
                  void inboxResult.refetch();
                }}
                aria-label={t("收件箱")}
                icon={<IconInbox size={17} />}
              />
            </Badge>
          </Tooltip>
          <Tooltip title={t("设置")} placement="bottom">
            <Button
              type="text"
              onClick={() => setSettingsOpen(true)}
              aria-label={t("设置")}
              icon={<IconSettings size={17} />}
            />
          </Tooltip>
          <Tooltip
            title={theme === "dark" ? t("切换到浅色") : t("切换到深色")}
            placement="bottom"
          >
            <Button
              type="text"
              onClick={() =>
                setTheme((value) => (value === "dark" ? "light" : "dark"))
              }
              aria-label={t("切换主题")}
              icon={
                theme === "dark" ? <IconSun size={17} /> : <IconMoon size={17} />
              }
            />
          </Tooltip>
          <Tooltip
            title={i18nHook.language === "zh-CN" ? "English" : "简体中文"}
            placement="bottom"
          >
            <Button
              type="text"
              onClick={() =>
                void i18nHook.changeLanguage(
                  i18nHook.language === "zh-CN" ? "en" : "zh-CN",
                )
              }
              aria-label={
                i18nHook.language === "zh-CN"
                  ? "Switch to English"
                  : "切换到中文"
              }
            >
              {i18nHook.language === "zh-CN" ? "EN" : "中"}
            </Button>
          </Tooltip>
          <Tooltip title={t("新建文档 (⌘⇧N)")} placement="bottom">
            <Button
              type="primary"
              onClick={requestNewDoc}
              icon={<IconPlus size={15} />}
            >
              {t("新建")}
            </Button>
          </Tooltip>
        </div>
      </header>

      <div className="app-body">
        <button
          type="button"
          className={`sidebar-backdrop${sidebarOpen ? " show" : ""}`}
          onClick={() => setSidebarOpen(false)}
          aria-label={t("关闭侧边栏")}
        />
        <Sidebar
          open={sidebarOpen}
          tree={tree}
          currentPath={currentPath}
          dirty={dirty}
          removingRootId={removingRoot?.id ?? null}
          removingRootName={removingRoot?.name ?? ""}
          onOpen={openFromSidebar}
          onDelete={handleSidebarDelete}
          onNew={requestNewDoc}
          onImport={handleSidebarImport}
          onAddRoot={openAddRoot}
          onRemoveRoot={handleSidebarRemoveRoot}
          reviewByPath={inboxByPath}
          unreadPaths={unreadPaths}
        />

        <main className="main">
          {loading ? <div className="loading-bar" /> : null}
          {currentPath ? (
            <div
              ref={splitRef}
              className={`workspace mode-${effectiveViewMode}`}
              style={
                !previewOnly && effectiveViewMode === "split"
                  ? ({
                      "--split-a": `${split}fr`,
                      "--split-b": `${100 - split}fr`,
                    } as CSSProperties)
                  : undefined
              }
            >
              {!previewOnly ? (
                <section className="pane editor-pane">
                  <div className="pane-head">
                    <span className="pane-title">
                      <IconPen size={14} />
                      {t("编辑")}
                    </span>
                    <span className="pane-meta">{t("自动保存")}</span>
                  </div>
                  <Editor
                    ref={editorRef}
                    document={doc}
                    theme={theme}
                    onChange={handleChange}
                    onSave={handleManualSave}
                    onScrollRatio={handleEditorScroll}
                  />
                </section>
              ) : null}

              {!previewOnly && effectiveViewMode === "split" ? (
                <div
                  className="split-handle"
                  onPointerDown={startSplitDrag}
                  onDoubleClick={() => setSplit(50)}
                  title={t("拖动调整分栏，双击恢复 50/50")}
                  role="separator"
                  aria-orientation="vertical"
                >
                  <span />
                </div>
              ) : null}

              <section className="pane preview-pane">
                <div className="pane-head">
                  <span className="pane-title">
                    <IconEye size={14} />
                    {t("预览")}
                  </span>
                  {previewOnly ? (
                    <span className="pane-meta">{t("前台只读 · Agent 修改自动更新")}</span>
                  ) : null}
                  <div className="pane-head-right">
                    {agentActivity ? (
                      <span
                        className={`agent-activity ${agentActivity.status}`}
                        title={agentActivity.message}
                      >
                        {agentActivity.status === "done" ? (
                          <IconCheck size={12} />
                        ) : agentActivity.status === "error" ? (
                          <IconAlert size={12} />
                        ) : (
                          <span className="spinner" />
                        )}
                        <span>{agentActivity.message}</span>
                      </span>
                    ) : null}
                    {currentPath ? (
                      <Tooltip
                        title={
                          codeRepos.length
                            ? t("已关联 {{count}} 个代码仓库", { count: codeRepos.length })
                            : t("关联代码仓库")
                        }
                      >
                        <Badge count={codeRepos.length} size="small" color="#2563eb" offset={[3, -3]}>
                          <Button
                            size="small"
                            icon={<IconGitBranch size={13} />}
                            onClick={() => {
                              if (codeRepos.length > 0) {
                                setCodeBrowserOpen(true);
                              } else {
                                setCodeRepoModalOpen(true);
                              }
                            }}
                          >
                            {t("代码")}
                          </Button>
                        </Badge>
                      </Tooltip>
                    ) : null}
                    {currentPath ? (
                      <Tooltip title={t("导入讨论并维护文档共识")}>
                        <Badge
                          count={discussions.length}
                          size="small"
                          color="#2563eb"
                          offset={[3, -3]}
                        >
                          <Button
                            size="small"
                            icon={<IconMessage size={13} />}
                            onClick={() => setDiscussionModalOpen(true)}
                            loading={
                              consensusActivity?.status !== "done" &&
                              consensusActivity?.status !== "error" &&
                              Boolean(consensusActivity)
                            }
                          >
                            {t("讨论")}
                          </Button>
                        </Badge>
                      </Tooltip>
                    ) : null}
                    <Tooltip title={t("查看 / 添加评论")}>
                      <Badge count={comments.length} size="small" color="#2563eb" offset={[3, -3]}>
                        <Button
                          size="small"
                          type={commentsOpen ? "primary" : "default"}
                          icon={<IconMessage size={13} />}
                          onClick={() => setCommentsOpen((value) => !value)}
                          aria-pressed={commentsOpen}
                        >
                          {t("评论")}
                        </Button>
                      </Badge>
                    </Tooltip>
                    <Dropdown
                      disabled={headings.length === 0}
                      trigger={["click"]}
                      menu={{
                        items: headings.map((heading, index) => ({
                          key: String(index),
                          label: (
                            <span style={{ paddingLeft: `${(heading.level - 1) * 12}px` }}>
                              {heading.text}
                            </span>
                          ),
                        })),
                        onClick: ({ key }) => handleJumpHeading(Number(key)),
                      }}
                    >
                      <Tooltip title={t("大纲")}>
                        <Button size="small" icon={<IconFileText size={13} />}>
                          {t("大纲")}
                        </Button>
                      </Tooltip>
                    </Dropdown>
                    <Tooltip title={t("版本历史 (H)")}>
                      <Button
                        size="small"
                        type={historyOpen ? "primary" : "default"}
                        icon={<IconHistory size={13} />}
                        onClick={() => setHistoryOpen((value) => !value)}
                      >
                        {t("历史")}
                      </Button>
                    </Tooltip>
                    <Badge
                      status={connection === "open" ? "processing" : "default"}
                      text={
                        connection === "open" ? t("实时") : t("离线渲染")
                      }
                    />
                  </div>
                </div>
                <Preview
                  key={currentPath}
                  html={html}
                  currentPath={currentPath}
                  comments={comments}
                  containerRef={previewRef}
                  onScrollRatio={handlePreviewScroll}
                  onOpenDoc={handlePreviewOpenDoc}
                  onRequestComment={setCommentQuote}
                  onRequestBlockComment={handleRequestBlockComment}
                  theme={theme}
                  readOnly={previewOnly}
                />
                {commentsOpen ? (
                  <CommentsPanel
                    key={currentPath ?? ""}
                    comments={comments}
                    docPath={currentPath ?? undefined}
                    onOpenDoc={handlePreviewOpenDoc}
                    review={docReview}
                    reviewing={reviewRequestPath === currentPath}
                    onRequestReview={() => void handleRequestReview()}
                    onSuggestion={(comment, accept) =>
                      void handleResolveSuggestion(comment, accept)
                    }
                    onReaction={(comment, reaction) =>
                      void handleReaction(comment, reaction)
                    }
                    onNewDocumentComment={() => setDocumentCommentOpen(true)}
                    onCopyLink={(comment) => void handleCopyCommentLink(comment)}
                    onExport={handleExportComments}
                    highlightId={new URLSearchParams(window.location.search).get("comment")}
                    onClose={() => setCommentsOpen(false)}
                    onJump={handleJumpToComment}
                    onResolve={(comment, resolved) =>
                      void handleResolveComment(comment, resolved)
                    }
                    onDelete={(comment) => void handleDeleteComment(comment)}
                    agentName={config?.agentName || "Code Agent"}
                    discussions={discussions}
                    discussionsLoaded={discussionsResult.isSuccess}
                    promptTemplates={promptTemplates}
                    onReply={handleReplyComment}
                    onGroup={handleGroupComments}
                    onRollback={(comment, message, round, side) =>
                      void handleRollbackComment(comment, message, round, side)
                    }
                    onTrigger={handleTriggerCommentAgent}
                  />
                ) : null}
              </section>
            </div>
          ) : (
            <Welcome
              tree={tree}
              config={config}
              readOnly={previewOnly}
              onNew={requestNewDoc}
              onAddRoot={() => setAddRootOpen(true)}
              onBrowse={() => setSidebarOpen(true)}
              onOpen={(path) => void openDoc(path)}
            />
          )}
        </main>
      </div>

      <footer className="statusbar">
        <div className="status-left">
          <span className={`conn-dot ${connection}`} />
          <span className="conn-label">{connectionLabel}</span>
          {currentPath ? (
            <>
              {previewOnly ? (
                <span className="foot-readonly">{t("· 审阅模式")}</span>
              ) : status === "saved" ? (
                <span className="foot-sep">·</span>
              ) : status === "saving" ? (
                <span className="foot-sep">{t("· 保存中…")}</span>
              ) : (
                <span className="foot-unsaved">{t("· 未保存")}</span>
              )}
              {meta ? (
                <>
                  <span className="foot-sep">·</span>
                  <span>{formatBytes(meta.size)}</span>
                </>
              ) : null}
            </>
          ) : null}
        </div>
        <div className="status-right">
          {currentPath ? (
            <>
              <span>{t("{{count}} 字", { count: stats.words })}</span>
              <span>{t("{{count}} 字符", { count: stats.chars })}</span>
              <span>{t("{{count}} 行", { count: stats.lines })}</span>
              <span
                className="status-path"
                title={
                  currentFile
                    ? `${currentRootLocation}/${currentFile.relPath}`
                    : currentPath
                }
              >
                {currentFile
                  ? `${currentFile.rootName}/${currentFile.relPath}`
                  : currentPath}
              </span>
            </>
          ) : (
            <span
              className="status-path"
              title={roots.map((root) => root.path).join("\n")}
            >
              {roots.length
                ? t("{{count}} 个目录", { count: roots.length })
                : (config?.root ?? "")}
            </span>
          )}
        </div>
      </footer>

      {newModal ? (
        <NewDocModal
          defaultDir={defaultDir}
          displayDir={displayDir}
          exists={(path) =>
            Boolean(tree?.files.some((file) => file.path === path))
          }
          onClose={closeNewModal}
          onCreate={handleCreate}
        />
      ) : null}

      {commentQuote ? (
        <CommentModal
          quote={commentQuote}
          agentName={config?.agentName || "Code Agent"}
          discussions={discussions}
          promptTemplates={promptTemplates}
          onClose={() => setCommentQuote(null)}
          onSubmit={handleCreateComment}
        />
      ) : null}

      {searchOpen ? (
        <SearchModal
          onClose={() => setSearchOpen(false)}
          onOpen={(path) => {
            setSearchOpen(false);
            void openDoc(path);
          }}
        />
      ) : null}

      {documentCommentOpen && currentPath ? (
        <CommentModal
          quote=""
          documentComment
          agentName={config?.agentName || "Code Agent"}
          discussions={discussions}
          promptTemplates={promptTemplates}
          onClose={() => setDocumentCommentOpen(false)}
          onSubmit={handleCreateDocumentComment}
        />
      ) : null}

      {historyOpen && currentPath ? (
        <HistoryModal
          path={currentPath}
          currentContent={contentRef.current}
          canRestore={!previewOnly}
          onClose={() => setHistoryOpen(false)}
          onRestored={(restored) => {
            setHistoryOpen(false);
            // A slow restore may finish after the user switched documents.
            if (currentPathRef.current !== restored.path) return;
            applySavedDoc(restored.content, restored.html);
            setMeta({ size: restored.size, modTime: restored.modTime });
            toast(i18n.t("已恢复到所选版本"), "success");
          }}
        />
      ) : null}
      {inboxOpen ? (
        <InboxModal
          docs={inboxDocs}
          counts={inboxCounts}
          error={inboxError}
          notificationsEnabled={notificationPermission === "granted"}
          onEnableNotifications={() => void handleEnableNotifications()}
          onClose={() => setInboxOpen(false)}
          onOpen={(path) => {
            setInboxOpen(false);
            void openDoc(path);
          }}
        />
      ) : null}

      {settingsOpen ? (
        <SettingsModal
          onClose={() => setSettingsOpen(false)}
          onSaved={() => void handleSettingsSaved()}
        />
      ) : null}

      {discussionModalOpen && currentPath ? (
        <DiscussionModal
          docPath={currentPath}
          activity={
            consensusActivity?.path === currentPath
              ? consensusActivity
              : undefined
          }
          onClose={() => setDiscussionModalOpen(false)}
        />
      ) : null}

      {codeBrowserOpen && currentPath ? (
        <CodeBrowserModal
          docPath={currentPath}
          repos={codeRepos}
          theme={theme}
          onClose={() => setCodeBrowserOpen(false)}
          onManageRepos={() => setCodeRepoModalOpen(true)}
          onBranchChanged={() => {
            void invalidate("docCodeRepos");
            void invalidate("codeRepos");
            toast(t("已切换分支"), "success");
          }}
        />
      ) : null}

      {codeRepoModalOpen && currentPath ? (
        <CodeRepoModal
          docPath={currentPath}
          initialBrowsePath={primaryRoot ? parentDir(primaryRoot.path) : ""}
          onClose={() => setCodeRepoModalOpen(false)}
          onChanged={handleCodeRepoChanged}
        />
      ) : null}

      {addRootOpen ? (
        <AddRootModal
          roots={roots}
          initialPath={primaryRoot ? parentDir(primaryRoot.path) : ""}
          onClose={closeAddRoot}
          onAddLocal={handleAddRoot}
          onAddRemote={handleAddRemoteRoot}
        />
      ) : null}

      <Toasts items={toasts} onDismiss={dismissToast} />
    </div>
    </ConfigProvider>
  );
}

function SaveIndicator({ status }: { status: SaveStatus }) {
  const { t } = useTranslation();
  return (
    <span className={`save-state ${status}`} title={t("自动保存已开启")}>
      {status === "saved" ? (
        <IconCheck size={13} />
      ) : status === "saving" ? (
        <span className="spinner" />
      ) : (
        <span className="unsaved-dot" />
      )}
      {status === "saved" ? t("已保存") : status === "saving" ? t("保存中") : t("未保存")}
    </span>
  );
}

interface WelcomeProps {
  tree: TreeResponse | null;
  config: ConfigResponse | null;
  readOnly: boolean;
  onNew: () => void;
  onAddRoot: () => void;
  onBrowse: () => void;
  onOpen: (path: string) => void;
}

function Welcome({
  tree,
  config,
  readOnly,
  onNew,
  onAddRoot,
  onBrowse,
  onOpen,
}: WelcomeProps) {
  const { t } = useTranslation();
  const roots = tree?.roots ?? [];
  const primary = roots.find((root) => root.primary) ?? roots[0];
  const recent = [...(tree?.files ?? [])]
    .sort((a, b) => Date.parse(b.modTime) - Date.parse(a.modTime))
    .slice(0, 5);
  return (
    <div className="welcome">
      <div className="welcome-glow" />
      <div className="welcome-inner">
        <div className="welcome-mark">C</div>
        <p className="eyebrow">
          {readOnly ? "REVIEW MODE · AGENT WRITES" : "LOCAL-FIRST MARKDOWN"}
        </p>
        <h1>
          {t("让 Markdown")}
          <br />
          <span className="welcome-accent">{t("所见即所得")}</span>
        </h1>
        <p className="welcome-lead">
          {readOnly
            ? t("审阅模式：前台不编辑文档，但可以新建、导入、删除文档；评论、关联和局部回滚在这里完成。")
            : t("本地文件、实时预览、自动保存。编辑在浏览器里，内容始终留在你的电脑上。")}
        </p>

        <div className="welcome-actions">
          {roots.length === 0 ? (
            <Button
              type="primary"
              size="large"
              onClick={onAddRoot}
              icon={<IconFolderPlus size={16} />}
            >
              {t("添加目录")}
            </Button>
          ) : (
            <>
              {!readOnly ? (
                <Button
                  type="primary"
                  size="large"
                  onClick={onNew}
                  icon={<IconPlus size={16} />}
                >
                  {t("新建文档")}
                </Button>
              ) : null}
              <Button
                type={readOnly ? "primary" : "default"}
                size="large"
                onClick={onBrowse}
                icon={<IconFileText size={16} />}
              >
                {t("浏览文件")}
              </Button>
              <Button
                size="large"
                onClick={onAddRoot}
                icon={<IconFolderPlus size={16} />}
              >
                {t("添加目录")}
              </Button>
            </>
          )}
        </div>
        {readOnly ? (
          <p className="welcome-readonly-note">
            {t("审阅模式，文件由后台 Agent 维护；切换文件不会写入磁盘。")}
          </p>
        ) : null}

        {recent.length ? (
          <div className="welcome-recent">
            <span className="recent-label">{t("最近文件")}</span>
            <div className="recent-list">
              {recent.map((file) => {
                const location = [file.rootName, file.dir]
                  .filter(Boolean)
                  .join("/");
                return (
                  <button
                    key={file.path}
                    type="button"
                    className="recent-item"
                    onClick={() => onOpen(file.path)}
                    title={`${file.rootName}/${file.relPath}`}
                  >
                    <IconFileText size={14} />
                    <span>{file.name}</span>
                    {location ? <em>{location}</em> : null}
                  </button>
                );
              })}
            </div>
          </div>
        ) : (
          <div className="welcome-recent">
            <p className="welcome-empty">
              {roots.length === 0
                ? t("还没有添加目录。添加一个包含 Markdown 文件的目录开始。")
                : readOnly
                  ? t("已添加的目录还没有 Markdown 文件，可以新建或导入文档。")
                  : t("已添加的目录还没有 Markdown 文件，新建一篇开始写作。")}
            </p>
          </div>
        )}

        <div className="welcome-features">
          <span>
            <IconCheck size={13} />{" "}
            {readOnly ? t("前台只读、Agent 写入") : t("GFM 表格 / 任务列表")}
          </span>
          <span>
            <IconCheck size={13} /> {t("代码高亮")}
          </span>
          <span>
            <IconCheck size={13} /> {t("外部修改实时同步")}
          </span>
          <span>
            <IconCheck size={13} /> {t("多目录工作区")}
          </span>
        </div>

        <p className="welcome-root">
          <IconFolder size={13} />
          {roots.length ? `${roots.length} 个目录` : t("尚未添加目录")}
          {primary ? <code>{primary.path}</code> : null}
          {roots.length > 1 ? (
            <span className="version">+{roots.length - 1}</span>
          ) : null}
          {config ? (
            <span className="version">
              {config.version.startsWith("v")
                ? config.version
                : `v${config.version}`}
            </span>
          ) : null}
        </p>
      </div>

      <div className="welcome-side" aria-hidden="true">
        <div className="mock-window">
          <div className="mock-bar">
            <span />
            <span />
            <span />
          </div>
          <div className="mock-body">
            <div className="mock-lines">
              <div className="mock-line w40 accent" />
              <div className="mock-line w80" />
              <div className="mock-line w65" />
              <div className="mock-gap" />
              <div className="mock-line w75" />
              <div className="mock-line w55" />
              <div className="mock-line w70" />
              <div className="mock-gap" />
              <div className="mock-line w35 accent" />
              <div className="mock-line w60" />
            </div>
            <div className="mock-preview">
              <div className="mock-h" />
              <div className="mock-p" />
              <div className="mock-p short" />
              <div className="mock-code">
                <div />
                <div />
                <div />
              </div>
            </div>
          </div>
        </div>
      </div>

      <span className="welcome-offline">
        {typeof navigator !== "undefined" &&
        /Mac|iPhone|iPad/i.test(navigator.platform)
          ? readOnly
            ? t("⌘B 侧边栏 · 文件变化自动更新")
            : t("⌘S 保存 · ⌘B 侧边栏 · ⌘\\ 切换视图")
          : readOnly
            ? t("Ctrl+B 侧边栏 · 文件变化自动更新")
            : t("Ctrl+S 保存 · Ctrl+B 侧边栏 · Ctrl+\\ 切换视图")}
      </span>
    </div>
  );
}
