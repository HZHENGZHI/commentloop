import {
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type ReactNode,
} from "react";
import { useTranslation } from "react-i18next";
import type { FileEntry, RootInfo, TreeResponse } from "../types";
import {
  IconChevronDown,
  IconChevronRight,
  IconFileText,
  IconFolder,
  IconFolderOpen,
  IconFolderPlus,
  IconPlus,
  IconSearch,
  IconServer,
  IconTrash,
  IconUpload,
  IconX,
} from "./Icons";

interface DirNode {
  name: string;
  path: string;
  display: string;
  dirs: Map<string, DirNode>;
  files: FileEntry[];
}

interface SidebarProps {
  open: boolean;
  tree: TreeResponse | null;
  currentPath: string | null;
  dirty: boolean;
  removingRootId: string | null;
  removingRootName: string;
  onOpen: (path: string) => void;
  onDelete: (path: string) => void;
  onNew: () => void;
  onImport: (files: FileList) => void;
  onAddRoot: () => void;
  onRemoveRoot: (root: RootInfo) => void;
}

function buildTree(files: FileEntry[], rootId: string): DirNode {
  const root: DirNode = {
    name: "",
    path: "",
    display: "",
    dirs: new Map(),
    files: [],
  };
  for (const file of files) {
    const segments = file.relPath.split("/");
    let node = root;
    for (let i = 0; i < segments.length - 1; i += 1) {
      const segment = segments[i]!;
      const display = segments.slice(0, i + 1).join("/");
      let child = node.dirs.get(segment);
      if (!child) {
        child = {
          name: segment,
          path: `${rootId}/${display}`,
          display,
          dirs: new Map(),
          files: [],
        };
        node.dirs.set(segment, child);
      }
      node = child;
    }
    node.files.push(file);
  }
  return root;
}

function displayFile(file: FileEntry): string {
  return `${file.rootName}/${file.relPath}`;
}

