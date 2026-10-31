import { QueryClient, queryOptions } from "@tanstack/react-query";
import {
  fetchAgentSessions,
  fetchAvailableDiscussionSessions,
  fetchCodeFile,
  fetchCodeFiles,
  fetchCodeRepos,
  fetchComments,
  fetchConfig,
  fetchDocCodeRepos,
  fetchDocDiscussions,
  fetchPromptTemplates,
  fetchTree,
} from "./api";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Data stays stale so opening a document always revalidates it (other
      // clients may have changed it); cached data renders meanwhile. Pushes
      // over WebSocket invalidate explicitly, so focus refetches and retries
      // only add load.
      staleTime: 0,
      refetchOnWindowFocus: false,
      retry: false,
    },
  },
});

type QueryRoot =
  | "codeFile"
  | "codeFiles"
  | "comments"
  | "config"
  | "codeRepos"
  | "docCodeRepos"
  | "discussions"
  | "discussionSessions"
  | "promptTemplates"
  | "agentSessions";

/** Marks every query under `root` stale (all documents) and refetches the active ones. */
export function invalidate(root: QueryRoot, options?: { cancelRefetch?: boolean }) {
  return queryClient.invalidateQueries({ queryKey: [root] }, options);
}

export const configQuery = queryOptions({
  queryKey: ["config"],
  queryFn: fetchConfig,
  staleTime: Infinity,
});

// Refreshed imperatively (debounced watcher events, after file operations), so
// observers only read the cache; see refreshTree in App.
export const treeQuery = queryOptions({
  queryKey: ["tree"],
  queryFn: fetchTree,
  enabled: false,
});

export const commentsQuery = (path: string) =>
  queryOptions({
    queryKey: ["comments", path],
    queryFn: () => fetchComments(path),
    enabled: path !== "",
  });

export const codeReposQuery = queryOptions({
  queryKey: ["codeRepos"],
  queryFn: fetchCodeRepos,
});

export const docCodeReposQuery = (path: string) =>
  queryOptions({
    queryKey: ["docCodeRepos", path],
    queryFn: () => fetchDocCodeRepos(path),
    enabled: path !== "",
  });

export const discussionsQuery = (path: string) =>
  queryOptions({
    queryKey: ["discussions", path],
    queryFn: () => fetchDocDiscussions(path),
    enabled: path !== "",
  });

// Built-in templates are localized by Accept-Language, so the UI language is
// part of the key: a cached entry from another language must never reach the
// editor.
export const promptTemplatesQuery = (language: string) =>
  queryOptions({
    queryKey: ["promptTemplates", language],
    queryFn: () => fetchPromptTemplates(language),
  });

export const agentSessionsQuery = (path: string) =>
  queryOptions({
    queryKey: ["agentSessions", path],
    queryFn: () => fetchAgentSessions(path),
    enabled: path !== "",
  });

// The branch is part of both keys so a branch switch never shows the other
// branch's cached files.
export const codeFilesQuery = (repoId: string, branch: string) =>
  queryOptions({
    queryKey: ["codeFiles", repoId, branch],
    queryFn: () => fetchCodeFiles(repoId, "", 5000),
    enabled: repoId !== "",
  });

export const codeFileQuery = (repoId: string, branch: string, path: string) =>
  queryOptions({
    queryKey: ["codeFile", repoId, branch, path],
    queryFn: () => fetchCodeFile(repoId, path),
    enabled: repoId !== "" && path !== "",
  });

export const discussionSessionsQuery = (path: string) =>
  queryOptions({
    queryKey: ["discussionSessions", path],
    queryFn: () => fetchAvailableDiscussionSessions(path),
    enabled: path !== "",
  });
