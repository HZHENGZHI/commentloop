import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Button, Checkbox, Input, Select, Tabs, Tag, Tooltip } from "antd";
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
import { IconGitBranch, IconMessage, IconServer, IconSettings } from "./Icons";
import { AppModal } from "./AppModal";
import { PromptTemplatesPanel } from "./PromptTemplatesPanel";

interface SettingsModalProps {
  onClose: () => void;
  onSaved: (settings: AppSettings) => void;
}

type SettingsTab = "lsp" | "agent" | "prompts";

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
  const [tab, setTab] = useState<SettingsTab>("agent");
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

  const renderBody = (content: ReactNode) => {
    if (loading) {
      return <div className="settings-loading">{t("正在读取设置…")}</div>;
    }
    if (!settings) {
      return (
        <div className="settings-loading error">
          {error || t("无法读取设置")}
        </div>
      );
    }
    return content;
  };

  const lspPanel = settings ? (
    <div className="settings-body">
      <div className="settings-intro">
        {t("为不同代码文件配置 LSP 服务器。修改后保存会重启语言服务器连接。")}
        <Button
          type="link"
          size="small"
          className="settings-text-btn"
          onClick={() => void resetLSP()}
        >
          {t("恢复默认")}
        </Button>
      </div>
      <div className="settings-lsp-head">
        <span>{t("语言")}</span>
        <span>{t("命令")}</span>
        <span>{t("参数（空格分隔）")}</span>
        <span>{t("状态")}</span>
      </div>
      <div className="settings-lsp-list">
        {settings.lspServers.map((item, index) => {
          const check = checks[item.id];
          const busy = checking === `lsp:${item.id}`;
          return (
            <div className="settings-lsp-row" key={item.id}>
              <Checkbox
                className="settings-lsp-language"
                checked={item.enabled}
                onChange={(event) =>
                  setSettings(
                    setLSP(settings, index, {
                      enabled: event.target.checked,
                    }),
                  )
                }
              >
                <strong>{item.label}</strong>
                <em>{item.extensions.join(" ")}</em>
              </Checkbox>
              <Input
                className="settings-input"
                value={item.command}
                onChange={(event) =>
                  setSettings(
                    setLSP(settings, index, { command: event.target.value }),
                  )
                }
                placeholder={t("命令，例如 gopls")}
              />
              <Input
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
                <Button
                  size="small"
                  className="settings-check-btn"
                  loading={busy}
                  disabled={busy}
                  onClick={() => void checkLSP(item)}
                >
                  {busy ? t("检查中…") : t("检查")}
                </Button>
                {check ? (
                  <Tooltip title={check.path || check.error}>
                    <Tag
                      className="settings-check-state"
                      color={check.found ? "success" : "error"}
                      style={{ maxWidth: "100%", marginInlineEnd: 0 }}
                    >
                      {check.found ? t("已找到") : t("未找到")}
                    </Tag>
                  </Tooltip>
                ) : null}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  ) : null;

  const agentPanel = settings ? (
    <div className="settings-body settings-agent">
      <div className="settings-intro">
        {t("选择你在后台使用的 Code Agent。它通过 MCP 读写文档；这里保存显示名称和可选的启动命令，用于界面标识和命令检查，不会自动启动模型进程。")}
      </div>
      <label className="settings-field">
        <span>{t("Agent 类型")}</span>
        <Select
          value={settings.agent.type}
          style={{ width: "100%" }}
          onChange={(value) => {
            const preset = AGENT_PRESETS.find(
              (item) => item.type === value,
            );
            if (preset) {
              updateAgent({
                type: preset.type,
                name: preset.name,
                command: preset.command,
                autoRun:
                  preset.type === "pi" ||
                  settings.agent.runner === "external"
                    ? settings.agent.autoRun
                    : false,
              });
            } else {
              updateAgent({ type: value });
            }
          }}
          options={AGENT_PRESETS.map((item) => ({
            value: item.type,
            label: `${item.name} · ${item.hint}`,
          }))}
        />
      </label>
      <label className="settings-field">
        <span>{t("界面显示名称")}</span>
        <Input
          className="settings-input"
          value={settings.agent.name}
          onChange={(event) => updateAgent({ name: event.target.value })}
          placeholder="Code Agent"
        />
      </label>
      <label className="settings-field">
        <span>{t("启动命令（可选）")}</span>
        <Input
          className="settings-input"
          value={settings.agent.command}
          onChange={(event) => updateAgent({ command: event.target.value })}
          placeholder="pi / claude / codex"
        />
      </label>
      <label className="settings-field">
        <span>{t("命令参数")}</span>
        <Input
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
        <Select
          value={settings.agent.runner}
          style={{ width: "100%" }}
          onChange={(value) =>
            updateAgent({
              runner: value as "embedded" | "external",
              autoRun:
                value === "external"
                  ? settings.agent.autoRun
                  : settings.agent.type === "pi"
                    ? settings.agent.autoRun
                    : false,
            })
          }
          options={[
            { value: "embedded", label: t("嵌入 Pi RPC（当前进程启动）") },
            { value: "external", label: t("外部 Agent Runner") },
          ]}
        />
      </label>
      <Checkbox
        className="settings-auto-run"
        checked={settings.agent.autoRun}
        disabled={
          settings.agent.type !== "pi" && settings.agent.runner !== "external"
        }
        onChange={(event) => updateAgent({ autoRun: event.target.checked })}
      >
        {t("用户提交评论后自动运行 Agent")}
        <em>
          {settings.agent.runner === "external"
            ? t("由外部 commentloop agent-runner 领取任务；需要先启动 runner")
            : t("嵌入模式目前仅支持 pi Agent；需要已配置 MCP 和模型")}
        </em>
      </Checkbox>
      <div className="settings-agent-check">
        <Button
          size="small"
          className="settings-check-btn"
          loading={checking === "agent"}
          disabled={checking === "agent" || !settings.agent.command}
          onClick={() => void checkAgent()}
        >
          {checking === "agent" ? t("检查中…") : t("检查命令")}
        </Button>
        {agentCheck ? (
          <Tooltip title={agentCheck.path || agentCheck.error}>
            <Tag
              className="settings-check-state"
              color={agentCheck.found ? "success" : "error"}
              style={{ maxWidth: "100%", marginInlineEnd: 0 }}
            >
              {agentCheck.found
                ? t("已找到：{{path}}", { path: agentCheck.path })
                : t("未找到命令")}
            </Tag>
          </Tooltip>
        ) : null}
      </div>
      <p className="settings-note">
        {t("自动运行会复用同一个 pi RPC 进程 / 会话，不会每条评论都新建会话；应用重启、进程异常或修改命令后才会重建。界面中的“Code Agent”名称会使用这里的显示名称。")}
      </p>
    </div>
  ) : null;

  const tabItems = [
    {
      key: "lsp",
      label: (
        <span
          style={{ display: "inline-flex", alignItems: "center", gap: 6 }}
        >
          <IconServer size={14} />
          {t("LSP 服务器")}
        </span>
      ),
      children: renderBody(lspPanel),
    },
    {
      key: "agent",
      label: (
        <span
          style={{ display: "inline-flex", alignItems: "center", gap: 6 }}
        >
          <IconGitBranch size={14} />
          {t("后台 Code Agent")}
        </span>
      ),
      children: renderBody(agentPanel),
    },
    {
      key: "prompts",
      label: (
        <span
          style={{ display: "inline-flex", alignItems: "center", gap: 6 }}
        >
          <IconMessage size={14} />
          {t("提示词")}
        </span>
      ),
      children: renderBody(<PromptTemplatesPanel />),
    },
  ];

  return (
    <AppModal
      title={
        <span className="app-modal-title">
          <IconSettings size={17} />
          {t("设置")}
        </span>
      }
      width={900}
      onClose={onClose}
      className="settings-modal"
      footer={
        <div className="settings-footer">
          {error ? <p className="modal-error">{error}</p> : null}
          <div className="modal-actions settings-actions">
            {tab === "prompts" ? (
              <Button onClick={onClose}>{t("关闭")}</Button>
            ) : (
              <>
                <Button onClick={onClose}>{t("取消")}</Button>
                <Button
                  type="primary"
                  loading={saving}
                  disabled={!settings || saving}
                  onClick={() => void save()}
                >
                  {saving ? t("保存中…") : t("保存设置")}
                </Button>
              </>
            )}
          </div>
        </div>
      }
    >
      <Tabs
        activeKey={tab}
        onChange={(key) => setTab(key as SettingsTab)}
        items={tabItems}
        destroyOnHidden
        classNames={{ header: "settings-tabs" }}
      />
    </AppModal>
  );
}