export function Sidebar({
  open,
  tree,
  currentPath,
  dirty,
  removingRootId,
  removingRootName,
  onOpen,
  onDelete,
  onNew,
  onImport,
  onAddRoot,
  onRemoveRoot,
}: SidebarProps) {
  const { t, i18n } = useTranslation();
  const [query, setQuery] = useState("");
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());
  const searchRef = useRef<HTMLInputElement>(null);
  const importRef = useRef<HTMLInputElement>(null);

  const roots = tree?.roots ?? [];
  const modKey = /Mac|iPhone|iPad/i.test(navigator.platform) ? "⌘K" : "Ctrl K";
  const normalizedQuery = query.trim().toLowerCase();
  const filtered = useMemo(() => {
    if (!normalizedQuery) return [];
    return (tree?.files ?? []).filter((file) =>
      displayFile(file).toLowerCase().includes(normalizedQuery),
    );
  }, [tree, normalizedQuery]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setQuery("");
        searchRef.current?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const toggleDir = (path: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  };

  const fileRow = (file: FileEntry, depth: number) => {
    const active = file.path === currentPath;
    return (
      <div
        key={file.path}
        className={`tree-row file-row${active ? " active" : ""}`}
        style={{ paddingLeft: 10 + depth * 14 }}
        role="button"
        tabIndex={0}
        title={displayFile(file)}
        onClick={() => onOpen(file.path)}
        onKeyDown={(event) => {
          if (event.target !== event.currentTarget) return;
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            onOpen(file.path);
          }
        }}
      >
        <span className="tree-icon file-icon">
          <IconFileText size={14} />
        </span>
        <span className="tree-name">{file.name}</span>
        {active && dirty ? <span className="dirty-dot" title={t("未保存")} /> : null}
        <button
          type="button"
          className="row-action"
          title={t("删除 {{name}}", { name: file.name })}
          aria-label={t("删除 {{name}}", { name: file.name })}
          onClick={(event) => {
            event.stopPropagation();
            if (
              window.confirm(
                t("确定删除 “{{name}}”？此操作不可撤销。", {
                  name: displayFile(file),
                }),
              )
            ) {
              onDelete(file.path);
            }
          }}
        >
          <IconTrash size={13} />
        </button>
      </div>
    );
  };

  const renderDir = (node: DirNode, depth: number): ReactNode => {
    const folders = [...node.dirs.values()].sort((a, b) =>
      a.name.localeCompare(b.name, i18n.language),
    );
    const files = [...node.files].sort((a, b) =>
      a.name.localeCompare(b.name, i18n.language),
    );
    return (
      <>
        {folders.map((folder) => {
          const isCollapsed = collapsed.has(folder.path);
          return (
            <div key={folder.path}>
              <div
                className="tree-row folder-row"
                style={{ paddingLeft: 8 + depth * 14 }}
                role="button"
                tabIndex={0}
                title={folder.display}
                onClick={() => toggleDir(folder.path)}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    toggleDir(folder.path);
                  }
                }}
              >
                <span className="chevron">
                  {isCollapsed ? (
                    <IconChevronRight size={12} />
                  ) : (
                    <IconChevronDown size={12} />
                  )}
                </span>
                <span className="tree-icon folder-icon">
                  {isCollapsed ? (
                    <IconFolder size={14} />
                  ) : (
                    <IconFolderOpen size={14} />
                  )}
                </span>
                <span className="tree-name">{folder.name}</span>
              </div>
              {!isCollapsed ? renderDir(folder, depth + 1) : null}
            </div>
          );
        })}
        {files.map((file) => fileRow(file, depth))}
      </>
    );
  };

  const renderRoot = (root: RootInfo) => {
    const key = `root:${root.id}`;
    const isCollapsed = collapsed.has(key);
    const removing = removingRootId === root.id;
    const node = buildTree(root.files, root.id);
    return (
      <div
        className={`root-group${removing ? " removing" : ""}`}
        key={root.id}
        aria-busy={removing}
      >
        <div
          className="tree-row root-row"
          style={{ paddingLeft: 7 }}
          role="button"
          tabIndex={0}
          title={
            root.kind === "remote" ? `${root.remote}:${root.path}` : root.path
          }
          onClick={() => {
            if (!removing) toggleDir(key);
          }}
          onKeyDown={(event) => {
            if (removing || event.target !== event.currentTarget) return;
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              toggleDir(key);
            }
          }}
        >
          <span className="chevron">
            {isCollapsed ? (
              <IconChevronRight size={12} />
            ) : (
              <IconChevronDown size={12} />
            )}
          </span>
          <span className="tree-icon root-icon">
            {root.kind === "remote" ? (
              <IconServer size={14} />
            ) : isCollapsed ? (
              <IconFolder size={14} />
            ) : (
              <IconFolderOpen size={14} />
            )}
          </span>
          <span className="tree-name">{root.name}</span>
          {root.kind === "remote" ? (
            <span className="root-remote">{t("远程")}</span>
          ) : root.primary ? (
            <span className="root-primary">{t("主")}</span>
          ) : null}
          {root.offline ? (
            <span className="root-offline">{t("离线")}</span>
          ) : null}
          {removing ? (
            <span className="root-removing-label">
              <span className="spinner" />
              {t("移除中")}
            </span>
          ) : root.offline ? null : root.kind === "remote" && root.loading ? (
            <span className="root-loading-count">
              <span className="spinner" />
              {t("读取中")}
            </span>
          ) : (
            <span className="root-count">{root.files.length}</span>
          )}
          {!removing ? (
            <button
              type="button"
              className="row-action root-remove"
              title={t("移除目录 {{name}}（不会删除磁盘文件）", { name: root.name })}
              aria-label={t("移除目录 {{name}}（不会删除磁盘文件）", { name: root.name })}
              onClick={(event) => {
                event.stopPropagation();
                onRemoveRoot(root);
              }}
            >
              <IconX size={13} />
            </button>
          ) : null}
        </div>
        {!isCollapsed ? (
          <>
            {renderDir(node, 1)}
            {root.offline ? (
              <p className="root-empty-hint root-offline-hint">
                {t("连接不可用：{{error}}", { error: root.offline })}
              </p>
            ) : root.kind === "remote" && root.loading ? (
              <p className="root-empty-hint root-loading-hint">
                <span className="spinner" />
                {t("正在读取远程文件…")}
              </p>
            ) : root.files.length === 0 ? (
              <p className="root-empty-hint">{t("此目录没有 Markdown 文件")}</p>
            ) : null}
          </>
        ) : null}
      </div>
    );
  };

  const noRoots = roots.length === 0;

  return (
    <aside className={`sidebar${open ? " open" : ""}`}>
      <div className="sidebar-head">
        <div className="sidebar-root">
          <span className="root-badge">MD</span>
          <span
            className="root-name"
            title={roots
              .map((root) =>
                root.kind === "remote"
                  ? `${root.remote}:${root.path}`
                  : root.path,
              )
              .join("\n")}
          >
            {roots.length ? t("{{count}} 个目录", { count: roots.length }) : "Commentloop"}
          </span>
        </div>
        <div className="sidebar-tools">
          <button
            type="button"
            className="icon-btn small"
            title={t("新建文档")}
            aria-label={t("新建文档")}
            onClick={onNew}
          >
            <IconPlus size={15} />
          </button>
          <button
            type="button"
            className="icon-btn small"
            title={t("导入 Markdown 文件")}
            aria-label={t("导入 Markdown 文件")}
            onClick={() => importRef.current?.click()}
          >
            <IconUpload size={15} />
          </button>
          <input
            ref={importRef}
            type="file"
            accept=".md,.markdown,.mdown,.mkd,text/markdown"
            multiple
            hidden
            onChange={(event: ChangeEvent<HTMLInputElement>) => {
              if (event.target.files?.length) {
                onImport(event.target.files);
              }
              event.target.value = "";
            }}
          />
          <button
            type="button"
            className="icon-btn small"
            title={t("添加目录")}
            aria-label={t("添加目录")}
            onClick={onAddRoot}
          >
            <IconFolderPlus size={15} />
          </button>
        </div>
      </div>

      <div className="sidebar-search">
        <IconSearch size={14} />
        <input
          ref={searchRef}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={t("搜索文件")}
          aria-label={t("搜索文件")}
          spellCheck={false}
        />
        {query ? (
          <button
            type="button"
            className="search-clear"
            onClick={() => setQuery("")}
            aria-label={t("清空搜索")}
          >
            ×
          </button>
        ) : (
          <kbd>{modKey}</kbd>
        )}
      </div>

      <div className="tree-scroll">
        {normalizedQuery ? (
          filtered.length ? (
            <div className="flat-results">
              {filtered.map((file) => {
                const location = [file.rootName, file.dir]
                  .filter(Boolean)
                  .join("/");
                return (
                  <div
                    key={file.path}
                    className={`tree-row file-row${file.path === currentPath ? " active" : ""}`}
                    style={{ paddingLeft: 10 }}
                    role="button"
                    tabIndex={0}
                    title={displayFile(file)}
                    onClick={() => onOpen(file.path)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter") onOpen(file.path);
                    }}
                  >
                    <span className="tree-icon file-icon">
                      <IconFileText size={14} />
                    </span>
                    <span className="tree-name">
                      {file.name}
                      <span className="tree-subdir">{location}</span>
                    </span>
                  </div>
                );
              })}
            </div>
          ) : (
            <p className="tree-empty">{t("没有匹配的文件")}</p>
          )
        ) : noRoots ? (
          <div className="tree-empty-block">
            <p>{t("还没有添加目录")}</p>
            <button type="button" className="btn ghost" onClick={onAddRoot}>
              {t("添加目录")}
            </button>
          </div>
        ) : (
          <div className="file-tree">{roots.map(renderRoot)}</div>
        )}
      </div>

      {removingRootId ? (
        <div className="sidebar-removing" role="status" aria-live="polite">
          <span className="spinner" />
          <span>
            {t("正在移除目录")}
            {removingRootName ? ` “${removingRootName}”` : ""}…
          </span>
        </div>
      ) : null}

      <div className="sidebar-foot">
        <span>
          {t("{{count}} 个目录", { count: roots.length })} ·{" "}
          {t("{{count}} 个文档", { count: tree?.files.length ?? 0 })}
        </span>
        {tree?.truncated ? <span className="truncated">{t("已截断")}</span> : null}
        <span
          className="root-path"
          title={roots
            .map((root) =>
              root.kind === "remote"
                ? `${root.remote}:${root.path}`
                : root.path,
            )
            .join("\n")}
        >
          {roots.length
            ? roots.length > 1
              ? `${roots[0]?.path ?? ""}  +${roots.length - 1}`
              : roots[0]?.path
            : ""}
        </span>
      </div>
    </aside>
  );
}
