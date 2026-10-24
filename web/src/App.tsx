import {
  useCallback,
  useEffect,
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
  fetchComments,
  fetchConfig,
  fetchAgentSessions,
  createAgentSession,
  setActiveAgentSession,
  fetchDoc,
  fetchDocCodeRepos,
  fetchDocDiscussions,
  fetchTree,
  removeRoot,
  renderPreview,
  replyComment,
  groupComments,
  rollbackComment,
  resolveComment,
  triggerCommentAgent,
  saveDoc,
} from "./api";
import { Editor, type EditorDocument, type EditorHandle } from "./components/Editor";
import { Preview } from "./components/Preview";
import { CommentsPanel } from "./components/CommentsPanel";
import { CommentModal } from "./components/CommentModal";
import { CodeRepoModal } from "./components/CodeRepoModal";
import { CodeBrowserModal } from "./components/CodeBrowserModal";
import { SettingsModal } from "./components/SettingsModal";
import { DiscussionModal } from "./components/DiscussionModal";
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
  IconMessage,
  IconMoon,
  IconPanelLeft,
  IconPen,
  IconPlus,
  IconSettings,
  IconSun,
} from "./components/Icons";
import { applyTheme, currentTheme } from "./theme";
import { useWebSocket } from "./useWebSocket";
import type {
  AddRemoteRootRequest,
  AgentSession,
  Comment,
  CommentMessage,
  CodeRepo,
  ConfigResponse,
  ConsensusActivity,
  Discussion,
  DocStats,
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
  scrollToQuote,
} from "./utils";

const PREVIEW_DELAY = 80;
const SAVE_DELAY = 850;
const LAST_FILE_KEY = "mdlive:last";
const SIDEBAR_KEY = "mdlive:sidebar";
const VIEW_KEY = "mdlive:view";
const SPLIT_KEY = "mdlive:split";

