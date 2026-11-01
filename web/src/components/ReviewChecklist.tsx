import { useEffect, useState } from "react";
import { Button, Checkbox, Input, Select, Tooltip } from "antd";
import { useTranslation } from "react-i18next";
import {
  addChecklistItem,
  applyChecklist,
  deleteChecklistItem,
  fetchChecklist,
  toggleChecklistItem,
} from "../api";
import type { ChecklistItem, ChecklistPlaybook } from "../types";
import { IconCheck, IconPlus, IconTrash } from "./Icons";

interface ReviewChecklistProps {
  path: string;
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function ReviewChecklist({ path }: ReviewChecklistProps) {
  const { t } = useTranslation();
  const [items, setItems] = useState<ChecklistItem[]>([]);
  const [playbooks, setPlaybooks] = useState<ChecklistPlaybook[]>([]);
  const [playbook, setPlaybook] = useState("");
  const [newTitle, setNewTitle] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const apply = (response: {
    items: ChecklistItem[];
    playbooks: ChecklistPlaybook[];
  }) => {
    setItems(response.items);
    setPlaybooks(response.playbooks);
    setPlaybook((current) => current || response.playbooks[0]?.id || "");
  };

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchChecklist(path)
      .then((response) => {
        if (!cancelled) apply(response);
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

  const run = async (action: () => Promise<{
    items: ChecklistItem[];
    playbooks: ChecklistPlaybook[];
  }>) => {
    setBusy(true);
    setError(null);
    try {
      apply(await action());
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };

  const done = items.filter((item) => item.done).length;
  return (
    <div className="review-checklist">
      <div className="review-checklist-head">
        <span className="review-checklist-title">
          <IconCheck size={12} />
          {t("评审清单")}
        </span>
        <span className="review-checklist-progress">
          {loading ? "" : `${done}/${items.length}`}
          {!loading && items.length > 0 && done === items.length ? (
            <em>{t("全部完成")}</em>
          ) : null}
        </span>
      </div>
      <div className="review-checklist-apply">
        <Select
          size="small"
          value={playbook || undefined}
          onChange={setPlaybook}
          placeholder={t("选择 playbook")}
          options={playbooks.map((item) => ({
            value: item.id,
            label: t(item.name),
          }))}
        />
        <Button
          size="small"
          disabled={!playbook || busy}
          onClick={() => void run(() => applyChecklist(path, playbook))}
        >
          {t("应用")}
        </Button>
      </div>
      {error ? <p className="review-checklist-error">{error}</p> : null}
      {items.length === 0 && !loading ? (
        <p className="review-checklist-empty">
          {t("选择 playbook 或添加检查项")}
        </p>
      ) : (
        <div className="review-checklist-list">
          {items.map((item) => (
            <div key={item.id} className="review-checklist-item">
              <Checkbox
                checked={item.done}
                disabled={busy}
                onChange={(event) =>
                  void run(() =>
                    toggleChecklistItem(path, item.id, event.target.checked),
                  )
                }
              >
                {item.title}
              </Checkbox>
              <Tooltip title={t("删除检查项")}>
                <Button
                  size="small"
                  type="text"
                  className="review-checklist-delete"
                  disabled={busy}
                  icon={<IconTrash size={11} />}
                  onClick={() =>
                    void run(() => deleteChecklistItem(path, item.id))
                  }
                  aria-label={t("删除检查项")}
                />
              </Tooltip>
            </div>
          ))}
        </div>
      )}
      <div className="review-checklist-add">
        <Input
          size="small"
          value={newTitle}
          onChange={(event) => setNewTitle(event.target.value)}
          placeholder={t("添加检查项")}
          onPressEnter={() => {
            const title = newTitle.trim();
            if (!title || busy) return;
            void run(async () => {
              const response = await addChecklistItem(path, title);
              setNewTitle("");
              return response;
            });
          }}
        />
        <Button
          size="small"
          type="text"
          disabled={!newTitle.trim() || busy}
          icon={<IconPlus size={12} />}
          onClick={() => {
            const title = newTitle.trim();
            if (!title || busy) return;
            void run(async () => {
              const response = await addChecklistItem(path, title);
              setNewTitle("");
              return response;
            });
          }}
          aria-label={t("添加检查项")}
        />
      </div>
    </div>
  );
}
