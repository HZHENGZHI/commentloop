import { useTranslation } from "react-i18next";
import type { ReviewStatus } from "../types";

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

export function ReviewStatusBadge({ status }: { status: ReviewStatus }) {
  const { t } = useTranslation();
  return (
    <span className={`review-status-badge ${status}`}>
      {t(REVIEW_STATUS_LABELS[status])}
    </span>
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
    <span className={`review-status-control ${value}`}>
      <span className={`review-dot ${value}`} aria-hidden="true" />
      <select
        className="review-status-select"
        value={value}
        onChange={(event) => onChange(event.target.value as ReviewStatus)}
        disabled={disabled}
        title={t("审阅状态")}
        aria-label={t("审阅状态")}
      >
        {REVIEW_STATUSES.map((status) => (
          <option key={status} value={status}>
            {t(REVIEW_STATUS_LABELS[status])}
          </option>
        ))}
      </select>
    </span>
  );
}
