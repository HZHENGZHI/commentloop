import { QueryClient, queryOptions } from "@tanstack/react-query";
import {
  fetchAgentSessions,
  fetchComments,
  fetchDocCodeRepos,
  fetchDocDiscussions,
  fetchPromptTemplates,
} from "./api";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // The server pushes changes over WebSocket and handlers invalidate
      // explicitly, so focus refetches and retries only add load.
      staleTime: 30_000,
      refetchOnWindowFocus: false,
      retry: false,
    },
  },
});

type QueryRoot =
  | "comments"
  | "docCodeRepos"
  | "discussions"
  | "promptTemplates"
  | "agentSessions";

/** Refetches every active query under `root` (all documents). */
export function invalidate(root: QueryRoot, options?: { cancelRefetch?: boolean }) {
  return queryClient.invalidateQueries({ queryKey: [root] }, options);
}

export const commentsQuery = (path: string) =>
  queryOptions({
    queryKey: ["comments", path],
    queryFn: () => fetchComments(path),
    enabled: path !== "",
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

export const promptTemplatesQuery = queryOptions({
  queryKey: ["promptTemplates"],
  queryFn: fetchPromptTemplates,
});

export const agentSessionsQuery = (path: string) =>
  queryOptions({
    queryKey: ["agentSessions", path],
    queryFn: () => fetchAgentSessions(path),
    enabled: path !== "",
  });
