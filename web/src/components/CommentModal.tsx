import { useEffect, useRef, useState, type FormEvent } from "react";
import { IconMessage, IconX } from "./Icons";

interface CommentModalProps {
  quote: string;
  agentName?: string;
  onClose: () => void;
  onSubmit: (body: string) => Promise<void>;
}

export function CommentModal({
  quote,
  agentName = "Code Agent",
  onClose,
  onSubmit,
}: CommentModalProps) {
  const [body, setBody] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    textareaRef.current?.focus();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!body.trim()) {
      setError("请输入评论内容");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await onSubmit(body.trim());
    } catch (err) {
      setError(err instanceof Error ? err.message : "添加评论失败");
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
      <div className="modal comment-modal" role="dialog" aria-modal="true" aria-label="添加评论">
        <div className="modal-head">
          <div className="modal-title">
            <IconMessage size={17} />
            添加评论
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
        <form onSubmit={submit}>
          <div className="comment-quote">
            <span>选中内容</span>
            <blockquote>{quote}</blockquote>
          </div>
          <label className="modal-label" htmlFor="comment-body">
            评论内容
          </label>
          <textarea
            id="comment-body"
            ref={textareaRef}
            className="comment-textarea"
            value={body}
            onChange={(event) => {
              setBody(event.target.value);
              setError(null);
            }}
            placeholder={`写下你的意见，${agentName} 可以通过 MCP 读取并回复…`}
            rows={5}
          />
          {error ? <p className="modal-error">{error}</p> : null}
          <div className="modal-actions">
            <button type="button" className="btn ghost" onClick={onClose}>
              取消
            </button>
            <button type="submit" className="btn primary" disabled={busy}>
              {busy ? "添加中…" : "添加评论"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
