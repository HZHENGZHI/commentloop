import i18n from "./i18n";
import type {
  AddRemoteRootRequest,
  AddRootResponse,
  BrowseResponse,
  Comment,
  AgentSession,
  AgentSessionResponse,
  AppSettings,
  CheckCommandResult,
  CodeRepo,
  CodeRepoListResponse,
  CodeFileList,
  CodeReadResult,
  ChecklistResponse,
  Consensus,
  DetectedAgent,
  DiscussionListResponse,
  DiscussionSession,
  DocCodeRepoResponse,
  DocReview,
  DocVersion,
  DocVersionDetail,
  GitHubSyncConfig,
  GitHubSyncResult,
  GitHubSyncTest,
  ImportDiscussionResponse,
  InboxResponse,
  LSPLocation,
  PromptTemplate,
  PromptTemplateInput,
  RemoteSessionRequest,
  RemoteSessionResponse,
  ConfigResponse,
  DocResponse,
  RenderResponse,
  ReviewStatus,
  SearchResponse,
  TreeResponse,
} from "./types";

/** Error thrown for non-2xx API responses; `code` is the stable server error code. */
export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

// The server localizes `error` from Accept-Language; this only guards against
// mixed-language messages leaking into the English UI.
function localizeAPIError(message: string, status: number): string {
  if (i18n.language !== "en") return message;
  return /[\u4e00-\u9fff]/.test(message) ? `Request failed (${status})` : message;
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      "Accept-Language": i18n.language,
      ...(init?.headers ?? {}),
    },
  });
  if (!res.ok) {
    let message = `Request failed (${res.status})`;
    let code = "";
    try {
      const body = (await res.json()) as { error?: string; code?: string };
      if (body.error) message = body.error;
      if (body.code) code = body.code;
    } catch {
      // Keep the status message.
    }
    throw new ApiError(localizeAPIError(message, res.status), res.status, code);
  }
  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

export function fetchConfig(): Promise<ConfigResponse> {
  return request<ConfigResponse>("/api/config");
}

export function fetchTree(): Promise<TreeResponse> {
  return request<TreeResponse>("/api/tree");
}

export function fetchGitHubSyncConfig(): Promise<GitHubSyncConfig> {
  return request<GitHubSyncConfig>("/api/github/config");
}

export function saveGitHubSyncConfig(
  repo: string,
  issue: number,
  token?: string,
): Promise<GitHubSyncConfig> {
  return request<GitHubSyncConfig>("/api/github/config", {
    method: "PUT",
    body: JSON.stringify({ repo, issue, token: token ?? "" }),
  });
}

export function clearGitHubSyncToken(
  repo: string,
  issue: number,
): Promise<GitHubSyncConfig> {
  return request<GitHubSyncConfig>("/api/github/config", {
    method: "PUT",
    body: JSON.stringify({ repo, issue, clearToken: true }),
  });
}

