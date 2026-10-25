import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { useTranslation } from "react-i18next";
import {
  browseDirectories,
  browseRemoteDirectories,
  closeRemoteSession,
  createRemoteSession,
} from "../api";
import type {
  AddRemoteRootRequest,
  BrowseEntry,
  BrowseResponse,
  RootInfo,
} from "../types";
import {
  IconArrowUp,
  IconChevronRight,
  IconFolder,
  IconFolderPlus,
  IconServer,
  IconX,
} from "./Icons";

interface AddRootModalProps {
  roots: RootInfo[];
  initialPath: string;
  onClose: () => void;
  onAddLocal: (path: string) => Promise<void>;
  onAddRemote: (config: AddRemoteRootRequest) => Promise<void>;
}

export function AddRootModal({
  roots,
  initialPath,
  onClose,
  onAddLocal,
  onAddRemote,
}: AddRootModalProps) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"local" | "remote">("local");
  const [data, setData] = useState<BrowseResponse | null>(null);
  const [pathInput, setPathInput] = useState(initialPath);
  const [showHidden, setShowHidden] = useState(false);
  const [loading, setLoading] = useState(true);
  const [localBusy, setLocalBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const seq = useRef(0);
  const pathRef = useRef<HTMLInputElement>(null);
  const hostRef = useRef<HTMLInputElement>(null);

  const [remoteName, setRemoteName] = useState("");
  const [remoteHost, setRemoteHost] = useState("");
  const [remotePort, setRemotePort] = useState("22");
  const [remoteUser, setRemoteUser] = useState("");
  const [remoteKeyPath, setRemoteKeyPath] = useState("");
  const [remotePath, setRemotePath] = useState("");
  const [remoteBusy, setRemoteBusy] = useState(false);
  const [remoteError, setRemoteError] = useState<string | null>(null);
  const [remoteBrowsing, setRemoteBrowsing] = useState(false);
  const [remoteData, setRemoteData] = useState<BrowseResponse | null>(null);
  const [remoteBrowseInput, setRemoteBrowseInput] = useState("");
  const [remoteLoading, setRemoteLoading] = useState(false);
  const [remoteBrowseError, setRemoteBrowseError] = useState<string | null>(null);
  const [remoteShowHidden, setRemoteShowHidden] = useState(false);
  const [remoteConnecting, setRemoteConnecting] = useState(false);
  const [remoteAdding, setRemoteAdding] = useState(false);
  const sessionRef = useRef<string | null>(null);
  const remoteSeq = useRef(0);
  const connectSeq = useRef(0);
  const connectAbort = useRef<AbortController | null>(null);
  const mountedRef = useRef(true);

  const load = useCallback(async (path: string, hidden: boolean) => {
    const id = ++seq.current;
    setLoading(true);
    setError(null);
    try {
      const next = await browseDirectories(path, hidden);
      if (id !== seq.current) return;
      setData(next);
      setPathInput(next.path);
    } catch (err) {
      if (id !== seq.current) return;
      setError(err instanceof Error ? err.message : t("无法读取目录"));
    } finally {
      if (id === seq.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(initialPath, false);
    pathRef.current?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [initialPath, load, onClose]);

  const endRemoteSession = useCallback(() => {
    const id = sessionRef.current;
    sessionRef.current = null;
    if (id) {
      void closeRemoteSession(id).catch(() => {});
    }
  }, []);

  useEffect(() => {
    return () => {
      mountedRef.current = false;
      connectAbort.current?.abort();
      connectAbort.current = null;
      const id = sessionRef.current;
      sessionRef.current = null;
      if (id) {
        void closeRemoteSession(id).catch(() => {});
      }
    };
  }, []);

  const currentPath = data?.path ?? "";
  const alreadyAdded = roots.some(
    (root) => root.kind === "local" && root.path === currentPath,
  );

  const navigate = (path: string) => {
    if (!path) return;
    void load(path, showHidden);
  };

  const submitPath = (event: FormEvent) => {
    event.preventDefault();
    if (pathInput.trim()) void load(pathInput, showHidden);
  };

  const toggleHidden = (checked: boolean) => {
    setShowHidden(checked);
    void load(currentPath || pathInput, checked);
  };

  const addLocal = async () => {
    if (!currentPath || alreadyAdded) return;
    setLocalBusy(true);
    setError(null);
    try {
      await onAddLocal(currentPath);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("添加目录失败"));
      setLocalBusy(false);
    }
  };

  const switchTab = (next: "local" | "remote") => {
    if (remoteBrowsing && next !== "remote") {
      backToRemoteForm();
    }
    setTab(next);
    if (next === "remote") {
      setTimeout(() => hostRef.current?.focus(), 0);
    }
  };

  const loadRemote = async (path: string, hidden: boolean) => {
    const session = sessionRef.current;
    if (!session) return;
    const id = ++remoteSeq.current;
    setRemoteLoading(true);
    setRemoteBrowseError(null);
    try {
      const next = await browseRemoteDirectories(session, path, hidden);
      if (id !== remoteSeq.current) return;
      setRemoteData(next);
      setRemoteBrowseInput(next.path);
    } catch (err) {
      if (id !== remoteSeq.current) return;
      setRemoteBrowseError(
        err instanceof Error ? err.message : t("无法读取远程目录"),
      );
    } finally {
      if (id === remoteSeq.current) setRemoteLoading(false);
    }
  };

  const startRemoteBrowse = async () => {
    setRemoteError(null);
    const port = Number(remotePort);
    if (!remoteHost.trim() || !remoteUser.trim()) {
      setRemoteError(t("主机和用户名不能为空"));
      return;
    }
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      setRemoteError(t("端口必须是 1–65535 之间的整数"));
      return;
    }
    const seq = ++connectSeq.current;
    const controller = new AbortController();
    connectAbort.current?.abort();
    connectAbort.current = controller;
    setRemoteConnecting(true);
    setRemoteBusy(true);
    endRemoteSession();
    try {
      const session = await createRemoteSession(
        {
          host: remoteHost.trim(),
          port,
          user: remoteUser.trim(),
          keyPath: remoteKeyPath.trim(),
        },
        controller.signal,
      );
      if (
        controller.signal.aborted ||
        seq !== connectSeq.current ||
        !mountedRef.current
      ) {
        void closeRemoteSession(session.session).catch(() => {});
        return;
      }
      sessionRef.current = session.session;
      setRemoteBrowsing(true);
      setRemoteData(null);
      setRemoteBrowseInput(remotePath.trim() || session.home);
      await loadRemote(remotePath.trim(), remoteShowHidden);
    } catch (err) {
      if (controller.signal.aborted || seq !== connectSeq.current) return;
      setRemoteError(
        err instanceof Error ? err.message : t("连接远程服务器失败"),
      );
      endRemoteSession();
      setRemoteBrowsing(false);
    } finally {
      if (seq === connectSeq.current) {
        setRemoteConnecting(false);
        setRemoteBusy(false);
        connectAbort.current = null;
      }
    }
  };

  const cancelRemoteConnect = () => {
    connectSeq.current += 1;
    remoteSeq.current += 1;
    connectAbort.current?.abort();
    connectAbort.current = null;
    setRemoteConnecting(false);
    setRemoteBusy(false);
    endRemoteSession();
    setRemoteBrowseError(null);
  };


  const addFromRemoteBrowser = async () => {
    if (!remoteData?.path) return;
    const port = Number(remotePort);
    setRemoteBusy(true);
    setRemoteAdding(true);
    setRemoteBrowseError(null);
    try {
      await onAddRemote({
        name: remoteName.trim(),
        host: remoteHost.trim(),
        port,
        user: remoteUser.trim(),
        keyPath: remoteKeyPath.trim(),
        path: remoteData.path,
      });
    } catch (err) {
      setRemoteBrowseError(
        err instanceof Error ? err.message : t("添加远程目录失败"),
      );
      setRemoteBusy(false);
      setRemoteAdding(false);
    }
  };

  const backToRemoteForm = () => {
    remoteSeq.current += 1;
    endRemoteSession();
    setRemoteBrowsing(false);
    setRemoteData(null);
    setRemoteLoading(false);
    setRemoteBrowseError(null);
  };

  const submitRemoteBrowsePath = (event: FormEvent) => {
    event.preventDefault();
    if (remoteBrowseInput.trim()) {
      void loadRemote(remoteBrowseInput, remoteShowHidden);
    }
  };

  const toggleRemoteHidden = (checked: boolean) => {
    setRemoteShowHidden(checked);
    void loadRemote(remoteData?.path ?? remoteBrowseInput, checked);
  };



  const renderEntry = (entry: BrowseEntry) => (
    <button
      key={entry.path}
      type="button"
      className="browser-row"
      title={entry.path}
      onClick={() => navigate(entry.path)}
    >
      <span className="browser-row-icon">
        <IconFolder size={15} />
      </span>
      <span className="browser-row-name">{entry.name}</span>
      <IconChevronRight size={13} />
    </button>
  );

  const renderRemoteEntry = (entry: BrowseEntry) => (
    <button
      key={entry.path}
      type="button"
      className="browser-row"
      title={entry.path}
      onClick={() => void loadRemote(entry.path, remoteShowHidden)}
    >
      <span className="browser-row-icon">
        <IconFolder size={15} />
      </span>
      <span className="browser-row-name">{entry.name}</span>
      <IconChevronRight size={13} />
    </button>
  );

  return (
    <div
      className="modal-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        className="modal browser-modal"
        role="dialog"
        aria-modal="true"
        aria-label={t("添加目录或远程服务器")}
      >
        <div className="modal-head">
          <div className="modal-title">
            {tab === "local" ? (
              <IconFolderPlus size={17} />
            ) : (
              <IconServer size={17} />
            )}
            {tab === "local"
              ? t("添加本地目录")
              : remoteBrowsing
                ? t("浏览远程目录")
                : t("添加远程服务器")}
          </div>
          <button
            type="button"
            className="icon-btn small"
            onClick={onClose}
            aria-label={t("关闭")}
          >
            <IconX size={15} />
          </button>
        </div>

        <div className="root-tabs" role="tablist" aria-label={t("目录类型")}>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "local"}
            className={tab === "local" ? "active" : ""}
            onClick={() => switchTab("local")}
            disabled={remoteBusy}
          >
            <IconFolder size={14} />
            {t("本地目录")}
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "remote"}
            className={tab === "remote" ? "active" : ""}
            onClick={() => switchTab("remote")}
            disabled={remoteBusy}
          >
            <IconServer size={14} />
            {t("远程服务器")}
          </button>
        </div>

        {tab === "local" ? (
          <>
            <form className="browser-path-form" onSubmit={submitPath}>
              <button
                type="button"
                className="browser-up"
                onClick={() => navigate(data?.parent ?? "")}
                disabled={!data?.parent}
                title={t("上一级")}
                aria-label={t("上一级")}
              >
                <IconArrowUp size={15} />
              </button>
              <div className="browser-path-wrap">
                <IconFolder size={14} />
                <input
                  ref={pathRef}
                  value={pathInput}
                  onChange={(event) => setPathInput(event.target.value)}
                  placeholder={t("~/Documents/notes 或 /Volumes/notes")}
                  spellCheck={false}
                  autoComplete="off"
                  aria-label={t("当前目录")}
                />
              </div>
              <button type="submit" className="btn ghost">
                {t("转到")}
              </button>
            </form>

            {data?.shortcuts.length ? (
              <div className="browser-shortcuts">
                {data.shortcuts.map((shortcut) => (
                  <button
                    key={shortcut.path}
                    type="button"
                    className={`browser-shortcut${
                      shortcut.path === currentPath ? " active" : ""
                    }`}
                    title={shortcut.path}
                    onClick={() => navigate(shortcut.path)}
                  >
                    <IconFolder size={12} />
                    {t(shortcut.name)}
                  </button>
                ))}
              </div>
            ) : null}

            <div className="browser-list" aria-live="polite">
              {loading ? (
                <div className="browser-loading">
                  <span className="spinner" />
                  {t("读取目录…")}
                </div>
              ) : data?.entries.length ? (
                data.entries.map(renderEntry)
              ) : (
                <div className="browser-empty">
                  {error ? t("无法读取此目录") : t("这里没有子目录")}
                </div>
              )}
            </div>

            <div className="browser-list-foot">
              <label className="browser-hidden-toggle">
                <input
                  type="checkbox"
                  checked={showHidden}
                  onChange={(event) => toggleHidden(event.target.checked)}
                />
                {t("显示隐藏目录")}
              </label>
              <span>
                {t("{{count}} 个子目录", { count: data?.entries.length ?? 0 })}
                {data?.truncated ? t("（已截断）") : ""}
              </span>
            </div>

            <p className="modal-hint browser-hint">
              {t("选择目录后，Commentloop 会递归读取其中的 Markdown 并监听外部修改。")}
              {t("当前工作区已有 {{count}} 个目录", { count: roots.length })}
            </p>
            {error ? <p className="modal-error">{error}</p> : null}

            <div className="browser-selection">
              <span>{t("当前目录")}</span>
              <code title={currentPath}>{currentPath || t("选择中…")}</code>
              {alreadyAdded ? (
                <span className="browser-added">{t("已添加")}</span>
              ) : null}
            </div>

            <div className="modal-actions">
              <button type="button" className="btn ghost" onClick={onClose}>
                {t("取消")}
              </button>
              <button
                type="button"
                className="btn primary"
                onClick={() => void addLocal()}
                disabled={!currentPath || loading || localBusy || alreadyAdded}
              >
                {localBusy ? t("添加中…") : t("添加此目录")}
              </button>
            </div>
          </>
        ) : remoteConnecting ? (
          <div className="remote-connecting" role="status" aria-live="polite">
            <span className="spinner remote-spinner" />
            <h3>{t("正在连接远程服务器…")}</h3>
            <code>
              {remoteUser || "user"}@{remoteHost || "host"}:{remotePort || "22"}
            </code>
            <p>{t("首次连接需要验证 SSH 密钥和主机指纹，可能需要几秒。")}</p>
            <div className="remote-connecting-actions">
              <button type="button" className="btn primary" disabled>
                {t("连接中…")}
              </button>
              <button
                type="button"
                className="btn ghost"
                onClick={cancelRemoteConnect}
              >
                {t("关闭")}
              </button>
            </div>
          </div>
        ) : remoteAdding ? (
          <div className="remote-connecting" role="status" aria-live="polite">
            <span className="spinner remote-spinner" />
            <h3>{t("正在读取远程文件…")}</h3>
            <code>
              {remoteUser || "user"}@{remoteHost || "host"}:{remotePort || "22"}
            </code>
            <p>
              {t("连接已建立，正在索引 Markdown 文件。目录较大时可能需要几秒，完成后文件会立即出现在侧栏。")}
            </p>
            <div className="remote-connecting-actions">
              <button type="button" className="btn ghost" onClick={onClose}>
                {t("关闭")}
              </button>
            </div>
          </div>
        ) : remoteBrowsing ? (
          <>
            <form className="browser-path-form" onSubmit={submitRemoteBrowsePath}>
              <button
                type="button"
                className="browser-up"
                onClick={() => {
                  if (remoteData?.parent) {
                    void loadRemote(remoteData.parent, remoteShowHidden);
                  }
                }}
                disabled={!remoteData?.parent}
                title={t("上一级")}
                aria-label={t("上一级")}
              >
                <IconArrowUp size={15} />
              </button>
              <div className="browser-path-wrap">
                <IconServer size={14} />
                <input
                  value={remoteBrowseInput}
                  onChange={(event) => setRemoteBrowseInput(event.target.value)}
                  placeholder={t("远程目录路径")}
                  spellCheck={false}
                  autoComplete="off"
                  aria-label={t("远程目录")}
                />
              </div>
              <button type="submit" className="btn ghost">
                {t("转到")}
              </button>
            </form>

            {remoteData?.shortcuts.length ? (
              <div className="browser-shortcuts">
                {remoteData.shortcuts.map((shortcut) => (
                  <button
                    key={shortcut.path}
                    type="button"
                    className={`browser-shortcut${
                      shortcut.path === remoteData.path ? " active" : ""
                    }`}
                    title={shortcut.path}
                    onClick={() =>
                      void loadRemote(shortcut.path, remoteShowHidden)
                    }
                  >
                    <IconFolder size={12} />
                    {t(shortcut.name)}
                  </button>
                ))}
              </div>
            ) : null}

            <div className="browser-list" aria-live="polite">
              {remoteLoading ? (
                <div className="browser-loading">
                  <span className="spinner" />
                  {t("读取远程目录…")}
                </div>
              ) : remoteData?.entries.length ? (
                remoteData.entries.map(renderRemoteEntry)
              ) : (
                <div className="browser-empty">
                  {remoteBrowseError ? t("无法读取此目录") : t("这里没有子目录")}
                </div>
              )}
            </div>

            <div className="browser-list-foot">
              <label className="browser-hidden-toggle">
                <input
                  type="checkbox"
                  checked={remoteShowHidden}
                  onChange={(event) => toggleRemoteHidden(event.target.checked)}
                />
                {t("显示隐藏目录")}
              </label>
              <span>
                {t("{{count}} 个子目录", {
                  count: remoteData?.entries.length ?? 0,
                })}
                {remoteData?.truncated ? t("（已截断）") : ""}
              </span>
            </div>

            <p className="modal-hint remote-hint">
              {t("已连接 {{user}}@{{host}}:{{port}}。点击目录进入，选择要加入工作区的远程目录。", { user: remoteUser, host: remoteHost, port: remotePort })}
            </p>
            {remoteBrowseError ? (
              <p className="modal-error">{remoteBrowseError}</p>
            ) : null}
            <div className="browser-selection">
              <span>{t("当前目录")}</span>
              <code title={remoteData?.path ?? ""}>
                {remoteData?.path || t("读取中…")}
              </code>
            </div>

            <div className="modal-actions">
              <button
                type="button"
                className="btn ghost"
                onClick={backToRemoteForm}
              >
                {t("返回连接设置")}
              </button>
              <button
                type="button"
                className="btn primary"
                onClick={() => void addFromRemoteBrowser()}
                disabled={!remoteData?.path || remoteLoading || remoteBusy}
              >
                {remoteBusy ? t("添加中…") : t("添加此目录")}
              </button>
            </div>
          </>
        ) : (
          <form
            className="remote-form"
            onSubmit={(event) => {
              event.preventDefault();
              if (!remoteBusy) void startRemoteBrowse();
            }}
          >
            <div className="remote-grid">
              <label>
                {t("名称（可选）")}
                <input
                  value={remoteName}
                  onChange={(event) => setRemoteName(event.target.value)}
                  placeholder={t("例如 dev-notes")}
                  autoComplete="off"
                />
              </label>
              <label>
                {t("主机")}
                <input
                  ref={hostRef}
                  value={remoteHost}
                  onChange={(event) => setRemoteHost(event.target.value)}
                  placeholder={t("192.168.1.10 或 example.com")}
                  spellCheck={false}
                  autoComplete="off"
                  required
                />
              </label>
              <label>
                {t("端口")}
                <input
                  type="number"
                  min={1}
                  max={65535}
                  value={remotePort}
                  onChange={(event) => setRemotePort(event.target.value)}
                  required
                />
              </label>
              <label>
                {t("用户名")}
                <input
                  value={remoteUser}
                  onChange={(event) => setRemoteUser(event.target.value)}
                  placeholder="root"
                  spellCheck={false}
                  autoComplete="username"
                  required
                />
              </label>
              <label className="wide">
                {t("私钥路径（可选）")}
                <input
                  value={remoteKeyPath}
                  onChange={(event) => setRemoteKeyPath(event.target.value)}
                  placeholder={t("~/.ssh/id_ed25519；留空则尝试 ssh-agent / 默认私钥")}
                  spellCheck={false}
                  autoComplete="off"
                />
              </label>
              <label className="wide">
                {t("远程目录")}
                <div className="remote-path-line">
                  <input
                    value={remotePath}
                    onChange={(event) => setRemotePath(event.target.value)}
                    placeholder={t("可留空；或填写浏览起点，如 /home/me/notes")}
                    spellCheck={false}
                    autoComplete="off"
                  />
                </div>
              </label>
            </div>
            <p className="modal-hint remote-hint">
              {t("使用 SSH Agent 或本机私钥认证，不支持密码 / ProxyJump。")}
              {t("首次连接前请先在终端执行一次")}{" "}
              <code>
                ssh
                {Number(remotePort) && Number(remotePort) !== 22
                  ? ` -p ${remotePort}`
                  : ""}{" "}
                {remoteUser || "user"}@{remoteHost || "host"}
              </code>{" "}
              {t("接受主机指纹。远程文件变更每 5 秒轮询同步。")}
            </p>
            {remoteError ? (
              <p className="modal-error">{remoteError}</p>
            ) : null}
            <div className="modal-actions">
              <button type="button" className="btn ghost" onClick={onClose}>
                {t("取消")}
              </button>
              <button
                type="submit"
                className="btn primary"
                disabled={remoteBusy || remoteConnecting}
              >
                {remoteBusy || remoteConnecting
                  ? t("连接中…")
                  : remoteError
                    ? t("重新连接")
                    : t("浏览远程目录")}
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}
