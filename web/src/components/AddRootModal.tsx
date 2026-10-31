import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type FormEvent,
} from "react";
import { useTranslation } from "react-i18next";
import {
  Button,
  Checkbox,
  Empty,
  Input,
  Tabs,
  Tag,
  type InputRef,
} from "antd";
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
} from "./Icons";
import { AppModal } from "./AppModal";

interface AddRootModalProps {
  roots: RootInfo[];
  initialPath: string;
  onClose: () => void;
  onAddLocal: (path: string) => Promise<void>;
  onAddRemote: (config: AddRemoteRootRequest) => Promise<void>;
}

type RootTab = "local" | "remote";

export function AddRootModal({
  roots,
  initialPath,
  onClose,
  onAddLocal,
  onAddRemote,
}: AddRootModalProps) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<RootTab>("local");
  const [data, setData] = useState<BrowseResponse | null>(null);
  const [pathInput, setPathInput] = useState(initialPath);
  const [showHidden, setShowHidden] = useState(false);
  const [loading, setLoading] = useState(true);
  const [localBusy, setLocalBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const seq = useRef(0);
  const pathRef = useRef<InputRef>(null);
  const hostRef = useRef<InputRef>(null);

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
  }, [initialPath, load]);

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

  const switchTab = (next: RootTab) => {
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
    <Button
      key={entry.path}
      type="text"
      block
      className="browser-row"
      title={entry.path}
      onClick={() => navigate(entry.path)}
    >
      <span className="browser-row-icon">
        <IconFolder size={15} />
      </span>
      <span className="browser-row-name">{entry.name}</span>
      <IconChevronRight size={13} />
    </Button>
  );

  const renderRemoteEntry = (entry: BrowseEntry) => (
    <Button
      key={entry.path}
      type="text"
      block
      className="browser-row"
      title={entry.path}
      onClick={() => void loadRemote(entry.path, remoteShowHidden)}
    >
      <span className="browser-row-icon">
        <IconFolder size={15} />
      </span>
      <span className="browser-row-name">{entry.name}</span>
      <IconChevronRight size={13} />
    </Button>
  );

  const localPanel = (
    <>
      <form className="browser-path-form" onSubmit={submitPath}>
        <Button
          className="browser-up"
          icon={<IconArrowUp size={15} />}
          onClick={() => navigate(data?.parent ?? "")}
          disabled={!data?.parent}
          title={t("上一级")}
          aria-label={t("上一级")}
        />
        <div className="browser-path-wrap">
          <IconFolder size={14} />
          <Input
            ref={pathRef}
            variant="borderless"
            value={pathInput}
            onChange={(event) => setPathInput(event.target.value)}
            placeholder={t("~/Documents/notes 或 /Volumes/notes")}
            spellCheck={false}
            autoComplete="off"
            aria-label={t("当前目录")}
          />
        </div>
        <Button htmlType="submit">{t("转到")}</Button>
      </form>

      {data?.shortcuts.length ? (
        <div className="browser-shortcuts">
          {data.shortcuts.map((shortcut) => (
            <Button
              key={shortcut.path}
              size="small"
              className={`browser-shortcut${
                shortcut.path === currentPath ? " active" : ""
              }`}
              title={shortcut.path}
              icon={<IconFolder size={12} />}
              onClick={() => navigate(shortcut.path)}
            >
              {t(shortcut.name)}
            </Button>
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
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={error ? t("无法读取此目录") : t("这里没有子目录")}
            />
          </div>
        )}
      </div>

      <div className="browser-list-foot">
        <Checkbox
          className="browser-hidden-toggle"
          checked={showHidden}
          onChange={(event) => toggleHidden(event.target.checked)}
        >
          {t("显示隐藏目录")}
        </Checkbox>
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
          <Tag
            color="success"
            className="browser-added"
            style={{ marginInlineEnd: 0 }}
          >
            {t("已添加")}
          </Tag>
        ) : null}
      </div>

      <div className="modal-actions">
        <Button onClick={onClose}>{t("取消")}</Button>
        <Button
          type="primary"
          onClick={() => void addLocal()}
          loading={localBusy}
          disabled={!currentPath || loading || localBusy || alreadyAdded}
        >
          {localBusy ? t("添加中…") : t("添加此目录")}
        </Button>
      </div>
    </>
  );

  const remotePanel = remoteConnecting ? (
    <div className="remote-connecting" role="status" aria-live="polite">
      <span className="spinner remote-spinner" />
      <h3>{t("正在连接远程服务器…")}</h3>
      <code>
        {remoteUser || "user"}@{remoteHost || "host"}:{remotePort || "22"}
      </code>
      <p>{t("首次连接需要验证 SSH 密钥和主机指纹，可能需要几秒。")}</p>
      <div className="remote-connecting-actions">
        <Button type="primary" disabled>
          {t("连接中…")}
        </Button>
        <Button onClick={cancelRemoteConnect}>{t("关闭")}</Button>
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
        <Button onClick={onClose}>{t("关闭")}</Button>
      </div>
    </div>
  ) : remoteBrowsing ? (
    <>
      <form className="browser-path-form" onSubmit={submitRemoteBrowsePath}>
        <Button
          className="browser-up"
          icon={<IconArrowUp size={15} />}
          onClick={() => {
            if (remoteData?.parent) {
              void loadRemote(remoteData.parent, remoteShowHidden);
            }
          }}
          disabled={!remoteData?.parent}
          title={t("上一级")}
          aria-label={t("上一级")}
        />
        <div className="browser-path-wrap">
          <IconServer size={14} />
          <Input
            variant="borderless"
            value={remoteBrowseInput}
            onChange={(event) => setRemoteBrowseInput(event.target.value)}
            placeholder={t("远程目录路径")}
            spellCheck={false}
            autoComplete="off"
            aria-label={t("远程目录")}
          />
        </div>
        <Button htmlType="submit">{t("转到")}</Button>
      </form>

      {remoteData?.shortcuts.length ? (
        <div className="browser-shortcuts">
          {remoteData.shortcuts.map((shortcut) => (
            <Button
              key={shortcut.path}
              size="small"
              className={`browser-shortcut${
                shortcut.path === remoteData.path ? " active" : ""
              }`}
              title={shortcut.path}
              icon={<IconFolder size={12} />}
              onClick={() => void loadRemote(shortcut.path, remoteShowHidden)}
            >
              {t(shortcut.name)}
            </Button>
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
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={
                remoteBrowseError ? t("无法读取此目录") : t("这里没有子目录")
              }
            />
          </div>
        )}
      </div>

      <div className="browser-list-foot">
        <Checkbox
          className="browser-hidden-toggle"
          checked={remoteShowHidden}
          onChange={(event) => toggleRemoteHidden(event.target.checked)}
        >
          {t("显示隐藏目录")}
        </Checkbox>
        <span>
          {t("{{count}} 个子目录", {
            count: remoteData?.entries.length ?? 0,
          })}
          {remoteData?.truncated ? t("（已截断）") : ""}
        </span>
      </div>

      <p className="modal-hint remote-hint">
        {t(
          "已连接 {{user}}@{{host}}:{{port}}。点击目录进入，选择要加入工作区的远程目录。",
          { user: remoteUser, host: remoteHost, port: remotePort },
        )}
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
        <Button onClick={backToRemoteForm}>{t("返回连接设置")}</Button>
        <Button
          type="primary"
          onClick={() => void addFromRemoteBrowser()}
          loading={remoteBusy}
          disabled={!remoteData?.path || remoteLoading || remoteBusy}
        >
          {remoteBusy ? t("添加中…") : t("添加此目录")}
        </Button>
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
          <Input
            value={remoteName}
            onChange={(event) => setRemoteName(event.target.value)}
            placeholder={t("例如 dev-notes")}
            autoComplete="off"
          />
        </label>
        <label>
          {t("主机")}
          <Input
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
          <Input
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
          <Input
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
          <Input
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
            <Input
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
      {remoteError ? <p className="modal-error">{remoteError}</p> : null}
      <div className="modal-actions">
        <Button onClick={onClose}>{t("取消")}</Button>
        <Button htmlType="submit" disabled={remoteBusy || remoteConnecting}>
          {remoteBusy || remoteConnecting
            ? t("连接中…")
            : remoteError
              ? t("重新连接")
              : t("浏览远程目录")}
        </Button>
      </div>
    </form>
  );

  return (
    <AppModal
      title={
        <span className="app-modal-title">
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
        </span>
      }
      width={680}
      onClose={onClose}
      className="browser-modal"
    >
      <Tabs
        activeKey={tab}
        onChange={(key) => switchTab(key as RootTab)}
        aria-label={t("目录类型")}
        classNames={{ header: "root-tabs" }}
        destroyOnHidden
        items={[
          {
            key: "local",
            disabled: remoteBusy,
            label: (
              <span
                style={{
                  display: "inline-flex",
                  alignItems: "center",
                  gap: 6,
                }}
              >
                <IconFolder size={14} />
                {t("本地目录")}
              </span>
            ),
            children: localPanel,
          },
          {
            key: "remote",
            disabled: remoteBusy,
            label: (
              <span
                style={{
                  display: "inline-flex",
                  alignItems: "center",
                  gap: 6,
                }}
              >
                <IconServer size={14} />
                {t("远程服务器")}
              </span>
            ),
            children: remotePanel,
          },
        ]}
      />
    </AppModal>
  );
}
