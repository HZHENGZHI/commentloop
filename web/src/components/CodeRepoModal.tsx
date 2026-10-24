import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import {
  addDocCodeRepo,
  browseDirectories,
  fetchCodeRepos,
  fetchDocCodeRepos,
  removeDocCodeRepo,
} from "../api";
import type { BrowseResponse, CodeRepo } from "../types";
import {
  IconArrowUp,
  IconChevronRight,
  IconFolder,
  IconFolderOpen,
  IconGitBranch,
  IconX,
} from "./Icons";

interface CodeRepoModalProps {
  docPath: string;
  initialBrowsePath: string;
  onClose: () => void;
  onChanged: (message: string) => void;
}

function repoMeta(repo: CodeRepo): string {
  const parts: string[] = [];
  if (repo.branch) parts.push(repo.branch);
  if (repo.head) parts.push(repo.head);
  if (repo.dirty) parts.push("有未提交改动");
  return parts.join(" · ");
}

export function CodeRepoModal({
  docPath,
  initialBrowsePath,
  onClose,
  onChanged,
}: CodeRepoModalProps) {
  const [repos, setRepos] = useState<CodeRepo[]>([]);
  const [linkedIDs, setLinkedIDs] = useState<Set<string>>(() => new Set());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busyID, setBusyID] = useState<string | null>(null);
  const [tab, setTab] = useState<"local" | "github">("local");
  const [pathInput, setPathInput] = useState("");
  const [addBusy, setAddBusy] = useState(false);
  const [githubUrl, setGithubUrl] = useState("");
  const [githubBranch, setGithubBranch] = useState("");
  const [githubBusy, setGithubBusy] = useState(false);
  const [browseOpen, setBrowseOpen] = useState(false);
  const [browse, setBrowse] = useState<BrowseResponse | null>(null);
  const [browseInput, setBrowseInput] = useState(initialBrowsePath);
  const [browseHidden, setBrowseHidden] = useState(false);
  const [browseLoading, setBrowseLoading] = useState(false);
  const [browseError, setBrowseError] = useState<string | null>(null);
  const browseSeq = useRef(0);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [all, linked] = await Promise.all([
        fetchCodeRepos(),
        fetchDocCodeRepos(docPath),
      ]);
      setRepos(all.repos ?? []);
      setLinkedIDs(new Set((linked.repos ?? []).map((repo) => repo.id)));
    } catch (err) {
      setError(err instanceof Error ? err.message : "读取代码仓库失败");
    } finally {
      setLoading(false);
    }
  }, [docPath]);

  useEffect(() => {
    void load();
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [load, onClose]);

  const loadBrowse = useCallback(async (path: string, hidden: boolean) => {
    const id = ++browseSeq.current;
    setBrowseLoading(true);
    setBrowseError(null);
    try {
      const data = await browseDirectories(path, hidden);
      if (id !== browseSeq.current) return;
      setBrowse(data);
      setBrowseInput(data.path);
    } catch (err) {
      if (id !== browseSeq.current) return;
      setBrowseError(err instanceof Error ? err.message : "无法读取目录");
    } finally {
      if (id === browseSeq.current) setBrowseLoading(false);
    }
  }, []);

  const openBrowser = () => {
    const next = !browseOpen;
    setBrowseOpen(next);
    if (next && !browse) void loadBrowse(browseInput || initialBrowsePath, browseHidden);
  };

  const link = async (repo: CodeRepo) => {
    setBusyID(repo.id);
    setError(null);
    try {
      await addDocCodeRepo(docPath, { repoId: repo.id });
      await load();
      onChanged(`已关联代码仓库：${repo.name}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "关联代码仓库失败");
    } finally {
      setBusyID(null);
    }
  };

  const unlink = async (repo: CodeRepo) => {
    setBusyID(repo.id);
    setError(null);
    try {
      await removeDocCodeRepo(docPath, repo.id);
      await load();
      onChanged(`已取消关联：${repo.name}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "取消关联失败");
    } finally {
      setBusyID(null);
    }
  };

  const addPath = async (event: FormEvent) => {
    event.preventDefault();
    const path = pathInput.trim();
    if (!path) {
      setError("请输入代码仓库路径");
      return;
    }
    setAddBusy(true);
    setError(null);
    try {
      const repo = await addDocCodeRepo(docPath, { repoPath: path });
      setPathInput("");
      setBrowseOpen(false);
      await load();
      onChanged(`已关联代码仓库：${repo.name}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "添加代码仓库失败");
    } finally {
      setAddBusy(false);
    }
  };

  const addGitHub = async (event: FormEvent) => {
    event.preventDefault();
    if (!githubUrl.trim()) {
      setError("请输入 GitHub 仓库地址");
      return;
    }
    setGithubBusy(true);
    setError(null);
    try {
      const repo = await addDocCodeRepo(docPath, {
        remoteUrl: githubUrl.trim(),
        remoteBranch: githubBranch.trim(),
      });
      setGithubUrl("");
      setGithubBranch("");
      await load();
      onChanged(`已克隆并关联 GitHub 仓库：${repo.name}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "克隆 GitHub 仓库失败");
    } finally {
      setGithubBusy(false);
    }
  };

  const linked = repos.filter((repo) => linkedIDs.has(repo.id));
  const available = repos.filter((repo) => !linkedIDs.has(repo.id));

  return (
    <div
      className="modal-backdrop comment-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        className="modal code-repo-modal"
        role="dialog"
        aria-modal="true"
        aria-label="关联代码仓库"
      >
        <div className="modal-head">
          <div className="modal-title">
            <IconGitBranch size={17} />
            关联代码仓库
          </div>
          <button
            type="button"
            className="icon-btn small"
            onClick={onClose}
            aria-label="关闭"
          >
            <IconX size={15} />
          </button>
        </div>
        <p className="code-repo-hint">
          关联后，代码 Agent 可以通过 MCP 读取仓库文件、搜索代码和查看 Git
          状态，用来分析并撰写这份文档。代码仓库是只读的。
        </p>

        <div className="root-tabs code-repo-tabs">
          <button
            type="button"
            className={tab === "local" ? "active" : ""}
            onClick={() => setTab("local")}
          >
            <IconFolder size={13} />
            本地目录
          </button>
          <button
            type="button"
            className={tab === "github" ? "active" : ""}
            onClick={() => setTab("github")}
          >
            <IconGitBranch size={13} />
            GitHub 仓库
          </button>
        </div>

        <div className="code-repo-section">
          <div className="code-repo-section-head">
            <span>已关联</span>
            <span className="code-repo-count">{linked.length}</span>
          </div>
          {linked.length === 0 ? (
            <div className="code-repo-empty">还没有关联代码仓库</div>
          ) : (
            <div className="code-repo-list">
              {linked.map((repo) => (
                <div className="code-repo-row linked" key={repo.id}>
                  <IconGitBranch size={14} />
                  <div className="code-repo-main">
                    <div className="code-repo-name">
                      {repo.name}
                      {repoMeta(repo) ? (
                        <span>{repoMeta(repo)}</span>
                      ) : null}
                    </div>
                    <div className="code-repo-path">
                      {repo.kind === "github" ? repo.remoteUrl : repo.path}
                    </div>
                  </div>
                  <button
                    type="button"
                    className="icon-btn small"
                    disabled={busyID === repo.id}
                    onClick={() => void unlink(repo)}
                    title="取消关联"
                    aria-label={`取消关联 ${repo.name}`}
                  >
                    <IconX size={13} />
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>

        {tab === "local" ? (
        <>
        <form className="code-repo-add" onSubmit={addPath}>
          <label className="modal-label" htmlFor="code-repo-path">
            添加代码仓库
          </label>
          <div className="code-repo-path-row">
            <input
              id="code-repo-path"
              value={pathInput}
              onChange={(event) => {
                setPathInput(event.target.value);
                setError(null);
              }}
              placeholder="/path/to/code-repository"
              autoComplete="off"
            />
            <button type="button" className="btn ghost" onClick={openBrowser}>
              <IconFolderOpen size={14} />
              浏览
            </button>
            <button type="submit" className="btn primary" disabled={addBusy}>
              {addBusy ? "关联中…" : "关联"}
            </button>
          </div>
          <p className="code-repo-hint small">
            选择代码仓库根目录；支持 Git 仓库，也支持普通代码目录。
          </p>
        </form>

        {browseOpen ? (
          <div className="code-browse">
            <div className="code-browse-head">
              <button
                type="button"
                className="icon-btn small"
                onClick={() => {
                  if (browse?.parent) void loadBrowse(browse.parent, browseHidden);
                }}
                disabled={!browse?.parent || browseLoading}
                title="上一级"
              >
                <IconArrowUp size={14} />
              </button>
              <form
                className="code-browse-path"
                onSubmit={(event) => {
                  event.preventDefault();
                  if (browseInput.trim()) void loadBrowse(browseInput.trim(), browseHidden);
                }}
              >
                <input
                  value={browseInput}
                  onChange={(event) => setBrowseInput(event.target.value)}
                  autoComplete="off"
                />
              </form>
              <label className="code-browse-hidden">
                <input
                  type="checkbox"
                  checked={browseHidden}
                  onChange={(event) => {
                    setBrowseHidden(event.target.checked);
                    void loadBrowse(browse?.path ?? browseInput, event.target.checked);
                  }}
                />
                显示隐藏
              </label>
            </div>
            {browse?.shortcuts?.length ? (
              <div className="code-browse-shortcuts">
                {browse.shortcuts.map((item) => (
                  <button
                    key={item.path}
                    type="button"
                    onClick={() => void loadBrowse(item.path, browseHidden)}
                  >
                    {item.name}
                  </button>
                ))}
              </div>
            ) : null}
            <div className="code-browse-list">
              {browseLoading ? (
                <div className="code-browse-empty">正在读取…</div>
              ) : browseError ? (
                <div className="code-browse-empty error">{browseError}</div>
              ) : browse?.entries.length ? (
                browse.entries.map((entry) => (
                  <button
                    type="button"
                    key={entry.path}
                    className="code-browse-entry"
                    onClick={() => void loadBrowse(entry.path, browseHidden)}
                  >
                    <IconFolder size={14} />
                    <span>{entry.name}</span>
                    <IconChevronRight size={12} />
                  </button>
                ))
              ) : (
                <div className="code-browse-empty">没有子目录</div>
              )}
            </div>
            <div className="code-browse-actions">
              <button
                type="button"
                className="btn ghost"
                onClick={() => setBrowseOpen(false)}
              >
                关闭浏览
              </button>
              <button
                type="button"
                className="btn primary"
                disabled={!browse?.path}
                onClick={() => {
                  if (!browse?.path) return;
                  setPathInput(browse.path);
                  setBrowseOpen(false);
                }}
              >
                选择此目录
              </button>
            </div>
          </div>
        ) : null}

        </>
        ) : (
          <form className="code-repo-add" onSubmit={addGitHub}>
            <label className="modal-label" htmlFor="github-repo-url">
              GitHub 仓库
            </label>
            <div className="code-repo-path-row">
              <input
                id="github-repo-url"
                value={githubUrl}
                onChange={(event) => {
                  setGithubUrl(event.target.value);
                  setError(null);
                }}
                placeholder="https://github.com/owner/repo 或 git@github.com:owner/repo.git"
                autoComplete="off"
              />
              <input
                className="code-repo-branch-input"
                value={githubBranch}
                onChange={(event) => setGithubBranch(event.target.value)}
                placeholder="分支（可选）"
                autoComplete="off"
              />
              <button type="submit" className="btn primary" disabled={githubBusy}>
                {githubBusy ? "克隆中…" : "克隆并关联"}
              </button>
            </div>
            <p className="code-repo-hint small">
              公开仓库直接填写地址；私有仓库使用 SSH 地址（已配置 SSH key），或在本机设置
              `GITHUB_TOKEN` / `GH_TOKEN`，或先执行 `gh auth login`。克隆为只读缓存，之后可在代码浏览里随时切换分支。
            </p>
          </form>
        )}

        <div className="code-repo-section">
          <div className="code-repo-section-head">
            <span>可用仓库</span>
            <span className="code-repo-count">{available.length}</span>
          </div>
          {loading ? (
            <div className="code-repo-empty">正在读取…</div>
          ) : available.length === 0 ? (
            <div className="code-repo-empty">
              {repos.length === 0 ? "还没有注册代码仓库" : "所有仓库都已关联"}
            </div>
          ) : (
            <div className="code-repo-list">
              {available.map((repo) => (
                <div className="code-repo-row" key={repo.id}>
                  <IconGitBranch size={14} />
                  <div className="code-repo-main">
                    <div className="code-repo-name">
                      {repo.name}
                      {repoMeta(repo) ? (
                        <span>{repoMeta(repo)}</span>
                      ) : null}
                    </div>
                    <div className="code-repo-path">
                      {repo.kind === "github" ? repo.remoteUrl : repo.path}
                    </div>
                  </div>
                  <button
                    type="button"
                    className="btn ghost small-btn"
                    disabled={busyID === repo.id}
                    onClick={() => void link(repo)}
                  >
                    {busyID === repo.id ? "关联中…" : "关联"}
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>

        {error ? <p className="modal-error">{error}</p> : null}
        <div className="modal-actions">
          <button type="button" className="btn ghost" onClick={onClose}>
            关闭
          </button>
        </div>
      </div>
    </div>
  );
}
