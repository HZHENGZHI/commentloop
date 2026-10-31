import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { Comment } from "../types";
import { Button, Checkbox, Empty, Input } from "antd";
import { IconLink, IconSearch } from "./Icons";
import { AppModal } from "./AppModal";

interface LinkCommentsModalProps {
  comment: Comment;
  comments: Comment[];
  agentName?: string;
  onClose: () => void;
  onSave: (memberIds: string[]) => Promise<void>;
}

function shortQuote(value: string): string {
  const text = value.replace(/\s+/g, " ").trim();
  return text.length > 72 ? `${text.slice(0, 72)}…` : text;
}

export function LinkCommentsModal({
  comment,
  comments,
  agentName = "Code Agent",
  onClose,
  onSave,
}: LinkCommentsModalProps) {
  const { t } = useTranslation();
  const [search, setSearch] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [selected, setSelected] = useState<Set<string>>(() => {
    const initial = new Set<string>();
    if (!comment.groupId) return initial;
    for (const item of comments) {
      if (item.id !== comment.id && item.groupId === comment.groupId) {
        initial.add(item.id);
      }
    }
    return initial;
  });

  const candidates = useMemo(() => {
    const query = search.trim().toLowerCase();
    return comments
      .filter((item) => item.id !== comment.id)
      .filter((item) => {
        if (!query) return true;
        return (
          item.quote.toLowerCase().includes(query) ||
          String(item.line).includes(query) ||
          (item.groupId ?? "").toLowerCase().includes(query)
        );
      })
      .sort((a, b) => {
        if (a.resolved !== b.resolved) return a.resolved ? 1 : -1;
        return (a.line || Number.MAX_SAFE_INTEGER) - (b.line || Number.MAX_SAFE_INTEGER);
      });
  }, [comments, comment.id, search]);

  const toggle = (target: Comment) => {
    setError(null);
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(target.id)) {
        next.delete(target.id);
        return next;
      }
      next.add(target.id);
      // Selecting one member of another group brings its whole group along,
      // so a save merges groups instead of silently splitting them.
      if (target.groupId) {
        for (const item of comments) {
          if (
            item.id !== comment.id &&
            item.groupId === target.groupId
          ) {
            next.add(item.id);
          }
        }
      }
      return next;
    });
  };

  const save = async () => {
    if (selected.size === 0 && !comment.groupId) return;
    setBusy(true);
    setError(null);
    try {
      await onSave(Array.from(selected));
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("保存关联失败"));
      setBusy(false);
    }
  };

  return (
    <AppModal
      title={
        <span className="app-modal-title">
          <IconLink size={17} />
          {t("关联评论")}
        </span>
      }
      width={560}
      onClose={onClose}
      className="link-comments-modal"
    >
        <p className="link-comments-hint">
          {t("同组评论会作为一条关联需求交给 {{agent}}：它会一起阅读、一起修改，每一条都会收到本轮修改前后的记录。", { agent: agentName })}
        </p>
        <div className="link-comments-current">
          <span>{t("当前评论")}</span>
          <blockquote>{shortQuote(comment.quote)}</blockquote>
        </div>
        <Input
          autoFocus
          allowClear
          prefix={<IconSearch size={13} />}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
          placeholder={t("搜索评论内容或行号")}
          style={{ marginBottom: 8 }}
        />
        <div className="link-comments-list">
          {candidates.length === 0 ? (
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description={
                comments.length <= 1
                  ? t("还没有其他评论可以关联")
                  : t("没有匹配的评论")
              }
            />
          ) : (
            candidates.map((item) => {
              const checked = selected.has(item.id);
              return (
                <div
                  key={item.id}
                  className={`link-comment-row${checked ? " checked" : ""}`}
                  onClick={() => toggle(item)}
                >
                  <Checkbox
                    checked={checked}
                    onChange={() => toggle(item)}
                    onClick={(event) => event.stopPropagation()}
                  />
                  <span className="link-comment-line">
                    {item.line > 0 ? t("第 {{line}} 行", { line: item.line }) : t("未定位")}
                  </span>
                  <span className="link-comment-quote">
                    {shortQuote(item.quote)}
                  </span>
                  {item.groupId ? (
                    <span className="link-comment-group-tag">{t("已有关联")}</span>
                  ) : null}
                  {item.resolved ? (
                    <span className="link-comment-resolved">{t("已解决")}</span>
                  ) : null}
                </div>
              );
            })
          )}
        </div>
        {error ? <p className="modal-error">{error}</p> : null}
        <div className="modal-actions link-comments-actions">
          <span className="link-comments-count">
            {selected.size > 0 ? t("{{count}} 条评论同组", { count: selected.size + 1 }) : t("未关联")}
          </span>
          <Button onClick={onClose}>{t("取消")}</Button>
          <Button
            type="primary"
            loading={busy}
            disabled={busy || (selected.size === 0 && !comment.groupId)}
            onClick={() => void save()}
          >
            {busy
              ? t("保存中…")
              : selected.size === 0
                ? t("取消关联")
                : t("保存关联")}
          </Button>
        </div>
    </AppModal>
  );
}
