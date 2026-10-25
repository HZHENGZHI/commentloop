import { useEffect, useRef, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import type { Discussion, PromptTemplate } from "../types";
import { DiscussionRefPicker } from "./DiscussionRefPicker";
import { IconMessage, IconX } from "./Icons";

interface CommentModalProps {
  quote: string;
  agentName?: string;
  discussions?: Discussion[];
  promptTemplates?: PromptTemplate[];
  onClose: () => void;
  onSubmit: (
    body: string,
    discussionIds: string[],
    promptTemplateId: string,
  ) => Promise<void>;
}

export function CommentModal({
  quote,
  agentName = "Code Agent",
  discussions = [],
  promptTemplates = [],
  onClose,
  onSubmit,
}: CommentModalProps) {
  const { t } = useTranslation();
  const [body, setBody] = useState("");
  const [discussionIds, setDiscussionIds] = useState<string[]>([]);
  const [promptTemplateId, setPromptTemplateId] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (!promptTemplateId && promptTemplates.length > 0) {
      const preferred =
        promptTemplates.find((item) => item.allowDocumentEdit) ??
        promptTemplates[0];
      setPromptTemplateId(preferred?.id ?? "");
    }
  }, [promptTemplateId, promptTemplates]);

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
      setError(t("请输入评论内容"));
      return;
    }
    setBusy(true);
    setError(null);
    try {
      await onSubmit(body.trim(), discussionIds, promptTemplateId);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("添加评论失败"));
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
      <div className="modal comment-modal" role="dialog" aria-modal="true" aria-label={t("添加评论")}>
        <div className="modal-head">
          <div className="modal-title">
            <IconMessage size={17} />
            {t("添加评论")}
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
        <form onSubmit={submit}>
          <div className="comment-quote">
            <span>{t("选中内容")}</span>
            <blockquote>{quote}</blockquote>
          </div>
          <label className="modal-label" htmlFor="comment-body">
            {t("评论内容")}
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
            placeholder={t("写下你的意见，{{agent}} 可以通过 MCP 读取并回复…", { agent: agentName })}
            rows={5}
          />
          {promptTemplates.length > 0 ? (
            <label className="comment-template-field">
              <span>{t("任务类型")}</span>
              <select
                value={promptTemplateId}
                onChange={(event) => setPromptTemplateId(event.target.value)}
              >
                {promptTemplates.map((item) => (
                  <option key={item.id} value={item.id}>
                    {t(item.name)}
                    {item.allowDocumentEdit ? "" : t("（只分析）")}
                  </option>
                ))}
              </select>
            </label>
          ) : null}
          <DiscussionRefPicker
            discussions={discussions}
            selected={discussionIds}
            onChange={setDiscussionIds}
          />
          {error ? <p className="modal-error">{error}</p> : null}
          <div className="modal-actions">
            <button type="button" className="btn ghost" onClick={onClose}>
              {t("取消")}
            </button>
            <button type="submit" className="btn primary" disabled={busy}>
              {busy ? t("添加中…") : t("添加评论")}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
