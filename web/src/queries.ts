import { QueryClient, queryOptions } from "@tanstack/react-query";
import {
  fetchAgentSessions,
  fetchCodeRepos,
  fetchComments,
  fetchDocCodeRepos,
  fetchDocDiscussions,
  fetchPromptTemplates,
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
  | "comments"
  | "codeRepos"
  | "docCodeRepos"
  | "discussions"
  | "promptTemplates"
  | "agentSessions";

/** Marks every query under `root` stale (all documents) and refetches the active ones. */
export function invalidate(root: QueryRoot, options?: { cancelRefetch?: boolean }) {
  return queryClient.invalidateQueries({ queryKey: [root] }, options);
}

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
    queryFn: fetchPromptTemplates,
  });

export const agentSessionsQuery = (path: string) =>
  queryOptions({
    queryKey: ["agentSessions", path],
    queryFn: () => fetchAgentSessions(path),
    enabled: path !== "",
  });
