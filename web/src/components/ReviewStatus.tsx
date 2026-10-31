import { Select, Tag } from "antd";
import { useTranslation } from "react-i18next";
import type { ReviewSeverity, ReviewStatus } from "../types";

export const REVIEW_STATUSES: ReviewStatus[] = [
  "draft",
  "in_review",
  "changes_requested",
  "approved",
];

export const REVIEW_STATUS_LABELS: Record<ReviewStatus, string> = {
  draft: "草稿",
  in_review: "审阅中",
  changes_requested: "需要修改",
  approved: "已通过",
};

export const SEVERITY_LABELS: Record<ReviewSeverity, string> = {
  blocking: "阻塞",
  should_fix: "应修",
  nit: "小问题",
  question: "疑问",
};

export function SeverityBadge({ severity }: { severity: ReviewSeverity }) {
  const { t } = useTranslation();
  const color =
    severity === "blocking"
      ? "red"
      : severity === "should_fix"
        ? "orange"
        : severity === "question"
          ? "blue"
          : undefined;
  return (
    <Tag bordered={false} color={color}>
      {t(SEVERITY_LABELS[severity])}
    </Tag>
  );
}

export function ReviewStatusBadge({ status }: { status: ReviewStatus }) {
  const { t } = useTranslation();
  const color =
    status === "in_review"
      ? "blue"
      : status === "changes_requested"
        ? "orange"
        : status === "approved"
          ? "green"
          : undefined;
  return (
    <Tag bordered={false} color={color}>
      {t(REVIEW_STATUS_LABELS[status])}
    </Tag>
  );
}

export function ReviewStatusDot({
  status,
  awaiting = 0,
  title,
}: {
  status: ReviewStatus;
  awaiting?: number;
  title?: string;
}) {
  return (
    <span
      className={`review-dot-wrap${awaiting > 0 ? " has-awaiting" : ""}`}
      title={title}
      aria-hidden="true"
    >
      <span className={`review-dot ${status}`} />
      {awaiting > 0 ? <span className="review-dot-count">{awaiting}</span> : null}
    </span>
  );
}

export function ReviewStatusSelect({
  value,
  onChange,
  disabled = false,
}: {
  value: ReviewStatus;
  onChange: (status: ReviewStatus) => void;
  disabled?: boolean;
}) {
  const { t } = useTranslation();
  return (
    <Select
      size="small"
      variant="borderless"
      className={`review-status-control ${value}`}
      value={value}
      onChange={(next) => onChange(next as ReviewStatus)}
      disabled={disabled}
      title={t("审阅状态")}
      aria-label={t("审阅状态")}
      popupMatchSelectWidth={false}
      options={REVIEW_STATUSES.map((status) => ({
        value: status,
        label: (
          <span className="review-status-option">
            <span className={`review-dot ${status}`} aria-hidden="true" />
            {t(REVIEW_STATUS_LABELS[status])}
          </span>
        ),
      }))}
    />
  );
}
