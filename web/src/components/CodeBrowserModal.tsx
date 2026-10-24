import { useEffect, useMemo, useState } from "react";
import {
  fetchCodeFile,
  fetchCodeFiles,
  fetchCodeRepoBranches,
  lspDefinition,
  switchCodeRepoBranch,
} from "../api";
import type {
  CodeFileEntry,
  CodeFileList,
  CodeRepo,
  LSPLocation,
  Theme,
} from "../types";
import {
  IconChevronDown,
  IconChevronRight,
  IconFileText,
  IconFolder,
  IconFolderOpen,
  IconGitBranch,
  IconLocate,
  IconX,
} from "./Icons";
import { CodeViewer } from "./CodeViewer";

interface CodeBrowserModalProps {
  docPath: string;
  repos: CodeRepo[];
  theme: Theme;
  onClose: () => void;
  onManageRepos: () => void;
  onBranchChanged: () => void;
}

interface CodeTreeNode {
  name: string;
  path: string;
  dirs: Map<string, CodeTreeNode>;
  files: CodeFileEntry[];
}

function buildCodeTree(files: CodeFileEntry[]): CodeTreeNode {
  const root: CodeTreeNode = {
    name: "",
    path: "",
    dirs: new Map(),
    files: [],
  };
  for (const file of files) {
    const segments = file.path.split("/").filter(Boolean);
    if (segments.length === 0) continue;
    let node = root;
    for (let i = 0; i < segments.length - 1; i += 1) {
      const segment = segments[i]!;
      const path = segments.slice(0, i + 1).join("/");
      let child = node.dirs.get(segment);
      if (!child) {
        child = { name: segment, path, dirs: new Map(), files: [] };
        node.dirs.set(segment, child);
      }
      node = child;
    }
    node.files.push(file);
  }
  return root;
}

function ancestors(path: string): string[] {
  const segments = path.split("/").filter(Boolean);
  const out: string[] = [];
  for (let i = 0; i < segments.length - 1; i += 1) {
    out.push(segments.slice(0, i + 1).join("/"));
  }
  return out;
}

interface CodeTreeProps {
  node: CodeTreeNode;
  depth: number;
  expanded: Set<string>;
  forceExpanded: boolean;
  selectedFile: string;
  onToggle: (path: string) => void;
  onSelect: (path: string) => void;
}

function CodeTree({
  node,
  depth,
  expanded,
  forceExpanded,
  selectedFile,
  onToggle,
  onSelect,
}: CodeTreeProps) {
  const dirs = [...node.dirs.values()].sort((a, b) =>
    a.name.localeCompare(b.name),
  );
  const files = [...node.files].sort((a, b) =>
    fileName(a.path).localeCompare(fileName(b.path)),
  );
  return (
    <>
      {dirs.map((dir) => {
        const open = forceExpanded || expanded.has(dir.path);
        return (
          <div className="code-tree-dir" key={dir.path}>
            <button
              type="button"
              className="code-browser-dir"
              style={{ paddingLeft: `${5 + depth * 13}px` }}
              onClick={() => onToggle(dir.path)}
              title={dir.path}
            >
              {open ? (
                <IconChevronDown size={11} />
              ) : (
                <IconChevronRight size={11} />
              )}
              {open ? (
                <IconFolderOpen size={13} />
              ) : (
                <IconFolder size={13} />
              )}
              <span>{dir.name}</span>
            </button>
            {open ? (
              <CodeTree
                node={dir}
                depth={depth + 1}
                expanded={expanded}
                forceExpanded={forceExpanded}
                selectedFile={selectedFile}
                onToggle={onToggle}
                onSelect={onSelect}
              />
            ) : null}
          </div>
        );
      })}
      {files.map((file) => (
        <button
          key={file.path}
          type="button"
          className={`code-browser-file${
            file.path === selectedFile ? " active" : ""
          }`}
          style={{ paddingLeft: `${5 + depth * 13 + 14}px` }}
          onClick={() => onSelect(file.path)}
          title={file.path}
        >
          <IconFileText size={12} />
          <span className="code-browser-file-name">
            {fileName(file.path)}
          </span>
        </button>
      ))}
    </>
  );
}

function isCodeFile(path: string): boolean {
  return /\.(go|ts|tsx|js|jsx|mjs|cjs|py|rs|java|kt|c|h|cc|cpp|hpp|cs|rb|php|swift|scala|sh|bash|zsh|sql|proto|graphql|gql|json|ya?ml|toml|ini|conf|xml|html|css|scss|less|vue|svelte|md)$/i.test(
    path,
  );
}

