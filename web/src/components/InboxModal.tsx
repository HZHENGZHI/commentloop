import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { InboxCounts, InboxDoc } from "../types";
import { IconAlert, IconChevronRight, IconInbox, IconX } from "./Icons";
import { ReviewStatusBadge, ReviewStatusDot } from "./ReviewStatus";

type InboxTab = "awaiting" | "agent" | "all";

interface InboxModalProps {
  docs: InboxDoc[];
  counts: InboxCounts;
  error?: boolean;
  onClose: () => void;
  onOpen: (path: string) => void;
}

function activityTime(value: string, locale: string): string {
  const time = Date.parse(value);
  if (!Number.isFinite(time)) return "";
  return new Date(time).toLocaleString(locale, {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function byActivity(a: InboxDoc, b: InboxDoc): number {
  return Date.parse(b.lastActivity) - Date.parse(a.lastActivity);
}

export function InboxModal({
  docs,
  counts,
  error = false,
  onClose,
  onOpen,
}: InboxModalProps) {
  const { t, i18n } = useTranslation();
  const [tab, setTab] = useState<InboxTab>(() =>
    docs.some((doc) => doc.awaiting > 0) ? "awaiting" : "all",
  );

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const awaitingDocs = useMemo(
    () =>
      docs
        .filter((doc) => doc.awaiting > 0)
        .sort((a, b) => b.awaiting - a.awaiting || byActivity(a, b)),
    [docs],
  );
  const agentDocs = useMemo(
    () =>
      docs
        .filter((doc) => doc.agentWorking > 0 || doc.pendingAgent > 0)
        .sort(
          (a, b) =>
            b.agentWorking * 2 + b.pendingAgent -
              (a.agentWorking * 2 + a.pendingAgent) || byActivity(a, b),
        ),
    [docs],
  );
  const allDocs = useMemo(() => [...docs].sort(byActivity), [docs]);

  const visibleDocs =
    tab === "awaiting" ? awaitingDocs : tab === "agent" ? agentDocs : allDocs;

  return (
    <div
      className="modal-backdrop inbox-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        className="modal inbox-modal"
        role="dialog"
        aria-modal="true"
        aria-label={t("收件箱")}
      >
        <div className="modal-head inbox-head">
          <div className="inbox-title">
            <span className="inbox-title-icon">
              <IconInbox size={17} />
            </span>
            <div>
              <div className="modal-title">{t("收件箱")}</div>
              <div className="inbox-subtitle">
                {t("{{docs}} 篇文档 · {{awaiting}} 条待我确认 · {{pending}} 条待处理", {
                  docs: docs.length,
                  awaiting: counts.awaiting,
                  pending: counts.agentWorking + counts.pendingAgent,
                })}
              </div>
            </div>
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

        <div className="inbox-tabs" role="tablist">
          {(
            [
              ["awaiting", "待我确认", awaitingDocs.length],
              ["agent", "等 Agent", agentDocs.length],
              ["all", "全部", allDocs.length],
            ] as const
          ).map(([key, label, count]) => (
            <button
              key={key}
              type="button"
              role="tab"
              aria-selected={tab === key}
              className={`inbox-tab${tab === key ? " active" : ""}`}
              onClick={() => setTab(key)}
            >
              {t(label)}
              <span className="inbox-tab-count">{count}</span>
            </button>
          ))}
        </div>

        <div className="inbox-list">
          {error ? (
            <div className="inbox-empty inbox-error">
              <IconAlert size={24} />
              <div>{t("收件箱加载失败")}</div>
              <span>{t("请重试。")}</span>
            </div>
          ) : visibleDocs.length === 0 ? (
            <div className="inbox-empty">
              <IconInbox size={24} />
              <div>{t("没有需要处理的审阅")}</div>
              <span>{t("有评论或审阅状态后，这里会按文档聚合待办。")}</span>
            </div>
          ) : (
            visibleDocs.map((doc) => (
              <button
                key={doc.path}
                type="button"
                className="inbox-row"
                onClick={() => onOpen(doc.path)}
              >
                <ReviewStatusDot
                  status={doc.status}
                  awaiting={doc.awaiting}
                  title={t("待我确认")}
                />
                <span className="inbox-row-body">
                  <span className="inbox-row-head">
                    <span className="inbox-row-name">{doc.name}</span>
                    <ReviewStatusBadge status={doc.status} />
                  </span>
                  <span className="inbox-row-path">
                    {doc.rootName}/{doc.relPath}
                  </span>
                  <span className="inbox-row-meta">
                    {doc.awaiting > 0 ? (
                      <span className="inbox-chip awaiting">
                        {t("{{count}} 条待我确认", { count: doc.awaiting })}
                      </span>
                    ) : null}
                    {doc.agentWorking > 0 ? (
                      <span className="inbox-chip working">
                        {t("{{count}} 条处理中", { count: doc.agentWorking })}
                      </span>
                    ) : null}
                    {doc.pendingAgent > 0 ? (
                      <span className="inbox-chip waiting">
                        {t("{{count}} 条等 Agent", { count: doc.pendingAgent })}
                      </span>
                    ) : null}
                    <span className="inbox-time">
                      {activityTime(doc.lastActivity, i18n.language)}
                    </span>
                  </span>
                </span>
                <IconChevronRight size={14} />
              </button>
            ))
          )}
        </div>

        <div className="inbox-foot">{t("只显示有评论或审阅状态的文档")}</div>
      </div>
    </div>
  );
}
