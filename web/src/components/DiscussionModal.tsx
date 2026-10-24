import { useCallback, useEffect, useRef, useState } from "react";
import {
  deleteDiscussion,
  fetchAvailableDiscussionSessions,
  fetchDocDiscussions,
  importDiscussionSession,
  refreshConsensus,
} from "../api";
import type {
  DetectedAgent,
  DiscussionListResponse,
  DiscussionSession,
} from "../types";
import {
  IconAlert,
  IconCheck,
  IconDownload,
  IconMessage,
  IconPlus,
  IconRefresh,
  IconTrash,
  IconX,
} from "./Icons";

interface DiscussionModalProps {
  docPath: string;
  onClose: () => void;
  onChanged: () => void;
}

function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function DiscussionModal({
  docPath,
  onClose,
  onChanged,
}: DiscussionModalProps) {
  const [data, setData] = useState<DiscussionListResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<"refresh" | "import" | string>("");
  const [sessionsOpen, setSessionsOpen] = useState(false);
  const [sessions, setSessions] = useState<DiscussionSession[]>([]);
  const [agents, setAgents] = useState<DetectedAgent[]>([]);
  const [agentFilter, setAgentFilter] = useState("");
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [sessionsError, setSessionsError] = useState("");
  const sessionsSeq = useRef(0);

  const load = useCallback(async (showLoading = false) => {
    if (showLoading) setLoading(true);
    setError("");
    try {
      const next = await fetchDocDiscussions(docPath);
      setData(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : "读取讨论上下文失败");
    } finally {
      if (showLoading) setLoading(false);
    }
  }, [docPath]);

  const loadSessions = useCallback(async () => {
    const seq = ++sessionsSeq.current;
    setSessionsLoading(true);
    setSessionsError("");
    try {
      const result = await fetchAvailableDiscussionSessions(docPath);
      if (seq !== sessionsSeq.current) return;
      setSessions(result.sessions ?? []);
      setAgents(result.agents ?? []);
    } catch (err) {
      if (seq !== sessionsSeq.current) return;
      setSessionsError(err instanceof Error ? err.message : "读取会话失败");
    } finally {
      if (seq === sessionsSeq.current) setSessionsLoading(false);
    }
  }, [docPath]);

  useEffect(() => {
    void load(true);
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [load, onClose]);

  const openSessions = () => {
    const next = !sessionsOpen;
    setSessionsOpen(next);
    if (next) void loadSessions();
  };

  const doRefresh = async () => {
    setBusy("refresh");
    setError("");
    try {
      await refreshConsensus(docPath);
      await load();
      onChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : "刷新共识失败");
    } finally {
      setBusy("");
    }
  };

  const doImport = async (session: DiscussionSession) => {
    if (session.imported) return;
    setBusy("import");
    setError("");
    try {
      const result = await importDiscussionSession(docPath, session.path);
      setSessions((current) =>
        current.map((item) =>
          item.path === session.path ? { ...item, imported: true } : item,
        ),
      );
      await load();
      onChanged();
      if (result.consensusError) {
        setError(`会话已导入，但共识提炼失败：${result.consensusError}`);
      }
      setSessionsOpen(true);
      void loadSessions();
    } catch (err) {
      setError(err instanceof Error ? err.message : "导入会话失败");
    } finally {
      setBusy("");
    }
  };

  const doDelete = async (id: string) => {
    if (!window.confirm("从当前文档移除这个讨论会话？不会删除原始会话文件。")) {
      return;
    }
    setBusy(id);
    setError("");
    try {
      await deleteDiscussion(docPath, id);
      await load();
      onChanged();
      void loadSessions();
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除讨论失败");
    } finally {
      setBusy("");
    }
  };

  const visibleSessions = agentFilter
    ? sessions.filter((item) => item.agent === agentFilter)
    : sessions;
  const selectedAgent = agents.find((item) => item.id === agentFilter);
  const discussions = data?.discussions ?? [];

  return (
    <div
      className="modal-backdrop comment-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        className="modal discussion-modal"
        role="dialog"
        aria-modal="true"
        aria-label="讨论上下文"
      >
        <div className="modal-head">
          <div className="modal-title">
            <IconMessage size={17} />
            讨论上下文
          </div>
          <button
            type="button"
            className="icon-btn small"
            onClick={onClose}
            aria-label="关闭"
          >
            <IconX size={15} />
          </button>
        </div>
        <p className="discussion-hint">
          导入之前与 Agent 的讨论，提炼成文档共识。之后评论修改会自动参考共识，你也可以在评论里额外选择具体讨论。
        </p>

        {error ? (
          <p className="modal-error">
            <IconAlert size={13} /> {error}
          </p>
        ) : null}

        <section className="discussion-section">
          <div className="discussion-section-head">
            <span>文档共识</span>
            <span className="discussion-section-meta">
              {data?.consensus
                ? `v${data.consensus.version} · ${formatDate(data.consensus.updatedAt)}`
                : "尚未生成"}
            </span>
            <button
              type="button"
              className="btn ghost discussion-refresh"
              onClick={() => void doRefresh()}
              disabled={Boolean(busy) || discussions.length === 0}
              title={discussions.length === 0 ? "先导入讨论会话" : "重新提炼共识"}
            >
              {busy === "refresh" ? (
                <span className="spinner" />
              ) : (
                <IconRefresh size={12} />
              )}
              刷新共识
            </button>
          </div>
          {loading ? (
            <div className="discussion-empty">正在读取…</div>
          ) : data?.consensus ? (
            <pre className="discussion-consensus">{data.consensus.content}</pre>
          ) : (
            <div className="discussion-empty">
              导入讨论后会自动提炼目标、决策、约束和待确认事项。
            </div>
          )}
        </section>

        <section className="discussion-section">
          <div className="discussion-section-head">
            <span>已导入讨论</span>
            <span className="discussion-section-meta">
              {discussions.length} 个会话
            </span>
            <button
              type="button"
              className="btn primary discussion-import-toggle"
              onClick={openSessions}
            >
              <IconPlus size={12} />
              导入会话
            </button>
          </div>
          {discussions.length === 0 ? (
            <div className="discussion-empty">还没有导入讨论会话</div>
          ) : (
            <div className="discussion-list">
              {discussions.map((item) => (
                <div className="discussion-row" key={item.id}>
                  <IconMessage size={14} />
                  <div className="discussion-row-main">
                    <div className="discussion-row-title">{item.title}</div>
                    <div className="discussion-row-meta">
                      {item.agentName || item.sourceKind} · {item.messageCount} 条消息 · 导入于{" "}
                      {formatDate(item.importedAt)}
                    </div>
                    <div className="discussion-row-path">{item.sourcePath}</div>
                  </div>
                  <button
                    type="button"
                    className="icon-btn small"
                    onClick={() => void doDelete(item.id)}
                    disabled={busy === item.id}
                    title="移除讨论"
                    aria-label="移除讨论"
                  >
                    {busy === item.id ? (
                      <span className="spinner" />
                    ) : (
                      <IconTrash size={13} />
                    )}
                  </button>
                </div>
              ))}
            </div>
          )}
        </section>

        {sessionsOpen ? (
          <section className="discussion-section discussion-sessions">
            <div className="discussion-section-head">
              <span>检测到的 Agent</span>
              <span className="discussion-section-meta">
                {agents.filter((item) => item.installed).length} 个 Agent ·{" "}
                {sessions.length} 个会话
              </span>
              <button
                type="button"
                className="icon-btn small"
                onClick={() => setSessionsOpen(false)}
                aria-label="收起会话列表"
              >
                <IconX size={13} />
              </button>
            </div>
            <div className="discussion-agent-tabs">
              <button
                type="button"
                className={!agentFilter ? "active" : ""}
                onClick={() => setAgentFilter("")}
              >
                全部
                <span>{sessions.length}</span>
              </button>
              {agents.map((agent) => (
                <button
                  key={agent.id}
                  type="button"
                  className={agentFilter === agent.id ? "active" : ""}
                  onClick={() => setAgentFilter(agent.id)}
                  title={
                    agent.supported
                      ? `已检测到 ${agent.count} 个 ${agent.name} 会话`
                      : "已检测到这个 Agent，但暂未支持读取它的会话"
                  }
                >
                  {agent.name}
                  <span>{agent.count}</span>
                  {!agent.supported ? <em>暂未支持</em> : null}
                </button>
              ))}
            </div>
            {sessionsLoading ? (
              <div className="discussion-empty">正在扫描会话…</div>
            ) : sessionsError ? (
              <div className="discussion-empty error">{sessionsError}</div>
            ) : visibleSessions.length === 0 ? (
              <div className="discussion-empty">
                {selectedAgent && !selectedAgent.supported
                  ? `已检测到 ${selectedAgent.name}，但暂未支持读取它的会话`
                  : "没有可导入的会话"}
              </div>
            ) : (
              <div className="discussion-session-list">
                {visibleSessions.map((session) => (
                  <div className="discussion-session-row" key={session.path}>
                    <IconDownload size={13} />
                    <div className="discussion-row-main">
                      <div className="discussion-row-title">{session.title}</div>
                      <div className="discussion-row-meta">
                        {session.agentName || session.sourceKind} · {session.messageCount} 条消息 ·{" "}
                        {formatDate(session.updatedAt)}
                      </div>
                      <div className="discussion-row-path">
                        {session.cwd || session.path}
                      </div>
                    </div>
                    <button
                      type="button"
                      className="btn ghost small-btn"
                      onClick={() => void doImport(session)}
                      disabled={Boolean(busy) || session.imported}
                    >
                      {session.imported
                        ? "已导入"
                        : busy === "import"
                          ? "提炼中…"
                          : "导入"}
                    </button>
                  </div>
                ))}
              </div>
            )}
          </section>
        ) : null}

        <div className="modal-actions">
          {data?.consensus ? (
            <span className="discussion-saved">
              <IconCheck size={12} /> 评论修改会自动参考这份共识
            </span>
          ) : null}
          <button type="button" className="btn ghost" onClick={onClose}>
            关闭
          </button>
        </div>
      </div>
    </div>
  );
}
