import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Checkbox, Input } from "antd";
import { useTranslation } from "react-i18next";
import {
  createPromptTemplate,
  deletePromptTemplate,
  optimizePromptTemplate,
  resetPromptTemplate,
  updatePromptTemplate,
} from "../api";
import { invalidate, promptTemplatesQuery } from "../queries";
import type { PromptTemplate } from "../types";
import { IconPlus, IconRefresh, IconSparkles, IconTrash } from "./Icons";

const VARIABLES = [
  "doc_path",
  "comment_quote",
  "comment_body",
  "comment_thread",
  "group_comment_ids",
  "consensus",
  "discussion_excerpts",
  "code_repos",
];

const NO_TEMPLATES: PromptTemplate[] = [];

export function PromptTemplatesPanel() {
  const { t, i18n } = useTranslation();
  // Pin the language the panel opened with: switching languages mid-edit must
  // not swap the list (and reset the form) underneath unsaved changes.
  const [language] = useState(i18n.language);
  const query = useQuery(promptTemplatesQuery(language));
  const templates = query.data?.templates ?? NO_TEMPLATES;
  const loading = query.isPending;
  const [selected, setSelected] = useState<PromptTemplate | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [body, setBody] = useState("");
  const [allowDocumentEdit, setAllowDocumentEdit] = useState(true);
  const [allowCodeRead, setAllowCodeRead] = useState(true);
  const [allowCommentReply, setAllowCommentReply] = useState(true);
  const [saving, setSaving] = useState(false);
  const [optimizing, setOptimizing] = useState(false);
  const [optimizedOriginal, setOptimizedOriginal] = useState("");
  const [optimizeNote, setOptimizeNote] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    setSelected((current) =>
      (current && templates.find((item) => item.id === current.id)) ??
      templates[0] ??
      null,
    );
  }, [templates]);

  const load = () => invalidate("promptTemplates");
  // Block writes while the list revalidates so the form cannot save stale text.
  const busy = saving || query.isFetching;
  const loadError = query.error
    ? query.error.message || t("读取提示词模板失败")
    : "";

  useEffect(() => {
    if (!selected) return;
    setName(selected.name);
    setDescription(selected.description);
    setBody(selected.body);
    setAllowDocumentEdit(selected.allowDocumentEdit);
    setAllowCodeRead(selected.allowCodeRead);
    setAllowCommentReply(selected.allowCommentReply);
    setOptimizedOriginal("");
    setOptimizeNote("");
  }, [selected]);

  const newTemplate = () => {
    setSelected(null);
    setName(t("自定义任务"));
    setDescription("");
    setBody("");
    setAllowDocumentEdit(false);
    setAllowCodeRead(true);
    setAllowCommentReply(true);
    setError("");
  };

  const save = async () => {
    setSaving(true);
    setError("");
    try {
      const payload = {
        name,
        description,
        body,
        allowDocumentEdit,
        allowCodeRead,
        allowCommentReply,
      };
      const saved = selected
        ? await updatePromptTemplate({ ...payload, id: selected.id })
        : await createPromptTemplate(payload);
      await load();
      setSelected(saved);
      setOptimizedOriginal("");
      setOptimizeNote("");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("保存提示词模板失败"));
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    if (!selected || selected.builtin) return;
    if (!window.confirm(t("删除模板“{{name}}”？", { name: selected.name }))) return;
    setSaving(true);
    setError("");
    try {
      await deletePromptTemplate(selected.id);
      setSelected(null);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("删除模板失败"));
    } finally {
      setSaving(false);
    }
  };

  const optimize = async () => {
    setOptimizing(true);
    setError("");
    setOptimizeNote("");
    try {
      const result = await optimizePromptTemplate({
        name,
        description,
        body,
        allowDocumentEdit,
        allowCodeRead,
        allowCommentReply,
      });
      setOptimizedOriginal(body);
      setBody(result.body);
      setOptimizeNote(t("AI 已优化，保存模板后生效。"));
    } catch (err) {
      setError(err instanceof Error ? err.message : t("AI 优化失败"));
    } finally {
      setOptimizing(false);
    }
  };

  const undoOptimize = () => {
    setBody(optimizedOriginal);
    setOptimizedOriginal("");
    setOptimizeNote("");
  };

  const reset = async () => {
    if (!selected?.builtin) return;
    if (!window.confirm(t("恢复“{{name}}”的默认内容？", { name: selected.name }))) return;
    setSaving(true);
    setError("");
    try {
      const item = await resetPromptTemplate(selected.id);
      setSelected(item);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("恢复默认失败"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="settings-body prompt-templates-panel">
      <div className="settings-intro">
        {t("为不同场景配置 Agent 的任务模板。模板只描述任务，MCP 工具规则、不能 resolve 评论等底层约束仍由 commentloop 强制附加。")}
      </div>
      <div className="prompt-template-layout">
        <div className="prompt-template-sidebar">
          <Button
            type="primary"
            size="small"
            onClick={newTemplate}
            icon={<IconPlus size={12} />}
          >
            {t("新建模板")}
          </Button>
          <div className="prompt-template-list">
            {loading ? (
              <div className="settings-loading">{t("正在读取…")}</div>
            ) : (
              templates.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  className={`prompt-template-item${selected?.id === item.id ? " active" : ""}`}
                  onClick={() => setSelected(item)}
                >
                  <span>{t(item.name)}</span>
                  <em>
                    {item.builtin ? t("内置") : t("自定义")} ·{" "}
                    {item.allowDocumentEdit ? t("可改文档") : t("只分析")}
                  </em>
                </button>
              ))
            )}
          </div>
        </div>
        <div className="prompt-template-editor">
          <label className="settings-field">
            <span>{t("名称")}</span>
            <Input
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder={t("例如：接口评审")}
            />
          </label>
          <label className="settings-field">
            <span>{t("说明")}</span>
            <Input
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder={t("这个模板适合什么场景")}
            />
          </label>
          <label className="settings-field">
            <span className="prompt-template-label-row">
              {t("模板内容")}
              <Button
                size="small"
                type="text"
                className="prompt-optimize-btn"
                onClick={() => void optimize()}
                disabled={optimizing || saving || (!body.trim() && !description.trim())}
                title={t("让配置的 pi Agent 把草稿优化成更清晰的任务提示词")}
                loading={optimizing}
                icon={<IconSparkles size={12} />}
              >
                {optimizing ? t("AI 优化中…") : t("AI 优化")}
              </Button>
            </span>
            <Input.TextArea
              className="prompt-template-textarea"
              value={body}
              onChange={(event) => setBody(event.target.value)}
              placeholder={t("描述这个任务要做什么、输出什么、不要做什么…")}
              rows={9}
            />
          </label>
          <div className="prompt-template-tools">
            <span className="prompt-template-tools-title">{t("工具权限")}</span>
            <div className="settings-auto-run prompt-template-allow">
              <Checkbox checked disabled />
              <span>{t("读取文档")}<em>{t("必需：文件树、文档、评论和共识")}</em></span>
            </div>
            <div className="settings-auto-run prompt-template-allow">
              <Checkbox
                checked={allowCodeRead}
                onChange={(event) => setAllowCodeRead(event.target.checked)}
              />
              <span>{t("读取代码仓库")}<em>{t("代码列表、源码读取、代码搜索和 Git 信息")}</em></span>
            </div>
            <div className="settings-auto-run prompt-template-allow">
              <Checkbox
                checked={allowCommentReply}
                onChange={(event) => setAllowCommentReply(event.target.checked)}
              />
              <span>{t("回复评论")}<em>{t("通过 reply_comment 把分析或修改结果返回给用户")}</em></span>
            </div>
            <div className="settings-auto-run prompt-template-allow">
              <Checkbox
                checked={allowDocumentEdit}
                onChange={(event) => setAllowDocumentEdit(event.target.checked)}
              />
              <span>{t("修改文档")}<em>{t("关闭后 write_file / 新建 / 删除会被服务端拦截")}</em></span>
            </div>
          </div>
          {optimizeNote ? (
            <div className="prompt-optimize-note">
              <span>{optimizeNote}</span>
              <Button size="small" type="link" onClick={undoOptimize}>
                {t("撤销优化")}
              </Button>
            </div>
          ) : null}
          <p className="settings-note prompt-template-vars">
            {t("可用变量：")}{VARIABLES.map((item) => `{{${item}}}`).join(" ")}
          </p>
          {error || loadError ? <p className="modal-error">{error || loadError}</p> : null}
          <div className="prompt-template-actions">
            {selected?.builtin ? (
              <Button
                size="small"
                onClick={() => void reset()}
                disabled={busy}
                icon={<IconRefresh size={12} />}
              >
                {t("恢复默认")}
              </Button>
            ) : null}
            {selected && !selected.builtin ? (
              <Button
                size="small"
                danger
                onClick={() => void remove()}
                disabled={busy}
                icon={<IconTrash size={12} />}
              >
                {t("删除")}
              </Button>
            ) : null}
            <Button
              type="primary"
              size="small"
              onClick={() => void save()}
              disabled={busy || !name.trim() || !body.trim()}
              loading={saving}
            >
              {saving ? t("保存中…") : t("保存模板")}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
