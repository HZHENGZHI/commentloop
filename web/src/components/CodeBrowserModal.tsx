import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Button, Empty, Input, Select, Tag } from "antd";
import {
  fetchCodeRepoBranches,
  lspDefinition,
  switchCodeRepoBranch,
} from "../api";
import { codeFileQuery, codeFilesQuery } from "../queries";
import type {
  CodeFileEntry,
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
} from "./Icons";
import { CodeViewer } from "./CodeViewer";
import { AppModal } from "./AppModal";

const NO_FILES: CodeFileEntry[] = [];

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
  const { t } = useTranslation();
  const [repoID, setRepoID] = useState(() => repos[0]?.id ?? "");
  const [filter, setFilter] = useState("");
  const [selectedFile, setSelectedFile] = useState("");
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
  const filesResult = useQuery(codeFilesQuery(repo?.id ?? "", repo?.branch ?? ""));
  const fileListMeta = filesResult.data ?? null;
  const files = fileListMeta?.files ?? NO_FILES;
  const filesLoading = filesResult.isLoading;
  const fileResult = useQuery(
    codeFileQuery(repo?.id ?? "", repo?.branch ?? "", selectedFile),
  );
  const content = fileResult.data?.content ?? "";
  const codeLoading = fileResult.isLoading;

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
    setSelectedFile("");
    setTarget(null);
    setPending(null);
    setMessage("");
    setFilter("");
    setCursor(null);
    setExpandedDirs(new Set());
    setBranchOpen(false);
  }, [repoID, repo?.branch]);

  useEffect(() => {
    if (!filesResult.data) return;
    const next = filesResult.data.files ?? [];
    setSelectedFile((current) =>
      next.some((file) => file.path === current) ? current : preferredFile(next),
    );
  }, [filesResult.data]);

  useEffect(() => {
    setCursor(null);
  }, [repo?.id, selectedFile]);

  const loadError = filesResult.error ?? fileResult.error;
  useEffect(() => {
    if (!loadError) return;
    setMessage(
      loadError.message ||
        (filesResult.error ? t("读取代码文件列表失败") : t("读取代码文件失败")),
    );
  }, [loadError]);

  useEffect(() => {
    // Wait for the revalidation too: cached text may predate the file on disk.
    if (!pending || pending.path !== selectedFile || !fileResult.data || fileResult.isFetching) return;
    setTarget({
      line: pending.line,
      character: pending.character,
      nonce: Date.now(),
    });
    setMessage(t("已跳转到 {{path}}:{{line}}", { path: pending.path, line: pending.line }));
    setPending(null);
  }, [pending, selectedFile, fileResult.data, fileResult.isFetching]);

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
      setBranchError(err instanceof Error ? err.message : t("读取分支失败"));
    } finally {
      setBranchLoading(false);
    }
  };

  const switchBranch = async (branch: string) => {
    if (!repo || branch === repo.branch || branchBusy) return;
    if (
      repo.kind !== "github" &&
      !window.confirm(
        t("切换本地仓库分支会更新工作区文件为「{{branch}}」。\n\n本地仓库有未提交改动时会被拒绝。继续？", { branch }),
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
      setTarget(null);
      setPending(null);
      setMessage(t("已切换到分支 {{branch}}", { branch }));
      onBranchChanged();
    } catch (err) {
      setBranchError(err instanceof Error ? err.message : t("切换分支失败"));
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
        t("定义在代码仓库外：{{path}}:{{line}}:{{character}}", { path: location.path, line: location.line, character: location.character }),
      );
      return;
    }
    if (location.path === selectedFile) {
      setTarget({
        line: location.line,
        character: location.character,
        nonce: Date.now(),
      });
      setMessage(t("已跳转到 {{path}}:{{line}}", { path: location.path, line: location.line }));
      return;
    }
    setPending(location);
    setSelectedFile(location.path);
    setMessage(t("正在打开 {{path}}:{{line}}", { path: location.path, line: location.line }));
  };

  const definition = async (line: number, character: number) => {
    if (!repo || !selectedFile || lspBusy) return;
    setLspBusy(true);
    setMessage(t("正在查询定义…"));
    try {
      const result = await lspDefinition(
        repo.id,
        selectedFile,
        line,
        character,
      );
      const location = result.locations?.[0];
      if (!location) {
        setMessage(t("没有找到定义；请确认 LSP 已完成索引后重试"));
        return;
      }
      openLocation(location);
      if ((result.locations?.length ?? 0) > 1) {
        setMessage(
          t("找到 {{count}} 个定义，已打开第一个 · {{path}}:{{line}}", { count: result.locations.length, path: location.path, line: location.line }),
        );
      }
    } catch (err) {
      setMessage(err instanceof Error ? err.message : t("LSP 查询失败"));
    } finally {
      setLspBusy(false);
    }
  };

  return (
    <AppModal
      title={
        <span className="app-modal-title code-browser-title">
          <IconGitBranch size={16} />
          {t("代码浏览")}
          <span>{fileName(docPath)}</span>
        </span>
      }
      width="92vw"
      onClose={onClose}
      className="code-browser-modal"
    >
      <div className="code-browser-head">
        {repos.length > 0 ? (
          <>
            <Select
              className="code-browser-repo"
              size="small"
              value={repo?.id ?? ""}
              onChange={(value) => setRepoID(value)}
              aria-label={t("选择代码仓库")}
              options={repos.map((item) => ({
                value: item.id,
                label: `${item.name}${item.branch ? ` · ${item.branch}` : ""}${
                  item.dirty ? ` · ${t("有改动")}` : ""
                }`,
              }))}
            />
            {repo && repo.kind === "local" && !repo.branch ? (
              <Tag
                className="code-browser-no-branch"
                title={t("这个本地目录不是 Git 仓库，无法切换分支；请关联仓库根目录或初始化 Git")}
              >
                {t("非 Git 仓库")}
              </Tag>
            ) : null}
            {repo && (repo.kind === "github" || repo.branch) ? (
              <div className="code-browser-branch-wrap">
                <Button
                  type="text"
                  size="small"
                  className={`code-browser-branch${
                    branchOpen ? " active" : ""
                  }`}
                  onClick={() => void openBranchMenu()}
                  title={t("切换分支")}
                  icon={<IconGitBranch size={11} />}
                >
                  {repo.branch || repo.remoteBranch || t("分支")}
                  <IconChevronDown size={10} />
                </Button>
                {branchOpen ? (
                  <div className="code-browser-branch-menu">
                    <div className="code-browser-branch-head">{t("切换分支")}</div>
                    {branchLoading ? (
                      <div className="code-browser-branch-empty">{t("正在读取分支…")}</div>
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
        <Button
          type="text"
          size="small"
          className="code-browser-manage"
          onClick={onManageRepos}
        >
          {t("管理仓库")}
        </Button>
      </div>

      <div
        className="code-browser-body"
        style={{ height: "max(160px, calc(min(860px, 90vh) - 150px))" }}
      >
        <aside className="code-browser-tree">
          <div className="code-browser-filter">
            <Input
              size="small"
              allowClear
              value={filter}
              onChange={(event) => setFilter(event.target.value)}
              placeholder={t("筛选文件…")}
            />
          </div>
          <div className="code-browser-files">
            {repos.length === 0 ? (
              <Empty
                className="code-browser-empty"
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={t("还没有关联代码仓库")}
              >
                <Button type="primary" onClick={onManageRepos}>
                  {t("关联代码仓库")}
                </Button>
              </Empty>
            ) : filesLoading ? (
              <div className="code-browser-empty">{t("正在读取文件…")}</div>
            ) : visibleFiles.length === 0 ? (
              <Empty
                className="code-browser-empty"
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={
                  files.length === 0 ? t("仓库中没有源码文件") : t("没有匹配的文件")
                }
              />
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
                  setPending(null);
                  setMessage("");
                }}
              />
            )}
          </div>
          {fileListMeta?.truncated ? (
            <div className="code-browser-truncated">
              {t("文件列表已截断（共 {{count}} 个匹配）", { count: fileListMeta.total })}
            </div>
          ) : null}
        </aside>

        <section className="code-browser-main">
          <div className="code-browser-file-head">
            <span className="code-browser-path">
              {selectedFile || t("选择一个代码文件")}
            </span>
            <div className="code-browser-lsp-actions">
              <span className="code-browser-lsp-hint">
                {lspBusy ? t("LSP 查询中…") : t("⌘/Ctrl + 点击代码符号")}
              </span>
              <Button
                size="small"
                icon={<IconLocate size={11} />}
                disabled={!cursor || lspBusy || !selectedFile}
                onClick={() => {
                  if (cursor) {
                    void definition(cursor.line, cursor.character);
                  }
                }}
                title={t("跳转到光标位置的定义")}
              >
                {t("跳转定义")}
              </Button>
            </div>
          </div>
          {codeLoading ? (
            <div className="code-browser-code-empty">{t("正在读取代码…")}</div>
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
                ? t("关联代码仓库后可以浏览源码")
                : t("从左侧选择文件查看代码")}
            </div>
          )}
        </section>
      </div>
      <div className={`code-browser-status${message ? " visible" : ""}`}>
        {message || t("代码只读；不会修改仓库文件")}
      </div>
    </AppModal>
  );
}
