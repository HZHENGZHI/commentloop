export type Theme = "light" | "dark";
export type ViewMode = "split" | "editor" | "preview";
export type SaveStatus = "saved" | "dirty" | "saving";

export interface FileEntry {
  path: string;
  relPath: string;
  name: string;
  dir: string;
  rootId: string;
  rootName: string;
  size: number;
  modTime: string;
}

export interface RootInfo {
  id: string;
  kind: "local" | "remote";
  name: string;
  path: string;
  remote?: string;
  primary: boolean;
  offline?: string;
  loading?: boolean;
  files: FileEntry[];
}

export interface TreeResponse {
  roots: RootInfo[];
  files: FileEntry[];
  truncated: boolean;
}

export interface DocResponse {
  path: string;
  content: string;
  html: string;
  revision: string;
  modTime: string;
  size: number;
}

export interface ConfigResponse {
  root: string;
  rootName: string;
  version: string;
  previewOnly: boolean;
  debug: boolean;
  hotReload: boolean;
  rootCount: number;
  agentName?: string;
}

export interface AddedRoot {
  id: string;
  kind: "local" | "remote";
  name: string;
  path: string;
  remote?: string;
  primary: boolean;
}

export interface AddRemoteRootRequest {
  name: string;
  host: string;
  port: number;
  user: string;
  keyPath: string;
  path: string;
}

export interface AddRootResponse {
  root: AddedRoot;
  warning?: string;
}

export interface BrowseEntry {
  name: string;
  path: string;
  hidden: boolean;
}

export interface BrowseShortcut {
  name: string;
  path: string;
}

export interface BrowseResponse {
  path: string;
  parent: string;
  entries: BrowseEntry[];
  shortcuts: BrowseShortcut[];
  truncated: boolean;
}

export interface RemoteSessionRequest {
  host: string;
  port: number;
  user: string;
  keyPath: string;
}

export interface RemoteSessionResponse {
  session: string;
  home: string;
}

export interface RenderResponse {
  html: string;
  revision: string;
}

export type WsServerMessage =
  | { type: "subscribed"; path: string }
  | { type: "unsubscribed" }
  | { type: "preview"; path: string; html: string; revision: string }
  | {
      type: "doc";
      path: string;
      content: string;
      html: string;
      revision: string;
      origin: "save" | "external" | "remote" | "init" | "rollback";
    }
  | { type: "tree" }
  | { type: "comments"; path: string }
  | { type: "discussions"; path: string }
  | {
      type: "consensus";
      path: string;
      status?: ConsensusPhase;
      message?: string;
    }
  | {
      type: "agent";
      path?: string;
      status?: "queued" | "working" | "done" | "error";
      message?: string;
    }
  | { type: "reload"; message?: string }
  | { type: "gone"; path: string }
  | { type: "error"; path?: string; message: string };

export interface ToastItem {
  id: number;
  kind: "info" | "success" | "warn" | "error";
  message: string;
}

export interface DocStats {
  words: number;
  chars: number;
  lines: number;
}




export interface AgentSession {
  id: string;
  docPath: string;
  title: string;
  active?: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface AgentSessionResponse {
  path: string;
  sessions: AgentSession[];
  activeId: string;
}

export interface LSPServerSetting {
  id: string;
  label: string;
  extensions: string[];
  languageId: string;
  command: string;
  args: string[];
  enabled: boolean;
}

export interface CodeAgentSetting {
  type: string;
  name: string;
  command: string;
  args: string[];
  runner: "embedded" | "external";
  autoRun: boolean;
}

export interface AppSettings {
  lspServers: LSPServerSetting[];
  agent: CodeAgentSetting;
}

export interface CheckCommandResult {
  found: boolean;
  path?: string;
  error?: string;
}

export interface CodeRepo {
  id: string;
  name: string;
  path: string;
  kind?: "local" | "github";
  remoteUrl?: string;
  remoteBranch?: string;
  branch?: string;
  head?: string;
  dirty?: boolean;
  changed?: string[];
  linked?: boolean;
}

export interface CodeRepoListResponse {
  repos: CodeRepo[];
}

export interface DocCodeRepoResponse {
  path: string;
  repos: CodeRepo[];
}


export interface CodeFileEntry {
  path: string;
  size: number;
  modTime: string;
}

export interface CodeFileList {
  files: CodeFileEntry[];
  total: number;
  truncated: boolean;
}

export interface CodeReadResult {
  path: string;
  content: string;
  startLine: number;
  endLine: number;
  totalLines: number;
  size: number;
  truncated: boolean;
}

export interface LSPLocation {
  path: string;
  line: number;
  character: number;
  external?: boolean;
}

export interface CommentMessage {
  id: string;
  author: "user" | "agent" | "system";
  body: string;
  before?: string;
  after?: string;
  versionId?: string;
  beforeVersionId?: string;
  createdAt: string;
}

export interface PromptTemplate {
  id: string;
  name: string;
  description: string;
  body: string;
  allowDocumentEdit: boolean;
  allowCodeRead: boolean;
  allowCommentReply: boolean;
  builtin: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface PromptTemplateInput {
  id?: string;
  name: string;
  description: string;
  body: string;
  allowDocumentEdit: boolean;
  allowCodeRead: boolean;
  allowCommentReply: boolean;
}

export interface Comment {
  id: string;
  path: string;
  quote: string;
  anchor?: string;
  groupId?: string;
  prefix?: string;
  suffix?: string;
  line: number;
  start: number;
  end: number;
  resolved: boolean;
  orphaned: boolean;
  createdAt: string;
  updatedAt: string;
  discussionIds?: string[];
  promptTemplateId?: string;
  messages: CommentMessage[];
}

export interface Discussion {
  id: string;
  title: string;
  sourcePath: string;
  sourceKind: "mdlive" | "pi" | "claude" | "codex" | string;
  agent: string;
  agentName: string;
  cwd?: string;
  sessionId?: string;
  messageCount: number;
  createdAt: string;
  importedAt: string;
}

export type ConsensusPhase =
  | "queued"
  | "collecting"
  | "summarizing"
  | "saving"
  | "done"
  | "error";

export interface ConsensusActivity {
  path: string;
  status: ConsensusPhase;
  message?: string;
}

export interface Consensus {
  content: string;
  source?: string;
  version: number;
  updatedAt: string;
}

export interface DiscussionListResponse {
  path: string;
  consensus?: Consensus;
  discussions: Discussion[];
}

export interface DiscussionSession {
  path: string;
  title: string;
  sourceKind: "mdlive" | "pi" | "claude" | "codex" | string;
  agent: string;
  agentName: string;
  cwd?: string;
  sessionId?: string;
  messageCount: number;
  createdAt: string;
  updatedAt: string;
  imported?: boolean;
}

export interface DetectedAgent {
  id: string;
  name: string;
  command: string;
  installed: boolean;
  supported: boolean;
  count: number;
}

export interface ImportDiscussionResponse {
  discussion: Discussion;
  consensus?: Consensus;
  consensusError?: string;
}
