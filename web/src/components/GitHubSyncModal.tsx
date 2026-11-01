import { useEffect, useState } from "react";
import { Alert, Button, Input, Popconfirm } from "antd";
import { useTranslation } from "react-i18next";
import {
  clearGitHubSyncToken,
  fetchGitHubSyncConfig,
  saveGitHubSyncConfig,
  syncGitHubReview,
  testGitHubSync,
} from "../api";
import type {
  Comment,
  DocReview,
  GitHubReviewSync,
  GitHubSyncConfig,
  GitHubSyncTest,
} from "../types";
import { buildGitHubMarkdown } from "../githubMarkdown";
import { IconGitBranch, IconLink } from "./Icons";
import { AppModal } from "./AppModal";

interface GitHubSyncModalProps {
  path: string;
  review?: DocReview;
  comments: Comment[];
  onClose: () => void;
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function GitHubSyncModal({
  path,
  review,
  comments,
  onClose,
}: GitHubSyncModalProps) {
  const { t } = useTranslation();
  const [config, setConfig] = useState<GitHubSyncConfig | null>(null);
  const [repo, setRepo] = useState("");
  const [issue, setIssue] = useState("");
  const [filePath, setFilePath] = useState("");
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [tested, setTested] = useState<GitHubSyncTest | null>(null);
  const [synced, setSynced] = useState<GitHubReviewSync | null>(null);

  useEffect(() => {
    fetchGitHubSyncConfig()
      .then((value) => {
        setConfig(value);
        setRepo(value.repo);
        setIssue(value.issue > 0 ? String(value.issue) : "");
        setFilePath(value.filePath);
      })
      .catch((err: unknown) => setError(errorText(err)));
  }, []);

  const markdown = () => buildGitHubMarkdown(path, review, comments);

  const save = async () => {
    const number = Number(issue);
    if (!repo.trim() || !Number.isInteger(number) || number <= 0) {
      setError(t("请填写 owner/repo 和有效的 Issue/PR 编号"));
      return;
    }
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const value = await saveGitHubSyncConfig(
        repo.trim(),
        number,
        filePath.trim(),
        token.trim() || undefined,
      );
      setConfig(value);
      setToken("");
      setNotice(t("GitHub 同步配置已保存"));
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };

  const test = async () => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      setTested(await testGitHubSync());
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };

  const sync = async () => {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      setSynced(await syncGitHubReview(path));
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(markdown());
      setNotice(t("已复制 GitHub 评论 Markdown"));
    } catch (err) {
      setError(t("复制 GitHub 评论失败：{{error}}", { error: errorText(err) }));
    }
  };

  const clearToken = async () => {
    const number = Number(issue);
    if (!repo.trim() || !Number.isInteger(number) || number <= 0) return;
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const value = await clearGitHubSyncToken(repo.trim(), number);
      setConfig(value);
      setToken("");
      setNotice(t("GitHub token 已清除"));
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  };

  const ready = Boolean(config?.configured && config?.tokenSet);
  return (
    <AppModal
      title={
        <span className="app-modal-title">
          <IconGitBranch size={17} />
          {t("GitHub 同步")}
        </span>
      }
      width={640}
      onClose={onClose}
      className="github-sync-modal"
      footer={
        <div className="github-sync-footer">
          <Button onClick={onClose}>{t("取消")}</Button>
          <Button type="primary" loading={busy} onClick={() => void save()}>
            {t("保存配置")}
          </Button>
        </div>
      }
    >
      <p className="modal-hint">
        {t("评论会作为一条 GitHub Issue/PR 评论发送；token 保存在 macOS 钥匙串，不会写入数据库。")}
      </p>
      <label className="modal-label" htmlFor="github-repo">
        {t("仓库")}
      </label>
      <Input
        id="github-repo"
        value={repo}
        onChange={(event) => setRepo(event.target.value)}
        placeholder="owner/repo"
      />
      <label className="modal-label" htmlFor="github-issue">
        {t("Issue / PR 编号")}
      </label>
      <Input
        id="github-issue"
        value={issue}
        onChange={(event) => setIssue(event.target.value.replace(/[^0-9]/g, ""))}
        placeholder="123"
      />
      <label className="modal-label" htmlFor="github-file">
        {t("PR 文件路径（可选）")}
      </label>
      <Input
        id="github-file"
        value={filePath}
        onChange={(event) => setFilePath(event.target.value)}
        placeholder="docs/plan.md"
      />
      <p className="modal-hint">
        {t("配置仓库内文件路径后优先创建 PR inline review；为空或失败会回退为 Issue 评论。")}
      </p>
      <label className="modal-label" htmlFor="github-token">
        {t("Fine-grained PAT")}
      </label>
      <Input.Password
        id="github-token"
        value={token}
        onChange={(event) => setToken(event.target.value)}
        placeholder={
          config?.tokenSet
            ? t("已保存，留空保持不变")
            : t("仅需目标仓库的 Issues 写权限")
        }
        autoComplete="off"
      />
      <p className="modal-hint">
        {t("读取时使用 /repos/{repo}/issues/{number}；写入时创建 issue comment。")}
      </p>
      {error ? <Alert type="error" showIcon message={error} /> : null}
      {notice ? <Alert type="success" showIcon message={notice} /> : null}
      {tested ? (
        <Alert
          type="success"
          showIcon
          message={t("连接成功：{{title}}", { title: tested.title })}
          description={
            <a href={tested.url} target="_blank" rel="noreferrer">
              <IconLink size={11} /> {tested.url}
            </a>
          }
        />
      ) : null}
      {synced ? (
        <Alert
          type={synced.warning ? "warning" : "success"}
          showIcon
          message={
            synced.mode === "review"
              ? t("已创建 GitHub inline review（{{count}} 条）", {
                  count: synced.inline,
                })
              : t("已同步为 GitHub Issue 评论")
          }
          description={
            <>
              <a href={synced.url} target="_blank" rel="noreferrer">
                <IconLink size={11} /> {synced.url}
              </a>
              {synced.warning ? <p>{synced.warning}</p> : null}
            </>
          }
        />
      ) : null}
      {config?.tokenSet ? (
        <Popconfirm
          title={t("清除已保存的 GitHub token？")}
          onConfirm={() => void clearToken()}
        >
          <Button danger size="small" disabled={busy}>
            {t("清除 token")}
          </Button>
        </Popconfirm>
      ) : null}
      <div className="github-sync-actions">
        <Button disabled={!ready || busy} onClick={() => void test()}>
          {t("测试连接")}
        </Button>
        <Button disabled={!ready || busy} onClick={() => void copy()}>
          {t("复制 Markdown")}
        </Button>
        <Button type="primary" disabled={!ready || busy} onClick={() => void sync()}>
          {t("同步当前评论")}
        </Button>
      </div>
    </AppModal>
  );
}
