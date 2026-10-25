import { useTranslation } from "react-i18next";
import type { Discussion } from "../types";

interface DiscussionRefPickerProps {
  discussions: Discussion[];
  selected: string[];
  onChange: (ids: string[]) => void;
}

export function DiscussionRefPicker({
  discussions,
  selected,
  onChange,
}: DiscussionRefPickerProps) {
  const { t } = useTranslation();
  if (discussions.length === 0) return null;
  const toggle = (id: string) => {
    onChange(
      selected.includes(id)
        ? selected.filter((item) => item !== id)
        : [...selected, id],
    );
  };
  return (
    <details className="discussion-ref-picker">
      <summary>
        {t("参考讨论")}
        <span>
          {selected.length > 0
            ? t("已选 {{count}} 个", { count: selected.length })
            : t("可选，帮助 Agent 遵循之前的决定")}
        </span>
      </summary>
      <div className="discussion-ref-list">
        {discussions.map((item) => (
          <label key={item.id} className="discussion-ref-item">
            <input
              type="checkbox"
              checked={selected.includes(item.id)}
              onChange={() => toggle(item.id)}
            />
            <span className="discussion-ref-main">
              <span className="discussion-ref-title">{item.title}</span>
              <span className="discussion-ref-meta">
                {t("{{count}} 条消息", { count: item.messageCount })} ·{" "}
                {item.sourceKind}
              </span>
            </span>
          </label>
        ))}
      </div>
    </details>
  );
}
