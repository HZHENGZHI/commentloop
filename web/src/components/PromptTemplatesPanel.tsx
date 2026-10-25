import { useCallback, useEffect, useState } from "react";
import {
  createPromptTemplate,
  deletePromptTemplate,
  fetchPromptTemplates,
  optimizePromptTemplate,
  resetPromptTemplate,
  updatePromptTemplate,
} from "../api";
import type { PromptTemplate } from "../types";
import { IconPlus, IconRefresh, IconSparkles, IconTrash } from "./Icons";

interface PromptTemplatesPanelProps {
  onChanged: () => void;
}

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

export function PromptTemplatesPanel({ onChanged }: PromptTemplatesPanelProps) {
  const [templates, setTemplates] = useState<PromptTemplate[]>([]);
  const [selected, setSelected] = useState<PromptTemplate | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [body, setBody] = useState("");
  const [allowDocumentEdit, setAllowDocumentEdit] = useState(true);
  const [allowCodeRead, setAllowCodeRead] = useState(true);
  const [allowCommentReply, setAllowCommentReply] = useState(true);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [optimizing, setOptimizing] = useState(false);
  const [optimizedOriginal, setOptimizedOriginal] = useState("");
  const [optimizeNote, setOptimizeNote] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const result = await fetchPromptTemplates();
      const items = result.templates ?? [];
      setTemplates(items);
      setSelected((current) => {
        const next = current
          ? items.find((item) => item.id === current.id) ?? items[0]
          : items[0] ?? null;
        return next ?? null;
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : "读取提示词模板失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

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
    setName("自定义任务");
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
      onChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存提示词模板失败");
    } finally {
      setSaving(false);
    }
  };

  const remove = async () => {
    if (!selected || selected.builtin) return;
    if (!window.confirm(`删除模板“${selected.name}”？`)) return;
    setSaving(true);
    setError("");
    try {
      await deletePromptTemplate(selected.id);
      setSelected(null);
      await load();
      onChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除模板失败");
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
      setOptimizeNote("AI 已优化，保存模板后生效。");
    } catch (err) {
      setError(err instanceof Error ? err.message : "AI 优化失败");
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
    if (!window.confirm(`恢复“${selected.name}”的默认内容？`)) return;
    setSaving(true);
    setError("");
    try {
      const item = await resetPromptTemplate(selected.id);
      setSelected(item);
      await load();
      onChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : "恢复默认失败");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="settings-body prompt-templates-panel">
      <div className="settings-intro">
        为不同场景配置 Agent 的任务模板。模板只描述任务，MCP 工具规则、不能 resolve
        评论等底层约束仍由 commentloop 强制附加。
      </div>
      <div className="prompt-template-layout">
        <div className="prompt-template-sidebar">
          <button type="button" className="btn primary small-btn" onClick={newTemplate}>
            <IconPlus size={12} /> 新建模板
          </button>
          <div className="prompt-template-list">
            {loading ? (
              <div className="settings-loading">正在读取…</div>
            ) : (
              templates.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  className={`prompt-template-item${selected?.id === item.id ? " active" : ""}`}
                  onClick={() => setSelected(item)}
                >
                  <span>{item.name}</span>
                  <em>
                    {item.builtin ? "内置" : "自定义"} ·{" "}
                    {item.allowDocumentEdit ? "可改文档" : "只分析"}
                  </em>
                </button>
              ))
            )}
          </div>
        </div>
        <div className="prompt-template-editor">
          <label className="settings-field">
            <span>名称</span>
            <input
              className="settings-input"
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="例如：接口评审"
            />
          </label>
          <label className="settings-field">
            <span>说明</span>
            <input
              className="settings-input"
              value={description}
              onChange={(event) => setDescription(event.target.value)}
              placeholder="这个模板适合什么场景"
            />
          </label>
          <label className="settings-field">
            <span className="prompt-template-label-row">
              模板内容
              <button
                type="button"
                className="prompt-optimize-btn"
                onClick={() => void optimize()}
                disabled={optimizing || saving || (!body.trim() && !description.trim())}
                title="让配置的 pi Agent 把草稿优化成更清晰的任务提示词"
              >
                {optimizing ? <span className="spinner" /> : <IconSparkles size={12} />}
                {optimizing ? "AI 优化中…" : "AI 优化"}
              </button>
            </span>
            <textarea
              className="prompt-template-textarea"
              value={body}
              onChange={(event) => setBody(event.target.value)}
              placeholder="描述这个任务要做什么、输出什么、不要做什么…"
              rows={9}
            />
          </label>
          <div className="prompt-template-tools">
            <span className="prompt-template-tools-title">工具权限</span>
            <label className="settings-auto-run prompt-template-allow">
              <input type="checkbox" checked disabled />
              <span>读取文档<em>必需：文件树、文档、评论和共识</em></span>
            </label>
            <label className="settings-auto-run prompt-template-allow">
              <input
                type="checkbox"
                checked={allowCodeRead}
                onChange={(event) => setAllowCodeRead(event.target.checked)}
              />
              <span>读取代码仓库<em>代码列表、源码读取、代码搜索和 Git 信息</em></span>
            </label>
            <label className="settings-auto-run prompt-template-allow">
              <input
                type="checkbox"
                checked={allowCommentReply}
                onChange={(event) => setAllowCommentReply(event.target.checked)}
              />
              <span>回复评论<em>通过 reply_comment 把分析或修改结果返回给用户</em></span>
            </label>
            <label className="settings-auto-run prompt-template-allow">
              <input
                type="checkbox"
                checked={allowDocumentEdit}
                onChange={(event) => setAllowDocumentEdit(event.target.checked)}
              />
              <span>修改文档<em>关闭后 write_file / 新建 / 删除会被服务端拦截</em></span>
            </label>
          </div>
          {optimizeNote ? (
            <div className="prompt-optimize-note">
              <span>{optimizeNote}</span>
              <button type="button" className="settings-text-btn" onClick={undoOptimize}>
                撤销优化
              </button>
            </div>
          ) : null}
          <p className="settings-note prompt-template-vars">
            可用变量：{VARIABLES.map((item) => `{{${item}}}`).join(" ")}
          </p>
          {error ? <p className="modal-error">{error}</p> : null}
          <div className="prompt-template-actions">
            {selected?.builtin ? (
              <button
                type="button"
                className="btn ghost small-btn"
                onClick={() => void reset()}
                disabled={saving}
              >
                <IconRefresh size={12} /> 恢复默认
              </button>
            ) : null}
            {selected && !selected.builtin ? (
              <button
                type="button"
                className="btn ghost small-btn danger-text"
                onClick={() => void remove()}
                disabled={saving}
              >
                <IconTrash size={12} /> 删除
              </button>
            ) : null}
            <button
              type="button"
              className="btn primary small-btn"
              onClick={() => void save()}
              disabled={saving || !name.trim() || !body.trim()}
            >
              {saving ? "保存中…" : "保存模板"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