function preferredFile(files: CodeFileEntry[]): string {
  const code = files.find((file) => isCodeFile(file.path));
  const readme = files.find((file) => /(^|\/)readme\.md$/i.test(file.path));
  return (code ?? readme ?? files[0])?.path ?? "";
}

function fileName(path: string): string {
  const index = path.lastIndexOf("/");
  return index < 0 ? path : path.slice(index + 1);
}

export function CodeBrowserModal({
  docPath,
  repos,
  theme,
  onClose,
  onManageRepos,
  onBranchChanged,
}: CodeBrowserModalProps) {
  const [repoID, setRepoID] = useState(() => repos[0]?.id ?? "");
  const [files, setFiles] = useState<CodeFileEntry[]>([]);
  const [fileListMeta, setFileListMeta] = useState<CodeFileList | null>(null);
  const [filesLoading, setFilesLoading] = useState(false);
  const [filter, setFilter] = useState("");
  const [selectedFile, setSelectedFile] = useState("");
  const [content, setContent] = useState("");
  const [codeLoading, setCodeLoading] = useState(false);
  const [expandedDirs, setExpandedDirs] = useState<Set<string>>(
    () => new Set(),
  );
  const [target, setTarget] = useState<{
    line: number;
    character: number;
    nonce: number;
  } | null>(null);
  const [pending, setPending] = useState<LSPLocation | null>(null);
  const [message, setMessage] = useState("");
  const [lspBusy, setLspBusy] = useState(false);
  const [cursor, setCursor] = useState<{ line: number; character: number } | null>(null);
  const [branchOpen, setBranchOpen] = useState(false);
  const [branches, setBranches] = useState<string[]>([]);
  const [branchLoading, setBranchLoading] = useState(false);
  const [branchBusy, setBranchBusy] = useState(false);
  const [branchError, setBranchError] = useState("");

  const repo = repos.find((item) => item.id === repoID) ?? repos[0];

  useEffect(() => {
    if (repos.length === 0) {
      setRepoID("");
      return;
    }
    if (!repos.some((item) => item.id === repoID)) {
      setRepoID(repos[0]!.id);
    }
  }, [repos, repoID]);

  useEffect(() => {
    setFiles([]);
    setFileListMeta(null);
    setSelectedFile("");
    setContent("");
    setTarget(null);
    setPending(null);
    setMessage("");
    setFilter("");
    setCursor(null);
    setExpandedDirs(new Set());
    setBranchOpen(false);
  }, [repoID, repo?.branch]);

  useEffect(() => {
    if (!repo) return;
    let alive = true;
    setFilesLoading(true);
    setMessage("");
    fetchCodeFiles(repo.id, "", 5000)
      .then((result) => {
        if (!alive) return;
        setFiles(result.files ?? []);
        setFileListMeta(result);
        setSelectedFile((current) =>
          result.files.some((file) => file.path === current)
            ? current
            : preferredFile(result.files ?? []),
        );
      })
      .catch((err) => {
        if (!alive) return;
        setMessage(
          err instanceof Error ? err.message : "读取代码文件列表失败",
        );
      })
      .finally(() => {
        if (alive) setFilesLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [repo?.id, repo?.branch]);

  useEffect(() => {
    if (!repo || !selectedFile) {
      setContent("");
      return;
    }
    let alive = true;
    setCodeLoading(true);
    setPending(null);
    setCursor(null);
    fetchCodeFile(repo.id, selectedFile)
      .then((result) => {
        if (alive) setContent(result.content ?? "");
      })
      .catch((err) => {
        if (!alive) return;
        setContent("");
        setMessage(err instanceof Error ? err.message : "读取代码文件失败");
      })
      .finally(() => {
        if (alive) setCodeLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [repo?.id, selectedFile]);

  useEffect(() => {
    if (!pending || pending.path !== selectedFile || !content) return;
    setTarget({
      line: pending.line,
      character: pending.character,
      nonce: Date.now(),
    });
    setMessage(`已跳转到 ${pending.path}:${pending.line}`);
    setPending(null);
  }, [pending, selectedFile, content]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !document.querySelector(".code-repo-modal")) {
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const openBranchMenu = async () => {
    if (!repo) return;
    if (repo.kind !== "github" && !repo.branch) return;
    if (branchOpen) {
      setBranchOpen(false);
      return;
    }
    setBranchOpen(true);
    setBranchError("");
    setBranchLoading(true);
    try {
      const result = await fetchCodeRepoBranches(repo.id);
      setBranches(result.branches ?? []);
    } catch (err) {
      setBranchError(err instanceof Error ? err.message : "读取分支失败");
    } finally {
      setBranchLoading(false);
    }
  };

  const switchBranch = async (branch: string) => {
    if (!repo || branch === repo.branch || branchBusy) return;
    if (
      repo.kind !== "github" &&
      !window.confirm(
        `切换本地仓库分支会更新工作区文件为「${branch}」。\n\n本地仓库有未提交改动时会被拒绝。继续？`,
      )
    ) {
      return;
    }
    setBranchBusy(true);
    setBranchError("");
    try {
      await switchCodeRepoBranch(repo.id, branch);
      setBranchOpen(false);
      setSelectedFile("");
      setContent("");
      setTarget(null);
      setPending(null);
      setMessage(`已切换到分支 ${branch}`);
      onBranchChanged();
    } catch (err) {
      setBranchError(err instanceof Error ? err.message : "切换分支失败");
    } finally {
      setBranchBusy(false);
    }
  };

  const visibleFiles = useMemo(() => {
    const query = filter.trim().toLowerCase();
    if (!query) return files;
    return files.filter((file) => file.path.toLowerCase().includes(query));
  }, [files, filter]);

  const forceExpanded = filter.trim() !== "";
  const tree = useMemo(
    () => buildCodeTree(forceExpanded ? visibleFiles : files),
    [files, visibleFiles, forceExpanded],
  );

  useEffect(() => {
    if (!selectedFile) return;
    const paths = ancestors(selectedFile);
    if (paths.length === 0) return;
    setExpandedDirs((current) => {
      let changed = false;
      const next = new Set(current);
      for (const path of paths) {
        if (!next.has(path)) {
          next.add(path);
          changed = true;
        }
      }
      return changed ? next : current;
    });
  }, [selectedFile]);

  const openLocation = (location: LSPLocation) => {
    if (location.external) {
      setMessage(
        `定义在代码仓库外：${location.path}:${location.line}:${location.character}`,
      );
      return;
    }
    if (location.path === selectedFile) {
      setTarget({
        line: location.line,
        character: location.character,
        nonce: Date.now(),
      });
      setMessage(`已跳转到 ${location.path}:${location.line}`);
      return;
    }
    setPending(location);
    setSelectedFile(location.path);
    setMessage(`正在打开 ${location.path}:${location.line}`);
  };

  const definition = async (line: number, character: number) => {
    if (!repo || !selectedFile || lspBusy) return;
    setLspBusy(true);
    setMessage("正在查询定义…");
    try {
      const result = await lspDefinition(
        repo.id,
        selectedFile,
        line,
        character,
      );
      const location = result.locations?.[0];
      if (!location) {
        setMessage("没有找到定义；请确认 LSP 已完成索引后重试");
        return;
      }
      openLocation(location);
      if ((result.locations?.length ?? 0) > 1) {
        setMessage(
          `找到 ${result.locations.length} 个定义，已打开第一个 · ${location.path}:${location.line}`,
        );
      }
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "LSP 查询失败");
    } finally {
      setLspBusy(false);
    }
  };

  return (
    <div
      className="modal-backdrop code-browser-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        className="modal code-browser-modal"
        role="dialog"
        aria-modal="true"
        aria-label="代码浏览"
      >
        <div className="code-browser-head">
          <div className="code-browser-title">
            <IconGitBranch size={16} />
            代码浏览
            <span>{fileName(docPath)}</span>
          </div>
          {repos.length > 0 ? (
            <>
            <select
              className="code-browser-repo"
              value={repo?.id ?? ""}
              onChange={(event) => setRepoID(event.target.value)}
              aria-label="选择代码仓库"
            >
              {repos.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                  {item.branch ? ` · ${item.branch}` : ""}
                  {item.dirty ? " · 有改动" : ""}
                </option>
              ))}
            </select>
            {repo && repo.kind === "local" && !repo.branch ? (
              <span
                className="code-browser-no-branch"
                title="这个本地目录不是 Git 仓库，无法切换分支；请关联仓库根目录或初始化 Git"
              >
                非 Git 仓库
              </span>
            ) : null}
            {repo && (repo.kind === "github" || repo.branch) ? (
              <div className="code-browser-branch-wrap">
                <button
                  type="button"
                  className={`code-browser-branch${branchOpen ? " active" : ""}`}
                  onClick={() => void openBranchMenu()}
                  title="切换分支"
                >
                  <IconGitBranch size={11} />
                  {repo.branch || repo.remoteBranch || "分支"}
                  <IconChevronDown size={10} />
                </button>
                {branchOpen ? (
                  <div className="code-browser-branch-menu">
                    <div className="code-browser-branch-head">切换分支</div>
                    {branchLoading ? (
                      <div className="code-browser-branch-empty">正在读取分支…</div>
                    ) : branchError ? (
                      <div className="code-browser-branch-empty error">{branchError}</div>
                    ) : (
                      <div className="code-browser-branch-list">
                        {branches.map((branch) => (
                          <button
                            key={branch}
                            type="button"
                            className={`code-browser-branch-item${
                              branch === repo.branch ? " active" : ""
                            }`}
                            onClick={() => void switchBranch(branch)}
                            disabled={branchBusy}
                          >
                            {branch}
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                ) : null}
              </div>
            ) : null}
            </>
          ) : null}
          <button
            type="button"
            className="code-browser-manage"
            onClick={onManageRepos}
          >
            管理仓库
          </button>
          <button
            type="button"
            className="icon-btn small"
            onClick={onClose}
            aria-label="关闭代码浏览"
          >
            <IconX size={15} />
          </button>
        </div>

        <div className="code-browser-body">
          <aside className="code-browser-tree">
            <div className="code-browser-filter">
              <input
                value={filter}
                onChange={(event) => setFilter(event.target.value)}
                placeholder="筛选文件…"
              />
            </div>
            <div className="code-browser-files">
              {repos.length === 0 ? (
                <div className="code-browser-empty">
                  <p>还没有关联代码仓库</p>
                  <button type="button" className="btn primary" onClick={onManageRepos}>
                    关联代码仓库
                  </button>
                </div>
              ) : filesLoading ? (
                <div className="code-browser-empty">正在读取文件…</div>
              ) : visibleFiles.length === 0 ? (
                <div className="code-browser-empty">
                  {files.length === 0 ? "仓库中没有源码文件" : "没有匹配的文件"}
                </div>
              ) : (
                <CodeTree
                  node={tree}
                  depth={0}
                  expanded={expandedDirs}
                  forceExpanded={forceExpanded}
                  selectedFile={selectedFile}
                  onToggle={(path) =>
                    setExpandedDirs((current) => {
                      const next = new Set(current);
                      if (next.has(path)) {
                        next.delete(path);
                      } else {
                        next.add(path);
                      }
                      return next;
                    })
                  }
                  onSelect={(path) => {
                    setSelectedFile(path);
                    setMessage("");
                  }}
                />
              )}
            </div>
            {fileListMeta?.truncated ? (
              <div className="code-browser-truncated">
                文件列表已截断（共 {fileListMeta.total} 个匹配）
              </div>
            ) : null}
          </aside>

          <section className="code-browser-main">
            <div className="code-browser-file-head">
              <span className="code-browser-path">
                {selectedFile || "选择一个代码文件"}
              </span>
              <div className="code-browser-lsp-actions">
                <span className="code-browser-lsp-hint">
                  {lspBusy ? "LSP 查询中…" : "⌘/Ctrl + 点击代码符号"}
                </span>
                <button
                  type="button"
                  className="code-browser-jump"
                  disabled={!cursor || lspBusy || !selectedFile}
                  onClick={() => {
                    if (cursor) {
                      void definition(cursor.line, cursor.character);
                    }
                  }}
                  title="跳转到光标位置的定义"
                >
                  <IconLocate size={11} />
                  跳转定义
                </button>
              </div>
            </div>
            {codeLoading ? (
              <div className="code-browser-code-empty">正在读取代码…</div>
            ) : selectedFile ? (
              <CodeViewer
                filename={selectedFile}
                content={content}
                theme={theme}
                target={target}
                onCursorChange={(line, character) =>
                  setCursor({ line, character })
                }
                onDefinition={(line, character) =>
                  void definition(line, character)
                }
              />
            ) : (
              <div className="code-browser-code-empty">
                {repos.length === 0
                  ? "关联代码仓库后可以浏览源码"
                  : "从左侧选择文件查看代码"}
              </div>
            )}
          </section>
        </div>
        <div className={`code-browser-status${message ? " visible" : ""}`}>
          {message || "代码只读；不会修改仓库文件"}
        </div>
      </div>
    </div>
  );
}