export function testGitHubSync(): Promise<GitHubSyncTest> {
  return request<GitHubSyncTest>("/api/github/test", {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export function postGitHubComment(body: string): Promise<GitHubSyncResult> {
  return request<GitHubSyncResult>("/api/github/comment", {
    method: "POST",
    body: JSON.stringify({ body }),
  });
}

export function searchDocs(query: string): Promise<SearchResponse> {
  return request<SearchResponse>(`/api/search?q=${encodeURIComponent(query)}`);
}

export function fetchChecklist(path: string): Promise<ChecklistResponse> {
  return request<ChecklistResponse>(
    `/api/doc/checklist?path=${encodeURIComponent(path)}`,
  );
}

export function applyChecklist(
  path: string,
  playbook: string,
): Promise<ChecklistResponse> {
  return request<ChecklistResponse>("/api/doc/checklist/apply", {
    method: "POST",
    body: JSON.stringify({ path, playbook }),
  });
}

export function addChecklistItem(
  path: string,
  title: string,
): Promise<ChecklistResponse> {
  return request<ChecklistResponse>("/api/doc/checklist/item", {
    method: "POST",
    body: JSON.stringify({ path, title }),
  });
}

export function toggleChecklistItem(
  path: string,
  id: string,
  done: boolean,
): Promise<ChecklistResponse> {
  return request<ChecklistResponse>("/api/doc/checklist/item", {
    method: "PATCH",
    body: JSON.stringify({ path, id, done }),
  });
}

export function deleteChecklistItem(
  path: string,
  id: string,
): Promise<ChecklistResponse> {
  return request<ChecklistResponse>(
    `/api/doc/checklist/item?path=${encodeURIComponent(path)}&id=${encodeURIComponent(id)}`,
    { method: "DELETE" },
  );
}

export function fetchDocVersions(
  path: string,
): Promise<{ versions: DocVersion[] }> {
  return request<{ versions: DocVersion[] }>(
    `/api/doc/versions?path=${encodeURIComponent(path)}`,
  );
}

export function fetchDocVersion(
  path: string,
  id: string,
): Promise<DocVersionDetail> {
  return request<DocVersionDetail>(
    `/api/doc/version?path=${encodeURIComponent(path)}&id=${encodeURIComponent(id)}`,
  );
}

export function restoreDocVersion(
  path: string,
  id: string,
): Promise<DocResponse> {
  return request<DocResponse>("/api/doc/restore", {
    method: "POST",
    body: JSON.stringify({ path, id }),
  });
}

export function fetchDoc(path: string): Promise<DocResponse> {
  return request<DocResponse>(`/api/doc?path=${encodeURIComponent(path)}`);
}

export function fetchInbox(): Promise<InboxResponse> {
  return request<InboxResponse>("/api/inbox");
}

export function fetchDocReview(path: string): Promise<DocReview> {
  return request<DocReview>(
    `/api/doc/review?path=${encodeURIComponent(path)}`,
  );
}

export function setDocReview(
  path: string,
  status: ReviewStatus,
): Promise<DocReview> {
  return request<DocReview>("/api/doc/review", {
    method: "POST",
    body: JSON.stringify({ path, status }),
  });
}

export function reactComment(
  path: string,
  id: string,
  reaction: string,
): Promise<Comment> {
  return request<Comment>("/api/comments/reaction", {
    method: "POST",
    body: JSON.stringify({ path, id, reaction }),
  });
}

export function resolveSuggestion(
  path: string,
  id: string,
  accept: boolean,
): Promise<Comment> {
  return request<Comment>("/api/comments/suggestion", {
    method: "POST",
    body: JSON.stringify({ path, id, accept }),
  });
}

export function requestDocReview(path: string): Promise<{ ok: boolean }> {
  return request<{ ok: boolean }>("/api/doc/review/request", {
    method: "POST",
    body: JSON.stringify({ path }),
  });
}

export function saveDoc(
  path: string,
  content: string,
  create = false,
): Promise<DocResponse> {
  return request<DocResponse>("/api/doc", {
    method: create ? "POST" : "PUT",
    body: JSON.stringify({ path, content }),
  });
}

export function deleteDoc(path: string): Promise<void> {
  return request<void>(`/api/doc?path=${encodeURIComponent(path)}`, {
    method: "DELETE",
  });
}

export function renderPreview(
  path: string,
  content: string,
  safe = false,
): Promise<RenderResponse> {
  return request<RenderResponse>("/api/render", {
    method: "POST",
    body: JSON.stringify({ path, content, safe }),
  });
}

export function addRoot(path: string): Promise<AddRootResponse> {
  return request<AddRootResponse>("/api/roots", {
    method: "POST",
    body: JSON.stringify({ path }),
  });
}

export function addRemoteRoot(
  config: AddRemoteRootRequest,
): Promise<AddRootResponse> {
  return request<AddRootResponse>("/api/roots/remote", {
    method: "POST",
    body: JSON.stringify(config),
  });
}

export function removeRoot(id: string): Promise<void> {
  return request<void>(`/api/roots?id=${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

export function browseDirectories(
  path: string,
  showHidden: boolean,
): Promise<BrowseResponse> {
  const params = new URLSearchParams();
  if (path) params.set("path", path);
  if (showHidden) params.set("hidden", "1");
  return request<BrowseResponse>(`/api/browse?${params.toString()}`);
}

export function createRemoteSession(
  config: RemoteSessionRequest,
  signal?: AbortSignal,
): Promise<RemoteSessionResponse> {
  return request<RemoteSessionResponse>("/api/remote/session", {
    method: "POST",
    body: JSON.stringify(config),
    signal,
  });
}




export function fetchAgentSessions(path: string): Promise<AgentSessionResponse> {
  return request<AgentSessionResponse>(
    `/api/doc/agent-sessions?path=${encodeURIComponent(path)}`,
  );
}

export function createAgentSession(path: string): Promise<AgentSession> {
  return request<AgentSession>("/api/doc/agent-sessions", {
    method: "POST",
    body: JSON.stringify({ path }),
  });
}

export function setActiveAgentSession(
  path: string,
  id: string,
): Promise<void> {
  return request<void>("/api/doc/agent-sessions/active", {
    method: "POST",
    body: JSON.stringify({ path, id }),
  });
}

export function fetchSettings(): Promise<AppSettings> {
  return request<AppSettings>("/api/settings");
}

export function fetchDefaultSettings(): Promise<AppSettings> {
  return request<AppSettings>("/api/settings/defaults");
}

export function saveSettings(settings: AppSettings): Promise<AppSettings> {
  return request<AppSettings>("/api/settings", {
    method: "PUT",
    body: JSON.stringify(settings),
  });
}

export function checkCommand(command: string): Promise<CheckCommandResult> {
  return request<CheckCommandResult>("/api/settings/check-command", {
    method: "POST",
    body: JSON.stringify({ command }),
  });
}

export function fetchDocDiscussions(
  path: string,
): Promise<DiscussionListResponse> {
  return request<DiscussionListResponse>(
    `/api/doc/discussions?path=${encodeURIComponent(path)}`,
  );
}

export function fetchAvailableDiscussionSessions(
  path: string,
): Promise<{ sessions: DiscussionSession[]; agents: DetectedAgent[] }> {
  return request<{ sessions: DiscussionSession[]; agents: DetectedAgent[] }>(
    `/api/doc/discussions/available?path=${encodeURIComponent(path)}`,
  );
}

export function importDiscussionSession(
  path: string,
  sessionPath: string,
): Promise<ImportDiscussionResponse> {
  return request<ImportDiscussionResponse>("/api/doc/discussions", {
    method: "POST",
    body: JSON.stringify({ path, sessionPath }),
  });
}

export function deleteDiscussion(path: string, id: string): Promise<void> {
  return request<void>(
    `/api/doc/discussions?path=${encodeURIComponent(path)}&id=${encodeURIComponent(id)}`,
    { method: "DELETE" },
  );
}

export function refreshConsensus(path: string): Promise<Consensus> {
  return request<Consensus>("/api/doc/consensus/refresh", {
    method: "POST",
    body: JSON.stringify({ path }),
  });
}

export function fetchPromptTemplates(
  language?: string,
): Promise<{ templates: PromptTemplate[] }> {
  return request<{ templates: PromptTemplate[] }>(
    "/api/prompt-templates",
    language ? { headers: { "Accept-Language": language } } : undefined,
  );
}

export function createPromptTemplate(
  input: PromptTemplateInput,
): Promise<PromptTemplate> {
  return request<PromptTemplate>("/api/prompt-templates", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function updatePromptTemplate(
  input: PromptTemplateInput,
): Promise<PromptTemplate> {
  return request<PromptTemplate>("/api/prompt-templates", {
    method: "PUT",
    body: JSON.stringify(input),
  });
}

export function deletePromptTemplate(id: string): Promise<void> {
  return request<void>(
    `/api/prompt-templates?id=${encodeURIComponent(id)}`,
    { method: "DELETE" },
  );
}

export function resetPromptTemplate(id: string): Promise<PromptTemplate> {
  return request<PromptTemplate>("/api/prompt-templates/reset", {
    method: "POST",
    body: JSON.stringify({ id }),
  });
}

export function optimizePromptTemplate(input: PromptTemplateInput): Promise<{ body: string }> {
  return request<{ body: string }>("/api/prompt-templates/optimize", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export function fetchCodeRepos(): Promise<CodeRepoListResponse> {
  return request<CodeRepoListResponse>("/api/code/repos");
}

export function fetchDocCodeRepos(path: string): Promise<DocCodeRepoResponse> {
  return request<DocCodeRepoResponse>(
    `/api/doc/code-repos?path=${encodeURIComponent(path)}`,
  );
}

export function addDocCodeRepo(
  docPath: string,
  repo: {
    repoId?: string;
    repoPath?: string;
    remoteUrl?: string;
    remoteBranch?: string;
  },
): Promise<CodeRepo> {
  return request<CodeRepo>("/api/doc/code-repos", {
    method: "POST",
    body: JSON.stringify({
      docPath,
      repoId: repo.repoId ?? "",
      repoPath: repo.repoPath ?? "",
      remoteUrl: repo.remoteUrl ?? "",
      remoteBranch: repo.remoteBranch ?? "",
    }),
  });
}

export function fetchCodeRepoBranches(
  id: string,
): Promise<{ branches: string[] }> {
  return request<{ branches: string[] }>(
    `/api/code/branches?id=${encodeURIComponent(id)}`,
  );
}

export function switchCodeRepoBranch(
  id: string,
  branch: string,
): Promise<CodeRepo> {
  return request<CodeRepo>("/api/code/branch", {
    method: "POST",
    body: JSON.stringify({ id, branch }),
  });
}

export function removeDocCodeRepo(
  docPath: string,
  repoId: string,
): Promise<void> {
  return request<void>(
    `/api/doc/code-repos?path=${encodeURIComponent(docPath)}&id=${encodeURIComponent(repoId)}`,
    { method: "DELETE" },
  );
}


export function fetchCodeFiles(
  repoId: string,
  query = "",
  limit = 5000,
): Promise<CodeFileList> {
  const params = new URLSearchParams({ repo: repoId, limit: String(limit) });
  if (query) params.set("query", query);
  return request<CodeFileList>(`/api/code/files?${params.toString()}`);
}

export function fetchCodeFile(
  repoId: string,
  path: string,
  start?: number,
  end?: number,
): Promise<CodeReadResult> {
  const params = new URLSearchParams({ repo: repoId, path });
  if (start) params.set("start", String(start));
  if (end) params.set("end", String(end));
  return request<CodeReadResult>(`/api/code/file?${params.toString()}`);
}

export function lspDefinition(
  repoId: string,
  path: string,
  line: number,
  character: number,
): Promise<{ locations: LSPLocation[] }> {
  return request<{ locations: LSPLocation[] }>("/api/lsp/definition", {
    method: "POST",
    body: JSON.stringify({ repo: repoId, path, line, character }),
  });
}

export function browseRemoteDirectories(
  session: string,
  path: string,
  showHidden: boolean,
): Promise<BrowseResponse> {
  const params = new URLSearchParams({ session });
  if (path) params.set("path", path);
  if (showHidden) params.set("hidden", "1");
  return request<BrowseResponse>(`/api/remote/browse?${params.toString()}`);
}

export function fetchComments(
  path: string,
): Promise<{ path: string; comments: Comment[] }> {
  return request<{ path: string; comments: Comment[] }>(
    `/api/comments?path=${encodeURIComponent(path)}`,
  );
}

export function createComment(
  path: string,
  quote: string,
  body: string,
  discussionIds: string[] = [],
  promptTemplateId = "",
  scope: "inline" | "document" = "inline",
): Promise<Comment> {
  return request<Comment>("/api/comments", {
    method: "POST",
    body: JSON.stringify({ path, quote, body, discussionIds, promptTemplateId, scope }),
  });
}

export function replyComment(
  path: string,
  id: string,
  body: string,
  options?: {
    before?: string;
    after?: string;
    author?: "user" | "agent";
    discussionIds?: string[];
    promptTemplateId?: string;
  },
): Promise<Comment> {
  const payload: Record<string, unknown> = {
    path,
    id,
    body,
    before: options?.before ?? "",
    after: options?.after ?? "",
    author: options?.author ?? "agent",
    promptTemplateId: options?.promptTemplateId ?? "",
  };
  // Omit the field when the caller does not know the current selection (e.g.
  // the discussion list has not loaded): the server keeps the stored context
  // instead of treating it as an explicit clear.
  if (options?.discussionIds !== undefined) {
    payload.discussionIds = options.discussionIds;
  }
  return request<Comment>("/api/comments/reply", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function resolveComment(
  path: string,
  id: string,
  resolved: boolean,
): Promise<Comment> {
  return request<Comment>("/api/comments/resolve", {
    method: "POST",
    body: JSON.stringify({ path, id, resolved }),
  });
}


export function triggerCommentAgent(
  path: string,
  id: string,
): Promise<void> {
  return request<void>("/api/comments/trigger", {
    method: "POST",
    body: JSON.stringify({ path, id }),
  });
}

export function groupComments(
  path: string,
  id: string,
  memberIds: string[],
): Promise<void> {
  return request<void>("/api/comments/group", {
    method: "POST",
    body: JSON.stringify({ path, id, memberIds }),
  });
}

export function rollbackComment(
  path: string,
  id: string,
  messageId: string,
  side: "before" | "after" = "after",
): Promise<{ round: number; comment: Comment; doc: DocResponse }> {
  return request<{ round: number; comment: Comment; doc: DocResponse }>(
    "/api/comments/rollback",
    {
      method: "POST",
      body: JSON.stringify({ path, id, messageId, side }),
    },
  );
}

export function deleteComment(path: string, id: string): Promise<void> {
  return request<void>(
    `/api/comments?path=${encodeURIComponent(path)}&id=${encodeURIComponent(id)}`,
    { method: "DELETE" },
  );
}

export function closeRemoteSession(session: string): Promise<void> {
  return request<void>(
    `/api/remote/session?session=${encodeURIComponent(session)}`,
    { method: "DELETE" },
  );
}