const EMPTY_STATS: DocStats = { words: 0, chars: 0, lines: 1 };

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
  return error instanceof Error ? error.message : "未知错误";
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
  const [config, setConfig] = useState<ConfigResponse | null>(null);
  const [configLoaded, setConfigLoaded] = useState(false);
  const [tree, setTree] = useState<TreeResponse | null>(null);
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
  const [comments, setComments] = useState<Comment[]>([]);
  const [commentsOpen, setCommentsOpen] = useState(false);
  const [commentQuote, setCommentQuote] = useState<string | null>(null);
  const [commentsNonce, setCommentsNonce] = useState(0);
  const [codeRepos, setCodeRepos] = useState<CodeRepo[]>([]);
  const [codeRepoModalOpen, setCodeRepoModalOpen] = useState(false);
  const [codeBrowserOpen, setCodeBrowserOpen] = useState(false);
  const [codeRepoNonce, setCodeRepoNonce] = useState(0);
  const [discussions, setDiscussions] = useState<Discussion[]>([]);
  const [discussionModalOpen, setDiscussionModalOpen] = useState(false);
  const [discussionNonce, setDiscussionNonce] = useState(0);
  const [consensusActivity, setConsensusActivity] =
    useState<ConsensusActivity | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [agentSessions, setAgentSessions] = useState<AgentSession[]>([]);
  const [activeAgentSessionId, setActiveAgentSessionId] = useState("");
  const [sessionsNonce, setSessionsNonce] = useState(0);
  const [agentActivity, setAgentActivity] = useState<{
    status: "queued" | "working" | "done" | "error";
    message: string;
  } | null>(null);
  const [toasts, setToasts] = useState<ToastItem[]>([]);
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
    : "根目录";
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
      const next = await fetchTree();
      setTree(next);
      return next;
    } catch (error) {
      toast(`读取文件列表失败：${errorMessage(error)}`, "error");
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
        if (seq !== saveSeqRef.current) return;
        savedContentRef.current = content;
        setCommentsNonce((value) => value + 1);
        setMeta({ size: result.size, modTime: result.modTime });
        if (contentRef.current === content) {
          setHtml(result.html);
          setStatus("saved");
          statusRef.current = "saved";
          if (options?.manual) toast("已保存", "success");
        } else {
          setStatus("dirty");
          statusRef.current = "dirty";
        }
      } catch (error) {
        if (seq !== saveSeqRef.current) return;
        setStatus("dirty");
        statusRef.current = "dirty";
        toast(`保存失败：${errorMessage(error)}`, "error");
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
        toast("只能打开 Markdown 文件", "warn");
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
              `当前文档尚未保存，已取消切换：${errorMessage(error)}`,
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
        }, 0);
      } catch (error) {
        if (seq !== openSeqRef.current) return;
        toast(`打开失败：${errorMessage(error)}`, "error");
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
    async (body: string, discussionIds: string[] = []) => {
      const path = currentPathRef.current;
      const quote = commentQuote;
      if (!path || !quote) throw new Error("当前没有打开的文档");
      await createComment(path, quote, body, discussionIds);
      setCommentQuote(null);
      setCommentsOpen(true);
      setCommentsNonce((value) => value + 1);
      toast("评论已添加", "success");
    },
    [commentQuote, toast],
  );

  const handleResolveComment = useCallback(
    async (comment: Comment, resolved: boolean) => {
      const path = currentPathRef.current;
      if (!path) return;
      try {
        await resolveComment(path, comment.id, resolved);
        setCommentsNonce((value) => value + 1);
      } catch (error) {
        toast(`更新评论失败：${errorMessage(error)}`, "error");
      }
    },
    [toast],
  );

  const handleReplyComment = useCallback(
    async (comment: Comment, body: string, discussionIds: string[] = []) => {
      const path = currentPathRef.current;
      if (!path) throw new Error("当前没有打开的文档");
      await replyComment(path, comment.id, body, {
        author: "user",
        discussionIds,
      });
      setCommentsNonce((value) => value + 1);
      toast("已回复，等待 Code Agent 继续修改", "success");
    },
    [toast],
  );

  const handleGroupComments = useCallback(
    async (comment: Comment, memberIds: string[]) => {
      const path = currentPathRef.current;
      if (!path) throw new Error("当前没有打开的文档");
      await groupComments(path, comment.id, memberIds);
      setCommentsNonce((value) => value + 1);
      toast(
        memberIds.length > 0
          ? `已关联 ${memberIds.length + 1} 条评论，Code Agent 会一起处理`
          : "已取消关联",
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
        toast("当前没有打开的文档", "error");
        return;
      }
      const sideLabel = side === "before" ? "前" : "后";
      const confirmed = window.confirm(
        `只把这个评论对应的段落恢复为「第 ${round} 次修改${sideLabel}」的内容？\n\n其他位置的修改会保留。`,
      );
      if (!confirmed) return;
      try {
        const result = await rollbackComment(path, comment.id, message.id, side);
        if (currentPathRef.current === result.doc.path) {
          applySavedDoc(result.doc.content, result.doc.html);
          setMeta({ size: result.doc.size, modTime: result.doc.modTime });
        }
        setCommentsNonce((value) => value + 1);
        toast(
          `已局部回滚到第 ${result.round} 次修改${sideLabel}的段落，等待你的确认`,
          "success",
        );
      } catch (error) {
        toast(`回滚失败：${errorMessage(error)}`, "error");
      }
    },
    [applySavedDoc, toast],
  );

  const handleCreateAgentSession = useCallback(async () => {
    const path = currentPathRef.current;
    if (!path) throw new Error("当前没有打开的文档");
    try {
      await createAgentSession(path);
      setSessionsNonce((value) => value + 1);
      toast("已新建 Agent 会话", "success");
    } catch (error) {
      toast(`新建会话失败：${errorMessage(error)}`, "error");
      throw error;
    }
  }, [toast]);

  const handleSwitchAgentSession = useCallback(
    async (id: string) => {
      const path = currentPathRef.current;
      if (!path) throw new Error("当前没有打开的文档");
      try {
        await setActiveAgentSession(path, id);
        setActiveAgentSessionId(id);
        setSessionsNonce((value) => value + 1);
        toast("已切换 Agent 会话", "success");
      } catch (error) {
        toast(`切换会话失败：${errorMessage(error)}`, "error");
        throw error;
      }
    },
    [toast],
  );

  const handleSettingsSaved = useCallback(async () => {
    setSettingsOpen(false);
    try {
      const next = await fetchConfig();
      setConfig(next);
    } catch {
      // Keep the last known config; the next page load will refresh it.
    }
    toast("设置已保存", "success");
  }, [toast]);

  const handleCodeRepoChanged = useCallback(
    (message: string) => {
      setCodeRepoNonce((value) => value + 1);
      toast(message, "success");
    },
    [toast],
  );

  const handleTriggerCommentAgent = useCallback(
    async (comment: Comment) => {
      const path = currentPathRef.current;
      if (!path) throw new Error("当前没有打开的文档");
      try {
        await triggerCommentAgent(path, comment.id);
        setCommentsNonce((value) => value + 1);
        toast("已通知后台 Agent 处理该评论", "success");
      } catch (error) {
        toast(`启动 Agent 失败：${errorMessage(error)}`, "error");
        throw error;
      }
    },
    [toast],
  );

  const handleDeleteComment = useCallback(
    async (comment: Comment) => {
      const path = currentPathRef.current;
      if (!path) return;
      if (!window.confirm("删除这条评论？")) return;
      try {
        await deleteComment(path, comment.id);
        setCommentsNonce((value) => value + 1);
        toast("评论已删除", "success");
      } catch (error) {
        toast(`删除评论失败：${errorMessage(error)}`, "error");
      }
    },
    [toast],
  );

  const handleJumpToComment = useCallback(
    (comment: Comment) => {
      if (viewMode === "editor") setViewMode("split");
      window.setTimeout(() => {
        if (!scrollToQuote(previewRef.current, comment.anchor || comment.quote)) {
          toast("未能在预览中定位这段文字", "warn");
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
        toast("文件已删除", "success");
        if (removingCurrent) {
          clearCurrent();
          initialPickRef.current = false;
        }
      } catch (error) {
        initialPickRef.current = false;
        toast(`删除失败：${errorMessage(error)}`, "error");
      }
    },
    [clearCurrent, refreshTree, toast],
  );

  const requestNewDoc = useCallback(() => {
    if (!roots.length) {
      toast("请先添加一个目录", "info");
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
      toast(`已添加目录 “${result.root.name}”`, "success");
      if (result.warning) toast(result.warning, "warn");
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
        `已连接远程服务器 “${result.root.remote ?? result.root.name}”`,
        "success",
      );
      if (result.warning) toast(result.warning, "warn");
    },
    [refreshTree, toast],
  );

  const handleRemoveRoot = useCallback(
    async (root: RootInfo) => {
      if (removingRoot) return;
      if (
        !window.confirm(
          `从工作区移除目录 “${root.name}”？\n不会删除磁盘上的文件。`,
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
              `当前文档保存失败，已取消移除目录：${errorMessage(error)}`,
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
        toast(`移除目录失败：${errorMessage(error)}`, "error");
      } finally {
        // Keep the spinner visible long enough to be perceivable even when the
        // server responds immediately.
        const remaining = 420 - (Date.now() - started);
        if (remaining > 0) {
          await new Promise((resolve) => window.setTimeout(resolve, remaining));
        }
        setRemovingRoot(null);
        if (removed) {
          toast(`已移除目录 “${root.name}”`, "success");
        }
      }
    },
    [clearCurrent, refreshTree, removingRoot, toast],
  );

  const handleCreate = useCallback(
    async (path: string) => {
      const result = await saveDoc(path, "", true);
      setNewModal(false);
      await refreshTree();
      await openDoc(result.path);
      toast("文档已创建", "success");
    },
    [openDoc, refreshTree, toast],
  );

  const handleImport = useCallback(
    async (files: FileList) => {
      const candidates = Array.from(files).filter((file) =>
        isMarkdownPath(file.name),
      );
      if (!candidates.length) {
        toast("请选择 Markdown 文件", "warn");
        return;
      }
      const targetRoot = currentFile
        ? roots.find((root) => root.id === currentFile.rootId)
        : primaryRoot;
      if (!targetRoot) {
        toast("请先添加一个目录", "warn");
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
              errorMessage(error).includes("已存在") &&
              attempt < 50
            ) {
              attempt += 1;
              target = `${targetRoot.id}/${suffixedName(base, attempt)}`;
              continue;
            }
            toast(`导入 ${file.name} 失败：${errorMessage(error)}`, "error");
            break;
          }
        }
      }
      await refreshTree();
      if (firstPath) await openDoc(firstPath);
      if (imported > 0) {
        toast(`已导入 ${imported} 个文件`, "success");
      }
    },
    [currentFile, openDoc, primaryRoot, refreshTree, roots, toast],
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
      document.body.classList.remove("dragging-split");
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
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
              "另一个窗口保存了不同内容；本地改动会在自动保存后覆盖",
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
        if (message.origin === "rollback") {
          if (incomingContent !== contentRef.current && dirty) {
            toast("评论段落已局部回滚，未保存的本地改动已被覆盖", "warn");
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
            toast("文件已被外部修改，已载入磁盘上的新内容", "warn");
          }
          applySavedDoc(incomingContent, incomingHtml);
          break;
        }
        // origin === "remote": another mdlive window is typing.
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
        break;
      case "comments":
        if (message.path === currentPathRef.current) {
          setCommentsNonce((value) => value + 1);
        }
        break;
      case "discussions":
        if (message.path === currentPathRef.current) {
          setDiscussionNonce((value) => value + 1);
        }
        break;
      case "consensus":
        if (message.path === currentPathRef.current) {
          if (message.status) {
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
            setDiscussionNonce((value) => value + 1);
          }
        }
        break;
      case "agent":
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
            message: message.message || "后台 Agent 正在处理…",
          });
          if (status === "queued" || status === "done" || status === "error") {
            toast(
              message.message || "后台 Agent 状态更新",
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
          toast("文件已被删除或移动", "warn");
          initialPickRef.current = true;
          clearCurrent();
          void refreshTree().finally(() => {
            initialPickRef.current = false;
          });
        }
        break;
      case "error":
        toast(message.message, "error");
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
    let alive = true;
    fetchConfig()
      .then((next) => {
        if (alive) setConfig(next);
      })
      .catch((error) => {
        if (alive) toast(`无法读取配置：${errorMessage(error)}`, "error");
      })
      .finally(() => {
        if (alive) setConfigLoaded(true);
      });
    void refreshTree();
    return () => {
      alive = false;
    };
  }, [refreshTree, toast]);

  useEffect(() => {
    if (config?.debug && !config.hotReload) {
      toast(
        "调试模式未找到 web/dist，前端热更新不可用；已回退到内嵌资源",
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
    toast("当前文件已不在工作区中，正在切换", "warn");
    clearCurrent();
  }, [tree, currentPath, clearCurrent, toast]);

  useEffect(() => {
    if (!currentPath) {
      setComments([]);
      return;
    }
    let alive = true;
    fetchComments(currentPath)
      .then((result) => {
        if (alive && currentPathRef.current === currentPath) {
          setComments(result.comments ?? []);
        }
      })
      .catch(() => {
        // Comments are optional; do not interrupt preview on failure.
      });
    return () => {
      alive = false;
    };
  }, [currentPath, doc.version, commentsNonce]);

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
    if (!currentPath) {
      setCodeRepos([]);
      return;
    }
    let alive = true;
    fetchDocCodeRepos(currentPath)
      .then((result) => {
        if (alive && currentPathRef.current === currentPath) {
          setCodeRepos(result.repos ?? []);
        }
      })
      .catch(() => {
        // Code repositories are optional metadata.
      });
    return () => {
      alive = false;
    };
  }, [currentPath, codeRepoNonce]);

  useEffect(() => {
    if (!currentPath) {
      setDiscussions([]);
      return;
    }
    let alive = true;
    fetchDocDiscussions(currentPath)
      .then((result) => {
        if (alive && currentPathRef.current === currentPath) {
          setDiscussions(result.discussions ?? []);
        }
      })
      .catch(() => {
        // Discussion context is optional metadata.
      });
    return () => {
      alive = false;
    };
  }, [currentPath, discussionNonce]);

  useEffect(() => {
    if (!currentPath) {
      setAgentSessions([]);
      setActiveAgentSessionId("");
      return;
    }
    let alive = true;
    fetchAgentSessions(currentPath)
      .then((result) => {
        if (!alive || currentPathRef.current !== currentPath) return;
        setAgentSessions(result.sessions ?? []);
        setActiveAgentSessionId(result.activeId ?? "");
      })
      .catch(() => {
        // Agent sessions are optional metadata for the review surface.
      });
    return () => {
      alive = false;
    };
  }, [currentPath, sessionsNonce]);

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
        if (!previewOnly) requestNewDoc();
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
      ? `${baseName(currentPath)} · mdlive`
      : "mdlive · Markdown 实时预览";
  }, [currentPath]);

  useEffect(() => {
    if (previewOnly || viewMode === "preview") return;
    const timer = window.setTimeout(() => editorRef.current?.focus(), 60);
    return () => window.clearTimeout(timer);
  }, [previewOnly, viewMode]);

  if (!configLoaded) {
    return (
      <div className="boot-screen">
        <div className="boot-mark">M</div>
        <div className="boot-title">mdlive</div>
        <span className="spinner" />
      </div>
    );
  }

  const connectionLabel =
    connection === "open"
      ? "实时同步"
      : connection === "connecting"
        ? "连接中…"
        : "重新连接中…";

  const dirty = status !== "saved";

  return (
    <div className={`app${sidebarOpen ? " sidebar-visible" : ""}`}>
      <header className="topbar">
        <div className="topbar-left">
          <button
            type="button"
            className="icon-btn"
            onClick={() => setSidebarOpen((value) => !value)}
            title={sidebarOpen ? "隐藏侧边栏 (⌘B)" : "显示侧边栏 (⌘B)"}
            aria-label="切换侧边栏"
            aria-pressed={sidebarOpen}
          >
            <IconPanelLeft size={17} />
          </button>
          <div className="brand">
            <span className="brand-mark">M</span>
            <span className="brand-name">mdlive</span>
          </div>
          <span className="brand-tag">Markdown live preview</span>
          {debugMode ? (
            <span
              className="mode-pill debug"
              title={
                config?.hotReload
                  ? "调试模式：前端资源变化后自动刷新"
                  : "调试模式：热更新目录不可用"
              }
            >
              DEBUG
            </span>
          ) : null}
          {previewOnly ? (
            <span
              className="mode-pill readonly"
              title="审阅模式：前台只读；后台 Agent 可写入，评论与局部回滚可用"
            >
              审阅模式
            </span>
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
                <AgentSessionPicker
                  sessions={agentSessions}
                  activeId={activeAgentSessionId}
                  onSwitch={handleSwitchAgentSession}
                  onCreate={handleCreateAgentSession}
                />
              </span>
            </>
          ) : (
            <span className="doc-name empty">未打开文档</span>
          )}
        </div>

        <div className="topbar-actions">
          {!previewOnly && currentPath ? (
            <SaveIndicator status={status} />
          ) : null}
          {!previewOnly ? (
            <div className="segmented" role="group" aria-label="视图模式">
              {VIEW_OPTIONS.map((option) => (
                <button
                  key={option.value}
                  type="button"
                  className={viewMode === option.value ? "active" : ""}
                  title={option.label}
                  aria-label={option.label}
                  aria-pressed={viewMode === option.value}
                  onClick={() => setViewMode(option.value)}
                >
                  {option.icon}
                </button>
              ))}
            </div>
          ) : null}
          <button
            type="button"
            className="icon-btn"
            onClick={() => setSettingsOpen(true)}
            title="设置"
            aria-label="设置"
          >
            <IconSettings size={17} />
          </button>
          <button
            type="button"
            className="icon-btn"
            onClick={() =>
              setTheme((value) => (value === "dark" ? "light" : "dark"))
            }
            title={theme === "dark" ? "切换到浅色" : "切换到深色"}
            aria-label="切换主题"
          >
            {theme === "dark" ? <IconSun size={17} /> : <IconMoon size={17} />}
          </button>
          {!previewOnly ? (
            <button
              type="button"
              className="btn primary new-btn"
              onClick={requestNewDoc}
              title="新建文档 (⌘⇧N)"
            >
              <IconPlus size={15} />
              <span>新建</span>
            </button>
          ) : null}
        </div>
      </header>

      <div className="app-body">
        <button
          type="button"
          className={`sidebar-backdrop${sidebarOpen ? " show" : ""}`}
          onClick={() => setSidebarOpen(false)}
          aria-label="关闭侧边栏"
        />
        <Sidebar
          open={sidebarOpen}
          tree={tree}
          currentPath={currentPath}
          dirty={dirty}
          readOnly={previewOnly}
          removingRootId={removingRoot?.id ?? null}
          removingRootName={removingRoot?.name ?? ""}
          onOpen={openFromSidebar}
          onDelete={(path) => void handleDelete(path)}
          onNew={requestNewDoc}
          onImport={(files) => void handleImport(files)}
          onAddRoot={() => setAddRootOpen(true)}
          onRemoveRoot={(root) => void handleRemoveRoot(root)}
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
                      编辑
                    </span>
                    <span className="pane-meta">自动保存</span>
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
                  title="拖动调整分栏，双击恢复 50/50"
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
                    预览
                  </span>
                  {previewOnly ? (
                    <span className="pane-meta">前台只读 · Agent 修改自动更新</span>
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
                      <button
                        type="button"
                        className="comments-toggle code-repos-toggle"
                        onClick={() => {
                          if (codeRepos.length > 0) {
                            setCodeBrowserOpen(true);
                          } else {
                            setCodeRepoModalOpen(true);
                          }
                        }}
                        title={
                          codeRepos.length
                            ? `已关联 ${codeRepos.length} 个代码仓库`
                            : "关联代码仓库"
                        }
                      >
                        <IconGitBranch size={13} />
                        代码
                        {codeRepos.length ? (
                          <span className="comments-badge">
                            {codeRepos.length}
                          </span>
                        ) : null}
                      </button>
                    ) : null}
                    {currentPath ? (
                      <button
                        type="button"
                        className="comments-toggle discussion-toggle"
                        onClick={() => setDiscussionModalOpen(true)}
                        title="导入讨论并维护文档共识"
                      >
                        <IconMessage size={13} />
                        讨论
                        {consensusActivity &&
                        consensusActivity.status !== "done" &&
                        consensusActivity.status !== "error" ? (
                          <span className="spinner" />
                        ) : discussions.length ? (
                          <span className="comments-badge">
                            {discussions.length}
                          </span>
                        ) : null}
                      </button>
                    ) : null}
                    <button
                      type="button"
                      className={`comments-toggle${commentsOpen ? " active" : ""}`}
                      onClick={() => setCommentsOpen((value) => !value)}
                      title="查看 / 添加评论"
                      aria-pressed={commentsOpen}
                    >
                      <IconMessage size={13} />
                      评论
                      {comments.length ? (
                        <span className="comments-badge">{comments.length}</span>
                      ) : null}
                    </button>
                    <span
                      className={`live-badge${connection === "open" ? " on" : ""}`}
                    >
                      <i />
                      {connection === "open" ? "实时" : "离线渲染"}
                    </span>
                  </div>
                </div>
                <Preview
                  key={currentPath}
                  html={html}
                  currentPath={currentPath}
                  comments={comments}
                  containerRef={previewRef}
                  onScrollRatio={handlePreviewScroll}
                  onOpenDoc={(path) => void openDoc(path)}
                  onRequestComment={(quote) => setCommentQuote(quote)}
                />
                {commentsOpen ? (
                  <CommentsPanel
                    comments={comments}
                    onClose={() => setCommentsOpen(false)}
                    onJump={handleJumpToComment}
                    onResolve={(comment, resolved) =>
                      void handleResolveComment(comment, resolved)
                    }
                    onDelete={(comment) => void handleDeleteComment(comment)}
                    agentName={config?.agentName || "Code Agent"}
                    discussions={discussions}
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
                <span className="foot-readonly">· 审阅模式</span>
              ) : status === "saved" ? (
                <span className="foot-sep">·</span>
              ) : status === "saving" ? (
                <span className="foot-sep">· 保存中…</span>
              ) : (
                <span className="foot-unsaved">· 未保存</span>
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
              <span>{stats.words} 字</span>
              <span>{stats.chars} 字符</span>
              <span>{stats.lines} 行</span>
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
                ? `${roots.length} 个目录`
                : (config?.root ?? "")}
            </span>
          )}
        </div>
      </footer>

      {newModal && !previewOnly ? (
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
          onClose={() => setCommentQuote(null)}
          onSubmit={handleCreateComment}
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
          refreshKey={discussionNonce}
          activity={
            consensusActivity?.path === currentPath
              ? consensusActivity
              : undefined
          }
          onClose={() => setDiscussionModalOpen(false)}
          onChanged={() => setDiscussionNonce((value) => value + 1)}
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
            setCodeRepoNonce((value) => value + 1);
            toast("已切换分支", "success");
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
  );
}

