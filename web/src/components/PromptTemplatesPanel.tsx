import { useCallback, useEffect, useState } from "react";
import {
  createPromptTemplate,
  deletePromptTemplate,
  fetchPromptTemplates,
  resetPromptTemplate,
  updatePromptTemplate,
} from "../api";
import type { PromptTemplate } from "../types";
import { IconPlus, IconRefresh, IconTrash } from "./Icons";

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
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
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
  }, [selected]);

  const newTemplate = () => {
    setSelected(null);
    setName("自定义任务");
    setDescription("");
    setBody("");
    setAllowDocumentEdit(false);
    setError("");
  };

  const save = async () => {
    setSaving(true);
    setError("");
    try {
      const payload = { name, description, body, allowDocumentEdit };
      const saved = selected
        ? await updatePromptTemplate({ ...payload, id: selected.id })
        : await createPromptTemplate(payload);
      await load();
      setSelected(saved);
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
        评论等底层约束仍由 mdlive 强制附加。
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
            <span>模板内容</span>
            <textarea
              className="prompt-template-textarea"
              value={body}
              onChange={(event) => setBody(event.target.value)}
              placeholder="描述这个任务要做什么、输出什么、不要做什么…"
              rows={9}
            />
          </label>
          <label className="settings-auto-run prompt-template-allow">
            <input
              type="checkbox"
              checked={allowDocumentEdit}
              onChange={(event) => setAllowDocumentEdit(event.target.checked)}
            />
            <span>
              允许 Agent 修改文档
              <em>关闭后模板会要求 Agent 只回复分析，不调用 write_file；服务端强制权限暂未实现</em>
            </span>
          </label>
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
