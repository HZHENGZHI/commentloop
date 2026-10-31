import { useCallback, useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import i18n from "../i18n";
import { localizeServerMessage } from "../i18n/serverMessages";
import {
  deleteDiscussion,
  fetchAvailableDiscussionSessions,
  importDiscussionSession,
  refreshConsensus,
} from "../api";
import type {
  ConsensusActivity,
  ConsensusPhase,
  DetectedAgent,
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
import { discussionsQuery, invalidate, queryClient } from "../queries";

interface DiscussionModalProps {
  docPath: string;
  activity?: ConsensusActivity;
  onClose: () => void;
}

const CONSENSUS_STEPS = ["整理讨论", "提炼共识", "保存结果"];

function consensusStage(status: ConsensusPhase): number {
  switch (status) {
    case "summarizing":
      return 1;
    case "saving":
    case "done":
      return 2;
    case "error":
      return 1;
    default:
      return 0;
  }
}

function consensusLabel(status: ConsensusPhase): string {
  switch (status) {
    case "queued":
      return i18n.t("准备更新共识…");
    case "collecting":
      return i18n.t("正在整理讨论内容…");
    case "summarizing":
      return i18n.t("Agent 正在提炼共识…");
    case "saving":
      return i18n.t("正在保存共识…");
    case "done":
      return i18n.t("共识已更新");
    case "error":
      return i18n.t("共识更新失败");
  }
}

function consensusWidth(status: ConsensusPhase): string {
  switch (status) {
    case "collecting":
      return "28%";
    case "summarizing":
      return "66%";
    case "saving":
      return "90%";
    case "done":
      return "100%";
    case "error":
      return "100%";
    default:
      return "8%";
  }
}

function formatDate(value: string, locale: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString(locale, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function DiscussionModal({
  docPath,
  activity,
  onClose,
}: DiscussionModalProps) {
  const { t, i18n } = useTranslation();
  const query = useQuery({ ...discussionsQuery(docPath), refetchOnMount: "always" });
  const data = query.data;
  const loading = query.isPending;
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<"refresh" | "import" | string>("");
  const [sessionsOpen, setSessionsOpen] = useState(false);
  const [sessions, setSessions] = useState<DiscussionSession[]>([]);
  const [agents, setAgents] = useState<DetectedAgent[]>([]);
  const [agentFilter, setAgentFilter] = useState("");
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [sessionsError, setSessionsError] = useState("");
  const sessionsSeq = useRef(0);

  const load = () => invalidate("discussions");

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
      setSessionsError(err instanceof Error ? err.message : t("读取会话失败"));
    } finally {
      if (seq === sessionsSeq.current) setSessionsLoading(false);
    }
  }, [docPath]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

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
    } catch (err) {
      setError(err instanceof Error ? err.message : t("刷新共识失败"));
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
      if (result.consensus) {
        queryClient.setQueryData(discussionsQuery(docPath).queryKey, (current) =>
          current ? { ...current, consensus: result.consensus } : current,
        );
      }
      setSessions((current) =>
        current.map((item) =>
          item.path === session.path ? { ...item, imported: true } : item,
        ),
      );
      await load();
      if (result.consensusError) {
        setError(t("会话已导入，但共识提炼失败：{{error}}", { error: result.consensusError }));
      }
      setSessionsOpen(true);
      void loadSessions();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("导入会话失败"));
    } finally {
      setBusy("");
    }
  };

  const doDelete = async (id: string) => {
    if (!window.confirm(t("从当前文档移除这个讨论会话？不会删除原始会话文件。"))) {
      return;
    }
    setBusy(id);
    setError("");
    try {
      await deleteDiscussion(docPath, id);
      await load();
      void loadSessions();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("删除讨论失败"));
    } finally {
      setBusy("");
    }
  };

  const visibleSessions = agentFilter
    ? sessions.filter((item) => item.agent === agentFilter)
    : sessions;
  const selectedAgent = agents.find((item) => item.id === agentFilter);
  const discussions = data?.discussions ?? [];
  const effectiveActivity =
    activity ??
    (busy === "import"
      ? { path: docPath, status: "queued" as const, message: t("正在导入会话…") }
      : busy === "refresh"
        ? {
            path: docPath,
            status: "collecting" as const,
            message: t("正在准备刷新共识…"),
          }
        : undefined);
  const stage = effectiveActivity
    ? consensusStage(effectiveActivity.status)
    : -1;
  const working =
    effectiveActivity &&
    effectiveActivity.status !== "done" &&
    effectiveActivity.status !== "error";

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
        aria-label={t("讨论上下文")}
      >
        <div className="modal-head">
          <div className="modal-title">
            <IconMessage size={17} />
            {t("讨论上下文")}
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
        <p className="discussion-hint">
          {t("导入之前与 Agent 的讨论，提炼成文档共识。之后评论修改会自动参考共识，你也可以在评论里额外选择具体讨论。")}
        </p>

        {error || query.error ? (
          <p className="modal-error">
            <IconAlert size={13} />{" "}
            {error ||
              (query.error instanceof Error ? query.error.message : t("读取讨论上下文失败"))}
          </p>
        ) : null}

        <section className="discussion-section">
          <div className="discussion-section-head">
            <span>{t("文档共识")}</span>
            <span className="discussion-section-meta">
              {data?.consensus
                ? `v${data.consensus.version} · ${formatDate(data.consensus.updatedAt, i18n.language)}`
                : t("尚未生成")}
            </span>
            <button
              type="button"
              className="btn ghost discussion-refresh"
              onClick={() => void doRefresh()}
              disabled={Boolean(busy) || discussions.length === 0}
              title={discussions.length === 0 ? t("先导入讨论会话") : t("重新提炼共识")}
            >
              {busy === "refresh" ? (
                <span className="spinner" />
              ) : (
                <IconRefresh size={12} />
              )}
              {t("刷新共识")}
            </button>
          </div>
          {effectiveActivity ? (
            <div
              className={`consensus-progress ${effectiveActivity.status}`}
              role="status"
              aria-live="polite"
            >
              <div className="consensus-progress-head">
                {working ? (
                  <span className="spinner" />
                ) : effectiveActivity.status === "done" ? (
                  <IconCheck size={13} />
                ) : (
                  <IconAlert size={13} />
                )}
                <span>
                  {localizeServerMessage(
                    effectiveActivity.message || "",
                    effectiveActivity.status,
                  ) || consensusLabel(effectiveActivity.status)}
                </span>
              </div>
              <div className="consensus-progress-bar">
                <span
                  style={{ width: consensusWidth(effectiveActivity.status) }}
                />
              </div>
              <div className="consensus-progress-steps">
                {CONSENSUS_STEPS.map((label, index) => (
                  <span
                    key={label}
                    className={
                      effectiveActivity.status === "done" || index < stage
                        ? "done"
                        : index === stage
                          ? "active"
                          : ""
                    }
                  >
                    {t(label)}
                  </span>
                ))}
              </div>
            </div>
          ) : null}
          {loading ? (
            <div className="discussion-empty">{t("正在读取…")}</div>
          ) : data?.consensus ? (
            <pre className="discussion-consensus">{data.consensus.content}</pre>
          ) : (
            <div className="discussion-empty">
              {t("导入讨论后会自动提炼目标、决策、约束和待确认事项。")}
            </div>
          )}
        </section>

        <section className="discussion-section">
          <div className="discussion-section-head">
            <span>{t("已导入讨论")}</span>
            <span className="discussion-section-meta">
              {t("{{count}} 个会话", { count: discussions.length })}
            </span>
            <button
              type="button"
              className="btn primary discussion-import-toggle"
              onClick={openSessions}
            >
              <IconPlus size={12} />
              {t("导入会话")}
            </button>
          </div>
          {discussions.length === 0 ? (
            <div className="discussion-empty">{t("还没有导入讨论会话")}</div>
          ) : (
            <div className="discussion-list">
              {discussions.map((item) => (
                <div className="discussion-row" key={item.id}>
                  <IconMessage size={14} />
                  <div className="discussion-row-main">
                    <div className="discussion-row-title">{item.title}</div>
                    <div className="discussion-row-meta">
                      {item.agentName || item.sourceKind} ·{" "}
                      {t("{{count}} 条消息", { count: item.messageCount })} ·{" "}
                      {t("导入于")}{" "}
                      {formatDate(item.importedAt, i18n.language)}
                    </div>
                    <div className="discussion-row-path">{item.sourcePath}</div>
                  </div>
                  <button
                    type="button"
                    className="icon-btn small"
                    onClick={() => void doDelete(item.id)}
                    disabled={busy === item.id}
                    title={t("移除讨论")}
                    aria-label={t("移除讨论")}
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
              <span>{t("检测到的 Agent")}</span>
              <span className="discussion-section-meta">
                {t("{{count}} 个 Agent", { count: agents.filter((item) => item.installed).length })} ·{" "}
                {t("{{count}} 个会话", { count: sessions.length })}
              </span>
              <button
                type="button"
                className="icon-btn small"
                onClick={() => setSessionsOpen(false)}
                aria-label={t("收起会话列表")}
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
                {t("全部")}
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
                      ? t("已检测到 {{count}} 个 {{name}} 会话", { count: agent.count, name: agent.name })
                      : t("已检测到这个 Agent，但暂未支持读取它的会话")
                  }
                >
                  {agent.name}
                  <span>{agent.count}</span>
                  {!agent.supported ? <em>{t("暂未支持")}</em> : null}
                </button>
              ))}
            </div>
            {sessionsLoading ? (
              <div className="discussion-empty">{t("正在扫描会话…")}</div>
            ) : sessionsError ? (
              <div className="discussion-empty error">{sessionsError}</div>
            ) : visibleSessions.length === 0 ? (
              <div className="discussion-empty">
                {selectedAgent && !selectedAgent.supported
                  ? t("已检测到 {{name}}，但暂未支持读取它的会话", { name: selectedAgent.name })
                  : t("没有可导入的会话")}
              </div>
            ) : (
              <div className="discussion-session-list">
                {visibleSessions.map((session) => (
                  <div className="discussion-session-row" key={session.path}>
                    <IconDownload size={13} />
                    <div className="discussion-row-main">
                      <div className="discussion-row-title">{session.title}</div>
                      <div className="discussion-row-meta">
                        {session.agentName || session.sourceKind} ·{" "}
                        {t("{{count}} 条消息", { count: session.messageCount })} ·{" "}
                        {formatDate(session.updatedAt, i18n.language)}
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
                        ? t("已导入")
                        : busy === "import"
                          ? t("提炼中…")
                          : t("导入")}
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
              <IconCheck size={12} /> {t("评论修改会自动参考这份共识")}
            </span>
          ) : null}
          <button type="button" className="btn ghost" onClick={onClose}>
            {t("关闭")}
          </button>
        </div>
      </div>
    </div>
  );
}
