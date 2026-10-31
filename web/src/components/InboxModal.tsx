import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, Button, Empty, Segmented, Tag } from "antd";
import type { InboxCounts, InboxDoc } from "../types";
import { IconChevronRight, IconInbox } from "./Icons";
import { AppModal } from "./AppModal";
import { ReviewStatusBadge, ReviewStatusDot } from "./ReviewStatus";

type InboxTab = "awaiting" | "agent" | "all";

interface InboxModalProps {
  docs: InboxDoc[];
  counts: InboxCounts;
  error?: boolean;
  notificationsEnabled?: boolean;
  onEnableNotifications?: () => void;
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
  notificationsEnabled = false,
  onEnableNotifications,
  onClose,
  onOpen,
}: InboxModalProps) {
  const { t, i18n } = useTranslation();
  // No explicit choice yet: follow the data as it arrives (the inbox can open
  // before /api/inbox resolves). Once the user picks a tab, keep it.
  const [tabChoice, setTabChoice] = useState<InboxTab | null>(null);
  const autoTab: InboxTab = docs.some((doc) => doc.awaiting > 0)
    ? "awaiting"
    : "all";
  const tab = tabChoice ?? autoTab;

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
    <AppModal
      title={
        <div className="inbox-title">
          <span className="inbox-title-icon">
            <IconInbox size={17} />
          </span>
          <div>
            <div className="app-modal-title">{t("收件箱")}</div>
            <div className="inbox-subtitle">
              {t("{{docs}} 篇文档 · {{awaiting}} 条待我确认 · {{pending}} 条待处理", {
                docs: docs.length,
                awaiting: counts.awaiting,
                pending: counts.agentWorking + counts.pendingAgent,
              })}
            </div>
          </div>
        </div>
      }
      width={680}
      onClose={onClose}
      className="inbox-modal"
    >
      <Segmented
        block
        size="small"
        value={tab}
        onChange={(value) => setTabChoice(value as InboxTab)}
        style={{ marginBottom: 10 }}
        options={[
          {
            value: "awaiting",
            label: (
              <>
                {t("待我确认")}{" "}
                <span className="inbox-tab-count">{awaitingDocs.length}</span>
              </>
            ),
          },
          {
            value: "agent",
            label: (
              <>
                {t("等 Agent")}{" "}
                <span className="inbox-tab-count">{agentDocs.length}</span>
              </>
            ),
          },
          {
            value: "all",
            label: (
              <>
                {t("全部")}{" "}
                <span className="inbox-tab-count">{allDocs.length}</span>
              </>
            ),
          },
        ]}
      />

      <div className="inbox-list">
        {error ? (
          <div className="inbox-empty inbox-error">
            <Alert
              type="error"
              showIcon
              title={t("收件箱加载失败")}
              description={t("请重试。")}
            />
          </div>
        ) : visibleDocs.length === 0 ? (
          <Empty
            className="inbox-empty"
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={
              <>
                <div>{t("没有需要处理的审阅")}</div>
                <span>{t("有评论或审阅状态后，这里会按文档聚合待办。")}</span>
              </>
            }
          />
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
                    <Tag
                      color="warning"
                      variant="filled"
                      className="inbox-chip awaiting"
                      style={{ marginInlineEnd: 0 }}
                    >
                      {t("{{count}} 条待我确认", { count: doc.awaiting })}
                    </Tag>
                  ) : null}
                  {doc.agentWorking > 0 ? (
                    <Tag
                      color="processing"
                      variant="filled"
                      className="inbox-chip working"
                      style={{ marginInlineEnd: 0 }}
                    >
                      {t("{{count}} 条处理中", { count: doc.agentWorking })}
                    </Tag>
                  ) : null}
                  {doc.pendingAgent > 0 ? (
                    <Tag
                      color="default"
                      variant="filled"
                      className="inbox-chip waiting"
                      style={{ marginInlineEnd: 0 }}
                    >
                      {t("{{count}} 条等 Agent", { count: doc.pendingAgent })}
                    </Tag>
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

      <div className="inbox-foot">
        <span>{t("只显示有评论或审阅状态的文档")}</span>
        {notificationsEnabled ? (
          <span className="inbox-notify on">{t("桌面通知已开启")}</span>
        ) : (
          <Button
            size="small"
            type="link"
            className="inbox-notify"
            onClick={onEnableNotifications}
          >
            {t("开启桌面通知")}
          </Button>
        )}
      </div>
    </AppModal>
  );
}