function SaveIndicator({ status }: { status: SaveStatus }) {
  return (
    <span className={`save-state ${status}`} title="自动保存已开启">
      {status === "saved" ? (
        <IconCheck size={13} />
      ) : status === "saving" ? (
        <span className="spinner" />
      ) : (
        <span className="unsaved-dot" />
      )}
      {status === "saved" ? "已保存" : status === "saving" ? "保存中" : "未保存"}
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
  const roots = tree?.roots ?? [];
  const primary = roots.find((root) => root.primary) ?? roots[0];
  const recent = [...(tree?.files ?? [])]
    .sort((a, b) => Date.parse(b.modTime) - Date.parse(a.modTime))
    .slice(0, 5);
  return (
    <div className="welcome">
      <div className="welcome-glow" />
      <div className="welcome-inner">
        <div className="welcome-mark">M</div>
        <p className="eyebrow">
          {readOnly ? "REVIEW MODE · AGENT WRITES" : "LOCAL-FIRST MARKDOWN"}
        </p>
        <h1>
          让 Markdown
          <br />
          <span className="welcome-accent">所见即所得</span>
        </h1>
        <p className="welcome-lead">
          {readOnly
            ? "审阅模式：前台只读，后台 Agent 负责写入；评论、关联和局部回滚在这里完成。"
            : "本地文件、实时预览、自动保存。编辑在浏览器里，内容始终留在你的电脑上。"}
        </p>

        <div className="welcome-actions">
          {roots.length === 0 ? (
            <button
              type="button"
              className="btn primary large"
              onClick={onAddRoot}
            >
              <IconFolderPlus size={16} />
              添加目录
            </button>
          ) : (
            <>
              {!readOnly ? (
                <button
                  type="button"
                  className="btn primary large"
                  onClick={onNew}
                >
                  <IconPlus size={16} />
                  新建文档
                </button>
              ) : null}
              <button
                type="button"
                className={`btn ${readOnly ? "primary" : "ghost"} large`}
                onClick={onBrowse}
              >
                <IconFileText size={16} />
                浏览文件
              </button>
              <button
                type="button"
                className="btn ghost large"
                onClick={onAddRoot}
              >
                <IconFolderPlus size={16} />
                添加目录
              </button>
            </>
          )}
        </div>
        {readOnly ? (
          <p className="welcome-readonly-note">
            审阅模式，文件由后台 Agent 维护；切换文件不会写入磁盘。
          </p>
        ) : null}

        {recent.length ? (
          <div className="welcome-recent">
            <span className="recent-label">最近文件</span>
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
                ? "还没有添加目录。添加一个包含 Markdown 文件的目录开始。"
                : readOnly
                  ? "已添加的目录还没有 Markdown 文件。后台 Agent 创建后，这里会自动出现。"
                  : "已添加的目录还没有 Markdown 文件，新建一篇开始写作。"}
            </p>
          </div>
        )}

        <div className="welcome-features">
          <span>
            <IconCheck size={13} />{" "}
            {readOnly ? "前台只读、Agent 写入" : "GFM 表格 / 任务列表"}
          </span>
          <span>
            <IconCheck size={13} /> 代码高亮
          </span>
          <span>
            <IconCheck size={13} /> 外部修改实时同步
          </span>
          <span>
            <IconCheck size={13} /> 多目录工作区
          </span>
        </div>

        <p className="welcome-root">
          <IconFolder size={13} />
          {roots.length ? `${roots.length} 个目录` : "尚未添加目录"}
          {primary ? <code>{primary.path}</code> : null}
          {roots.length > 1 ? (
            <span className="version">+{roots.length - 1}</span>
          ) : null}
          {config ? <span className="version">v{config.version}</span> : null}
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
            ? "⌘B 侧边栏 · 文件变化自动更新"
            : "⌘S 保存 · ⌘B 侧边栏 · ⌘\\ 切换视图"
          : readOnly
            ? "Ctrl+B 侧边栏 · 文件变化自动更新"
            : "Ctrl+S 保存 · Ctrl+B 侧边栏 · Ctrl+\\ 切换视图"}
      </span>
    </div>
  );
}
