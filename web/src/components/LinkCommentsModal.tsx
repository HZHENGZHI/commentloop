import { useEffect, useMemo, useRef, useState } from "react";
import type { Comment } from "../types";
import { IconLink, IconSearch, IconX } from "./Icons";

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
  const [search, setSearch] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);
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

  useEffect(() => {
    searchRef.current?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

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
      setError(err instanceof Error ? err.message : "保存关联失败");
      setBusy(false);
    }
  };

  return (
    <div
      className="modal-backdrop comment-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        className="modal link-comments-modal"
        role="dialog"
        aria-modal="true"
        aria-label="关联评论"
      >
        <div className="modal-head">
          <div className="modal-title">
            <IconLink size={17} />
            关联评论
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
        <p className="link-comments-hint">
          同组评论会作为一条关联需求交给 {agentName}：它会一起阅读、一起修改，
          每一条都会收到本轮修改前后的记录。
        </p>
        <div className="link-comments-current">
          <span>当前评论</span>
          <blockquote>{shortQuote(comment.quote)}</blockquote>
        </div>
        <label className="link-comments-search">
          <IconSearch size={13} />
          <input
            ref={searchRef}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="搜索评论内容或行号"
          />
        </label>
        <div className="link-comments-list">
          {candidates.length === 0 ? (
            <div className="link-comments-empty">
              {comments.length <= 1
                ? "还没有其他评论可以关联"
                : "没有匹配的评论"}
            </div>
          ) : (
            candidates.map((item) => {
              const checked = selected.has(item.id);
              return (
                <label
                  key={item.id}
                  className={`link-comment-row${checked ? " checked" : ""}`}
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    onChange={() => toggle(item)}
                  />
                  <span className="link-comment-line">
                    {item.line > 0 ? `第 ${item.line} 行` : "未定位"}
                  </span>
                  <span className="link-comment-quote">
                    {shortQuote(item.quote)}
                  </span>
                  {item.groupId ? (
                    <span className="link-comment-group-tag">已有关联</span>
                  ) : null}
                  {item.resolved ? (
                    <span className="link-comment-resolved">已解决</span>
                  ) : null}
                </label>
              );
            })
          )}
        </div>
        {error ? <p className="modal-error">{error}</p> : null}
        <div className="modal-actions link-comments-actions">
          <span className="link-comments-count">
            {selected.size > 0 ? `${selected.size + 1} 条评论同组` : "未关联"}
          </span>
          <button type="button" className="btn ghost" onClick={onClose}>
            取消
          </button>
          <button
            type="button"
            className="btn primary"
            disabled={busy || (selected.size === 0 && !comment.groupId)}
            onClick={() => void save()}
          >
            {busy
              ? "保存中…"
              : selected.size === 0
                ? "取消关联"
                : "保存关联"}
          </button>
        </div>
      </div>
    </div>
  );
}
