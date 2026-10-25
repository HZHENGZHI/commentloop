import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AgentSession } from "../types";
import { IconCheck, IconChevronDown, IconMessage, IconPlus } from "./Icons";

interface AgentSessionPickerProps {
  sessions: AgentSession[];
  activeId: string;
  busy?: boolean;
  onSwitch: (id: string) => Promise<void>;
  onCreate: () => Promise<void>;
}

function sessionTime(value: string, locale: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString(locale, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function AgentSessionPicker({
  sessions,
  activeId,
  busy = false,
  onSwitch,
  onCreate,
}: AgentSessionPickerProps) {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  const [working, setWorking] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const active = sessions.find((item) => item.id === activeId) ?? sessions[0];

  useEffect(() => {
    setOpen(false);
  }, [activeId]);

  useEffect(() => {
    if (!open) return;
    const onPointer = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    window.addEventListener("mousedown", onPointer);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onPointer);
      window.removeEventListener("keydown", onKey);
    };
  }, [open]);

  const switchTo = async (id: string) => {
    if (id === activeId || working) {
      setOpen(false);
      return;
    }
    setWorking(true);
    try {
      await onSwitch(id);
      setOpen(false);
    } finally {
      setWorking(false);
    }
  };

  const create = async () => {
    if (working) return;
    setWorking(true);
    try {
      await onCreate();
      setOpen(false);
    } finally {
      setWorking(false);
    }
  };

  return (
    <div className="agent-session-picker" ref={rootRef}>
      <button
        type="button"
        className={`agent-session-trigger${open ? " active" : ""}`}
        onClick={() => setOpen((value) => !value)}
        title={t("切换这个文档的 Agent 会话")}
        aria-expanded={open}
      >
        <IconMessage size={10} />
        <span>{active?.title ? t(active.title) : t("会话")}</span>
        <IconChevronDown size={10} />
      </button>
      {open ? (
        <div className="agent-session-menu">
          <div className="agent-session-head">
            <span>{t("Agent 会话")}</span>
            <button
              type="button"
              className="agent-session-new"
              onClick={() => void create()}
              disabled={busy || working}
            >
              <IconPlus size={11} />
              {t("新建")}
            </button>
          </div>
          <div className="agent-session-list">
            {sessions.map((item) => (
              <button
                key={item.id}
                type="button"
                className={`agent-session-item${
                  item.id === activeId ? " active" : ""
                }`}
                onClick={() => void switchTo(item.id)}
                disabled={working}
              >
                <span className="agent-session-title">
                  {t(item.title)}
                  {item.id === activeId ? <IconCheck size={11} /> : null}
                </span>
                <span className="agent-session-time">
                  {sessionTime(item.createdAt, i18n.language)}
                </span>
              </button>
            ))}
          </div>
          <p className="agent-session-hint">
            {t("每个会话独立保存上下文；切换后新的评论任务使用所选会话。")}
          </p>
        </div>
      ) : null}
    </div>
  );
}
