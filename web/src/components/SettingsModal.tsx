import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  checkCommand,
  fetchDefaultSettings,
  fetchSettings,
  saveSettings,
} from "../api";
import type {
  AppSettings,
  CheckCommandResult,
  CodeAgentSetting,
  LSPServerSetting,
} from "../types";
import { IconGitBranch, IconMessage, IconServer, IconSettings, IconX } from "./Icons";
import { PromptTemplatesPanel } from "./PromptTemplatesPanel";

interface SettingsModalProps {
  onClose: () => void;
  onSaved: (settings: AppSettings) => void;
}

const AGENT_PRESETS: {
  type: string;
  name: string;
  command: string;
  hint: string;
}[] = [
  { type: "pi", name: "pi Agent", command: "pi", hint: "pi coding agent" },
  {
    type: "claude-code",
    name: "Claude Code",
    command: "claude",
    hint: "Anthropic Claude Code",
  },
  {
    type: "codex",
    name: "OpenAI Codex",
    command: "codex",
    hint: "OpenAI Codex CLI",
  },
  {
    type: "cursor",
    name: "Cursor Agent",
    command: "cursor-agent",
    hint: "Cursor Agent CLI",
  },
  { type: "custom", name: "Code Agent", command: "", hint: "custom agent" },
];

function argsText(values?: string[]): string {
  return (values ?? []).join(" ");
}

