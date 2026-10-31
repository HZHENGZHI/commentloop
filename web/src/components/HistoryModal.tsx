import { useEffect, useMemo, useState } from "react";
import { Alert, Button, Empty, Popconfirm, Spin } from "antd";
import { useTranslation } from "react-i18next";
import {
  fetchDocVersion,
  fetchDocVersions,
  restoreDocVersion,
} from "../api";
import type { DocResponse, DocVersion, DocVersionDetail } from "../types";
import { diffLines } from "../diff";
import { AppModal } from "./AppModal";

interface HistoryModalProps {
  path: string;
  currentContent: string;
  canRestore?: boolean;
  onClose: () => void;
  onRestored: (doc: DocResponse) => void;
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function formatVersionTime(value: string, locale: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString(locale, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function HistoryModal({
  path,
  currentContent,
  canRestore = true,
  onClose,
  onRestored,
}: HistoryModalProps) {
  const { t, i18n } = useTranslation();
  const [versions, setVersions] = useState<DocVersion[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [detail, setDetail] = useState<DocVersionDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [restoring, setRestoring] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchDocVersions(path)
      .then((response) => {
        if (cancelled) return;
        setVersions(response.versions);
        setSelected(response.versions[0]?.id ?? null);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errorText(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [path]);

  useEffect(() => {
    if (!selected) {
      setDetail(null);
      return;
    }
    let cancelled = false;
    setLoadingDetail(true);
    fetchDocVersion(path, selected)
      .then((version) => {
        if (!cancelled) setDetail(version);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errorText(err));
      })
      .finally(() => {
        if (!cancelled) setLoadingDetail(false);
      });
    return () => {
      cancelled = true;
    };
  }, [path, selected]);

  const diff = useMemo(
    () => (detail ? diffLines(currentContent, detail.content) : []),
    [currentContent, detail],
  );

  const restore = async () => {
    if (!selected) return;
    setRestoring(true);
    setError(null);
    try {
      const doc = await restoreDocVersion(path, selected);
      onRestored(doc);
    } catch (err) {
      setError(errorText(err));
    } finally {
      setRestoring(false);
    }
  };

  return (
    <AppModal
      title={t("版本历史")}
      width={820}
      onClose={onClose}
      className="history-modal"
      footer={
        <div className="history-footer">
          <span className="history-hint">
            {canRestore ? t("与当前文档对比") : t("审阅模式只读，无法恢复")}
          </span>
          <div className="history-footer-actions">
            <Button onClick={onClose}>{t("取消")}</Button>
            <Popconfirm
              title={t("恢复后将覆盖当前内容，确认？")}
              onConfirm={() => void restore()}
              disabled={!selected || restoring || !canRestore}
            >
              <Button
                type="primary"
                disabled={!selected || !canRestore}
                loading={restoring}
              >
                {t("恢复此版本")}
              </Button>
            </Popconfirm>
          </div>
        </div>
      }
    >
      {error ? (
        <Alert
          type="error"
          showIcon
          message={t("操作失败：{{error}}", { error })}
          className="history-error"
        />
      ) : null}
      {loading ? (
        <div className="history-loading">
          <Spin />
        </div>
      ) : versions.length === 0 ? (
        <Empty description={t("还没有历史版本")} />
      ) : (
        <div className="history-layout">
          <div className="history-list">
            {versions.map((version, index) => (
              <button
                key={version.id}
                type="button"
                className={`history-item${selected === version.id ? " active" : ""}`}
                onClick={() => setSelected(version.id)}
              >
                <span className="history-time">
                  {formatVersionTime(version.createdAt, i18n.language)}
                </span>
                <span className="history-item-meta">
                  {index === 0 ? <em>{t("当前")}</em> : null}
                  <span>{Math.max(1, Math.round(version.size / 1024))} KB</span>
                </span>
              </button>
            ))}
          </div>
          <div className="history-diff-pane">
            {loadingDetail || !detail ? (
              <div className="history-loading">
                <Spin />
              </div>
            ) : (
              <pre className="history-diff">
                {diff.map((line, index) => (
                  <div
                    key={`${index}-${line.kind}`}
                    className={`diff-line ${line.kind}`}
                  >
                    <span className="diff-sign">
                      {line.kind === "add"
                        ? "+"
                        : line.kind === "remove"
                          ? "−"
                          : " "}
                    </span>
                    {line.text}
                  </div>
                ))}
              </pre>
            )}
          </div>
        </div>
      )}
    </AppModal>
  );
}
