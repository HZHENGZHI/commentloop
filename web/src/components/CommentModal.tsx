import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import type { Discussion, PromptTemplate } from "../types";
import { DiscussionRefPicker } from "./DiscussionRefPicker";
import { Button, Input, Select } from "antd";
import { IconMessage } from "./Icons";
import { AppModal } from "./AppModal";

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

  useEffect(() => {
    if (!promptTemplateId && promptTemplates.length > 0) {
      const preferred =
        promptTemplates.find((item) => item.allowDocumentEdit) ??
        promptTemplates[0];
      setPromptTemplateId(preferred?.id ?? "");
    }
  }, [promptTemplateId, promptTemplates]);

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
    <AppModal
      title={
        <span className="app-modal-title">
          <IconMessage size={17} />
          {t("添加评论")}
        </span>
      }
      width={560}
      onClose={onClose}
      className="comment-modal"
    >
      <form onSubmit={submit}>
        <div className="comment-quote">
          <span>{t("选中内容")}</span>
          <blockquote>{quote}</blockquote>
        </div>
        <label className="modal-label" htmlFor="comment-body">
          {t("评论内容")}
        </label>
        <Input.TextArea
          id="comment-body"
          autoFocus
          value={body}
          onChange={(event) => {
            setBody(event.target.value);
            setError(null);
          }}
          placeholder={t("写下你的意见，{{agent}} 可以通过 MCP 读取并回复…", {
            agent: agentName,
          })}
          rows={5}
        />
        {promptTemplates.length > 0 ? (
          <label className="comment-template-field">
            <span>{t("任务类型")}</span>
            <Select
              value={promptTemplateId}
              onChange={setPromptTemplateId}
              options={promptTemplates.map((item) => ({
                value: item.id,
                label: `${t(item.name)}${item.allowDocumentEdit ? "" : t("（只分析）")}`,
              }))}
            />
          </label>
        ) : null}
        <DiscussionRefPicker
          discussions={discussions}
          selected={discussionIds}
          onChange={setDiscussionIds}
        />
        {error ? <p className="modal-error">{error}</p> : null}
        <div className="modal-actions">
          <Button onClick={onClose}>{t("取消")}</Button>
          <Button type="primary" htmlType="submit" loading={busy}>
            {busy ? t("添加中…") : t("添加评论")}
          </Button>
        </div>
      </form>
    </AppModal>
  );
}