function parseArgs(value: string): string[] {
  return value
    .split(/\s+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function setLSP(
  settings: AppSettings,
  index: number,
  patch: Partial<LSPServerSetting>,
): AppSettings {
  return {
    ...settings,
    lspServers: settings.lspServers.map((item, i) =>
      i === index ? { ...item, ...patch } : item,
    ),
  };
}

export function SettingsModal({ onClose, onSaved }: SettingsModalProps) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<"lsp" | "agent" | "prompts">("lsp");
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [checks, setChecks] = useState<Record<string, CheckCommandResult>>({});
  const [agentCheck, setAgentCheck] = useState<CheckCommandResult | null>(null);
  const [checking, setChecking] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    fetchSettings()
      .then((result) => {
        if (alive) setSettings(result);
      })
      .catch((err) => {
        if (alive) setError(err instanceof Error ? err.message : t("读取设置失败"));
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
    // Load once when the modal opens. The parent passes a new onClose closure
    // on every render; depending on it would refetch and overwrite unsaved
    // edits whenever a toast or WebSocket update re-renders the app.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const updateAgent = (patch: Partial<CodeAgentSetting>) => {
    setSettings((current) =>
      current
        ? { ...current, agent: { ...current.agent, ...patch } }
        : current,
    );
    setAgentCheck(null);
  };

  const checkLSP = async (item: LSPServerSetting) => {
    setChecking(`lsp:${item.id}`);
    try {
      const result = await checkCommand(item.command);
      setChecks((current) => ({ ...current, [item.id]: result }));
    } catch (err) {
      setChecks((current) => ({
        ...current,
        [item.id]: {
          found: false,
          error: err instanceof Error ? err.message : t("检查失败"),
        },
      }));
    } finally {
      setChecking(null);
    }
  };

  const checkAgent = async () => {
    if (!settings) return;
    setChecking("agent");
    try {
      const result = await checkCommand(settings.agent.command);
      setAgentCheck(result);
    } catch (err) {
      setAgentCheck({
        found: false,
        error: err instanceof Error ? err.message : t("检查失败"),
      });
    } finally {
      setChecking(null);
    }
  };

  const resetLSP = async () => {
    try {
      const defaults = await fetchDefaultSettings();
      setSettings((current) =>
        current ? { ...current, lspServers: defaults.lspServers } : current,
      );
      setChecks({});
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("读取默认配置失败"));
    }
  };

  const save = async () => {
    if (!settings) return;
    setSaving(true);
    setError(null);
    try {
      const result = await saveSettings(settings);
      onSaved(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("保存设置失败"));
      setSaving(false);
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
        className="modal settings-modal"
        role="dialog"
        aria-modal="true"
        aria-label={t("设置")}
      >
        <div className="modal-head">
          <div className="modal-title">
            <IconSettings size={17} />
            {t("设置")}
          </div>
          <button
            type="button"
            className="icon-btn small"
            onClick={onClose}
            aria-label={t("关闭设置")}
          >
            <IconX size={15} />
          </button>
        </div>

        <div className="settings-tabs">
          <button
            type="button"
            className={tab === "lsp" ? "active" : ""}
            onClick={() => setTab("lsp")}
          >
            <IconServer size={14} />
            {t("LSP 服务器")}
          </button>
          <button
            type="button"
            className={tab === "agent" ? "active" : ""}
            onClick={() => setTab("agent")}
          >
            <IconGitBranch size={14} />
            {t("后台 Code Agent")}
          </button>
          <button
            type="button"
            className={tab === "prompts" ? "active" : ""}
            onClick={() => setTab("prompts")}
          >
            <IconMessage size={14} />
            {t("提示词")}
          </button>
        </div>

        {loading ? (
          <div className="settings-loading">{t("正在读取设置…")}</div>
        ) : !settings ? (
          <div className="settings-loading error">{error || t("无法读取设置")}</div>
        ) : tab === "lsp" ? (
          <div className="settings-body">
            <div className="settings-intro">
              {t("为不同代码文件配置 LSP 服务器。修改后保存会重启语言服务器连接。")}
              <button type="button" className="settings-text-btn" onClick={() => void resetLSP()}>
                {t("恢复默认")}
              </button>
            </div>
            <div className="settings-lsp-head">
              <span>{t("语言")}</span>
              <span>{t("命令")}</span>
              <span>{t("参数（空格分隔）")}</span>
              <span>{t("状态")}</span>
            </div>
            <div className="settings-lsp-list">
              {settings.lspServers.map((item, index) => (
                <div className="settings-lsp-row" key={item.id}>
                  <label className="settings-lsp-language">
                    <input
                      type="checkbox"
                      checked={item.enabled}
                      onChange={(event) =>
                        setSettings(
                          setLSP(settings, index, {
                            enabled: event.target.checked,
                          }),
                        )
                      }
                    />
                    <span>
                      <strong>{item.label}</strong>
                      <em>{item.extensions.join(" ")}</em>
                    </span>
                  </label>
                  <input
                    className="settings-input"
                    value={item.command}
                    onChange={(event) =>
                      setSettings(
                        setLSP(settings, index, { command: event.target.value }),
                      )
                    }
                    placeholder={t("命令，例如 gopls")}
                  />
                  <input
                    className="settings-input"
                    value={argsText(item.args)}
                    onChange={(event) =>
                      setSettings(
                        setLSP(settings, index, {
                          args: parseArgs(event.target.value),
                        }),
                      )
                    }
                    placeholder={t("例如 --stdio")}
                  />
                  <div className="settings-check-cell">
                    <button
                      type="button"
                      className="settings-check-btn"
                      disabled={checking === `lsp:${item.id}`}
                      onClick={() => void checkLSP(item)}
                    >
                      {checking === `lsp:${item.id}` ? t("检查中…") : t("检查")}
                    </button>
                    {checks[item.id] ? (
                      <span
                        className={`settings-check-state${
                          checks[item.id].found ? " ok" : " bad"
                        }`}
                        title={checks[item.id].path || checks[item.id].error}
                      >
                        {checks[item.id].found ? t("已找到") : t("未找到")}
                      </span>
                    ) : null}
                  </div>
                </div>
              ))}
            </div>
          </div>
        ) : tab === "prompts" ? (
          <PromptTemplatesPanel />
        ) : (
          <div className="settings-body settings-agent">
            <div className="settings-intro">
              {t("选择你在后台使用的 Code Agent。它通过 MCP 读写文档；这里保存显示名称和可选的启动命令，用于界面标识和命令检查，不会自动启动模型进程。")}
            </div>
            <label className="settings-field">
              <span>{t("Agent 类型")}</span>
              <select
                value={settings.agent.type}
                onChange={(event) => {
                  const preset = AGENT_PRESETS.find(
                    (item) => item.type === event.target.value,
                  );
                  if (preset) {
                    updateAgent({
                      type: preset.type,
                      name: preset.name,
                      command: preset.command,
                      autoRun:
                        preset.type === "pi" || settings.agent.runner === "external"
                          ? settings.agent.autoRun
                          : false,
                    });
                  } else {
                    updateAgent({ type: event.target.value });
                  }
                }}
              >
                {AGENT_PRESETS.map((item) => (
                  <option key={item.type} value={item.type}>
                    {item.name} · {item.hint}
                  </option>
                ))}
              </select>
            </label>
            <label className="settings-field">
              <span>{t("界面显示名称")}</span>
              <input
                className="settings-input"
                value={settings.agent.name}
                onChange={(event) => updateAgent({ name: event.target.value })}
                placeholder="Code Agent"
              />
            </label>
            <label className="settings-field">
              <span>{t("启动命令（可选）")}</span>
              <input
                className="settings-input"
                value={settings.agent.command}
                onChange={(event) => updateAgent({ command: event.target.value })}
                placeholder="pi / claude / codex"
              />
            </label>
            <label className="settings-field">
              <span>{t("命令参数")}</span>
              <input
                className="settings-input"
                value={argsText(settings.agent.args)}
                onChange={(event) =>
                  updateAgent({ args: parseArgs(event.target.value) })
                }
                placeholder={t("参数用空格分隔")}
              />
            </label>
            <label className="settings-field">
              <span>{t("自动运行方式")}</span>
              <select
                value={settings.agent.runner}
                onChange={(event) =>
                  updateAgent({
                    runner: event.target.value as "embedded" | "external",
                    autoRun:
                      event.target.value === "external"
                        ? settings.agent.autoRun
                        : settings.agent.type === "pi"
                          ? settings.agent.autoRun
                          : false,
                  })
                }
              >
                <option value="embedded">{t("嵌入 Pi RPC（当前进程启动）")}</option>
                <option value="external">{t("外部 Agent Runner")}</option>
              </select>
            </label>
            <label className="settings-auto-run">
              <input
                type="checkbox"
                checked={settings.agent.autoRun}
                disabled={settings.agent.type !== "pi" && settings.agent.runner !== "external"}
                onChange={(event) =>
                  updateAgent({ autoRun: event.target.checked })
                }
              />
              <span>
                {t("用户提交评论后自动运行 Agent")}
                <em>
                  {settings.agent.runner === "external"
                    ? t("由外部 commentloop agent-runner 领取任务；需要先启动 runner")
                    : t("嵌入模式目前仅支持 pi Agent；需要已配置 MCP 和模型")}
                </em>
              </span>
            </label>
            <div className="settings-agent-check">
              <button
                type="button"
                className="settings-check-btn"
                disabled={checking === "agent" || !settings.agent.command}
                onClick={() => void checkAgent()}
              >
                {checking === "agent" ? t("检查中…") : t("检查命令")}
              </button>
              {agentCheck ? (
                <span
                  className={`settings-check-state${
                    agentCheck.found ? " ok" : " bad"
                  }`}
                  title={agentCheck.path || agentCheck.error}
                >
                  {agentCheck.found ? t("已找到：{{path}}", { path: agentCheck.path }) : t("未找到命令")}
                </span>
              ) : null}
            </div>
            <p className="settings-note">
              {t("自动运行会复用同一个 pi RPC 进程 / 会话，不会每条评论都新建会话；应用重启、进程异常或修改命令后才会重建。界面中的“Code Agent”名称会使用这里的显示名称。")}
            </p>
          </div>
        )}

        {error ? <p className="modal-error">{error}</p> : null}
        <div className="modal-actions settings-actions">
          {tab === "prompts" ? (
            <button type="button" className="btn ghost" onClick={onClose}>
              {t("关闭")}
            </button>
          ) : (
            <>
              <button type="button" className="btn ghost" onClick={onClose}>
                {t("取消")}
              </button>
              <button
                type="button"
                className="btn primary"
                disabled={!settings || saving}
                onClick={() => void save()}
              >
                {saving ? t("保存中…") : t("保存设置")}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}
