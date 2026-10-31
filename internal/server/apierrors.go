package server

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

var (
	apiErrorsByCode    = map[string]*apiError{}
	apiErrorsByMessage = map[string]*apiError{} // exact ZH and EN text
	apiDetailPrefixes  []apiErrorPrefix         // longest prefix first
	apiPatternErrors   []*apiError
)

func defineError(code, zh, en string) *apiError {
	return registerAPIError(&apiError{Code: code, ZH: zh, EN: en})
}

func defineDetailError(code, zh, en string) *apiError {
	return registerAPIError(&apiError{Code: code, ZH: zh, EN: en, detail: true})
}

func registerAPIError(e *apiError) *apiError {
	if _, dup := apiErrorsByCode[e.Code]; dup {
		panic("duplicate API error code " + e.Code)
	}
	apiErrorsByCode[e.Code] = e
	switch {
	case e.detail:
		apiDetailPrefixes = append(apiDetailPrefixes, apiErrorPrefix{e.ZH, e}, apiErrorPrefix{e.EN, e})
		sort.SliceStable(apiDetailPrefixes, func(i, j int) bool {
			return len(apiDetailPrefixes[i].prefix) > len(apiDetailPrefixes[j].prefix)
		})
	case strings.Contains(e.ZH, "%"):
		e.zhRe = templatePattern(e.ZH)
		e.enRe = templatePattern(e.EN)
		apiPatternErrors = append(apiPatternErrors, e)
	default:
		apiErrorsByMessage[e.ZH] = e
		apiErrorsByMessage[strings.TrimSuffix(e.EN, ".")] = e
	}
	return e
}

var templateVerb = regexp.MustCompile(`%[ds]`)

func templatePattern(tmpl string) *regexp.Regexp {
	parts := templateVerb.Split(tmpl, -1)
	for i := range parts {
		parts[i] = regexp.QuoteMeta(parts[i])
	}
	return regexp.MustCompile("^" + strings.Join(parts, "(.+?)") + "$")
}

// lookupAPIError maps a plain message (e.g. an err.Error() from a lower layer,
// in either language) back to its catalog entry.
func lookupAPIError(message string) *apiError {
	if e, ok := apiErrorsByMessage[strings.TrimSuffix(message, ".")]; ok {
		return e
	}
	for _, p := range apiDetailPrefixes {
		if strings.HasPrefix(message, p.prefix) {
			return p.entry
		}
	}
	for _, e := range apiPatternErrors {
		if e.zhRe.MatchString(message) || e.enRe.MatchString(message) {
			return e
		}
	}
	return nil
}

func (e *apiError) localize(message, lang string) string {
	if lang != "en" {
		return message
	}
	switch {
	case e.detail:
		if strings.HasPrefix(message, e.EN) {
			return message
		}
		return e.EN + strings.TrimPrefix(message, e.ZH)
	case e.zhRe != nil:
		m := e.zhRe.FindStringSubmatch(message)
		if m == nil {
			return message
		}
		args := make([]any, len(m)-1)
		for i, v := range m[1:] {
			args[i] = v
		}
		return fmt.Sprintf(templateVerb.ReplaceAllString(e.EN, "%s"), args...)
	}
	return e.EN
}

// writeLocalizedError is for responses written outside the s.api adapter.
func writeLocalizedError(w http.ResponseWriter, r *http.Request, status int, e *apiError) {
	message := e.ZH
	if promptLanguage(r) == "en" {
		message = e.EN
	}
	writeJSON(w, status, map[string]string{"code": e.Code, "error": message})
}

func writeError(w http.ResponseWriter, status int, e *apiError) {
	writeJSON(w, status, map[string]string{"code": e.Code, "error": e.ZH})
}

func writeErrorDetail(w http.ResponseWriter, status int, e *apiError, detail string) {
	writeJSON(w, status, map[string]string{"code": e.Code, "error": e.ZH + detail})
}

// fallbackErrorCode is used for messages that are not in the catalog.
func fallbackErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusRequestEntityTooLarge:
		return "payload_too_large"
	}
	if status >= 500 {
		return "internal_error"
	}
	return "request_failed"
}

var (
	errAPIEndpointNotFound                   = defineError("api_endpoint_not_found", "接口不存在", "API endpoint not found.")
	errInvalidRequest                        = defineError("invalid_request", "请求格式错误", "Invalid request.")
	errRequestBodyTooLarge                   = defineError("request_body_too_large", "请求内容过大", "Request body is too large.")
	errInvalidPathNotMarkdownFile            = defineError("invalid_path_not_markdown_file", "路径无效或不是 Markdown 文件", "Invalid path or not a Markdown file.")
	errInvalidPath                           = defineError("invalid_path", "路径无效", "Invalid path.")
	errPathNotDirectory                      = defineError("path_not_directory", "路径不是目录", "Path is not a directory.")
	errPathIsDirectory                       = defineError("path_is_directory", "路径是目录", "Path is a directory.")
	errCommentNotFound                       = defineError("comment_not_found", "评论不存在", "Comment not found.")
	errCommentEmpty                          = defineError("comment_empty", "评论内容不能为空", "Comment cannot be empty.")
	errCommentTooLong                        = defineError("comment_too_long", "评论内容过长", "Comment is too long.")
	errReplyEmpty                            = defineError("reply_empty", "回复内容不能为空", "Reply cannot be empty.")
	errReplyTooLong                          = defineError("reply_too_long", "回复内容过长", "Reply is too long.")
	errLinkedCommentDoesNotExist             = defineError("linked_comment_does_not_exist", "关联的评论不存在或不属于当前文档", "The linked comment does not exist or belongs to another document.")
	errSelectTextComment                     = defineError("select_text_comment", "请选择要评论的文本", "Select the text to comment on.")
	errDocumentNotFound                      = defineError("document_not_found", "文档不存在", "Document not found.")
	errDocumentPathDoesNotMatch              = defineError("document_path_does_not_match", "文档路径不匹配", "Document path does not match.")
	errNoDocumentOpen                        = defineError("no_document_open", "尚未打开文档", "No document is open.")
	errCouldNotReadDocumentLocate            = defineError("could_not_read_document_locate", "无法读取文档，不能定位评论位置", "Could not read the document to locate the comment.")
	errDocumentTooLarge                      = defineError("document_too_large", "文档过大（上限 8 MiB）", "Document is too large (limit: 8 MiB).")
	errDocumentTooLargePreviewPaused         = defineError("document_too_large_preview_paused", "文档过大，实时预览已暂停", "Document is too large; live preview is paused.")
	errFileNotFound                          = defineError("file_not_found", "文件不存在", "File not found.")
	errFileAlreadyExists                     = defineError("file_already_exists", "文件已存在", "File already exists.")
	errFileNameAlreadyExists                 = defineError("file_name_already_exists", "同名文件已存在", "A file with this name already exists.")
	errFileTooLarge                          = defineError("file_too_large", "文件过大", "File is too large.")
	errCouldNotAccessFile                    = defineError("could_not_access_file", "无法访问文件", "Could not access the file.")
	errDirectoryNotFound                     = defineError("directory_not_found", "目录不存在", "Directory not found.")
	errDirectoryPathEmpty                    = defineError("directory_path_empty", "目录路径为空", "Directory path is empty.")
	errEnterDirectoryPath                    = defineError("enter_directory_path", "请输入目录路径", "Enter a directory path.")
	errDirectoryAlreadyWorkspace             = defineError("directory_already_workspace", "目录已在工作区中", "Directory is already in the workspace.")
	errRemoteDirectoryAlreadyWorkspace       = defineError("remote_directory_already_workspace", "该远程目录已在工作区中", "That remote directory is already in the workspace.")
	errDirectoryAddedConfigNotSaved          = defineError("directory_added_config_not_saved", "目录已添加，但写入本地配置失败", "Directory was added, but the local configuration could not be saved.")
	errTooManyDirectories                    = defineError("too_many_directories", "最多只能添加 %d 个目录", "You can add at most %d directories.")
	errTemplateNameBodyRequired              = defineError("template_name_body_required", "模板名称和内容不能为空", "Template name and body are required.")
	errTemplateContentTooLong                = defineError("template_content_too_long", "模板内容过长", "Template content is too long.")
	errTemplateNotFound                      = defineError("template_not_found", "模板不存在", "Template not found.")
	errPromptTemplateNotFound                = defineError("prompt_template_not_found", "提示词模板不存在", "Prompt template not found.")
	errBuiltTemplatesCannotDeleted           = defineError("built_templates_cannot_deleted", "内置模板不能删除，只能恢复默认", "Built-in templates cannot be deleted; restore the default instead.")
	errMissingTemplateID                     = defineError("missing_template_id", "缺少模板 ID", "Missing template ID.")
	errMissingDocumentPath                   = defineError("missing_document_path", "缺少文档路径", "Missing document path.")
	errMissingDirectoryID                    = defineError("missing_directory_id", "缺少目录 id", "Missing directory ID.")
	errMissingCodeFilePath                   = defineError("missing_code_file_path", "缺少代码文件路径", "Missing code file path.")
	errLineColumnMustStartAt                 = defineError("line_column_must_start_at", "行号和列号必须从 1 开始", "Line and column must start at 1.")
	errCodeRepositoryNotFound                = defineError("code_repository_not_found", "代码仓库不存在", "Code repository not found.")
	errLinkNotFound                          = defineError("link_not_found", "关联不存在", "Link not found.")
	errBranchNameRequired                    = defineError("branch_name_required", "分支名不能为空", "Branch name is required.")
	errGitHubOnlyBranchListing               = defineError("github_only_branch_listing", "只有 GitHub 仓库支持分支列表", "Only GitHub repositories support branch listing.")
	errGitHubOnlyBranchSwitching             = defineError("github_only_branch_switching", "只有 GitHub 仓库支持切换分支", "Only GitHub repositories support branch switching.")
	errJobNotFound                           = defineError("job_not_found", "任务不存在", "Job not found.")
	errJobNotOwnedByRunner                   = defineError("job_not_owned_by_runner", "任务不存在或不属于该 Runner", "Job not found or does not belong to this runner.")
	errRunnerIDAgentTypeRequired             = defineError("runnerid_agenttype_required", "runnerId 和 agentType 不能为空", "runnerId and agentType are required.")
	errExternalAgentJobFailed                = defineError("external_agent_job_failed", "外部 Agent 任务失败", "External agent job failed.")
	errAgentJobTokenInvalidExpired           = defineError("agent_job_token_invalid_expired", "Agent 任务令牌无效或已过期", "Agent job token is invalid or expired.")
	errCrossSiteRequestRejected              = defineError("cross_site_request_rejected", "跨站请求已被拒绝", "Cross-site request rejected.")
	errUIActionAgentForbidden                = defineError("ui_action_agent_forbidden", "受管 Agent 任务不能执行此操作", "Managed agent jobs cannot perform this action.")
	errTaskCannotAccessCodeRepository        = defineError("task_cannot_access_code_repository", "当前任务不能访问这个代码仓库", "This task cannot access this code repository.")
	errTaskCannotEditDocument                = defineError("task_cannot_edit_document", "当前任务模板不允许修改文档", "This task template does not allow document edits.")
	errTaskCannotReplyComments               = defineError("task_cannot_reply_comments", "当前任务模板不允许回复评论", "This task template does not allow comment replies.")
	errTaskCannotReadCode                    = defineError("task_cannot_read_code", "当前任务模板不允许读取代码仓库", "This task template does not allow code repository reads.")
	errTaskMayOnlyEditIts                    = defineError("task_may_only_edit_its", "当前任务只能修改关联文档", "This task may only edit its linked document.")
	errTaskMayOnlyReadIts                    = defineError("task_may_only_read_its", "当前任务只能读取关联文档", "This task may only read its linked document.")
	errTaskMayOnlyReplyComments              = defineError("task_may_only_reply_comments", "当前任务只能回复关联文档的评论", "This task may only reply to comments on its linked document.")
	errTaskMayOnlyReadComments               = defineError("task_may_only_read_comments", "当前任务只能读取关联文档的评论", "This task may only read comments on its linked document.")
	errManagedAgentJobsCannotDelete          = defineError("managed_agent_jobs_cannot_delete", "受管 Agent 任务不能删除文档", "Managed agent jobs cannot delete documents.")
	errManagedAgentJobsCannotCreate          = defineError("managed_agent_jobs_cannot_create", "受管 Agent 任务不能新建文档", "Managed agent jobs cannot create documents.")
	errManagedAgentJobsMustSpecify           = defineError("managed_agent_jobs_must_specify", "受管 Agent 任务必须指定关联文档", "Managed agent jobs must specify a linked document.")
	errReviewUiReadOnly                      = defineError("review_ui_read_only", "审阅模式下前台不能修改文件；后台 Agent 使用 MCP/CLI 写入", "The review UI is read-only; the background agent writes through MCP/CLI.")
	errReviewUiReadOnlyAskAgent              = defineError("review_ui_read_only_ask_agent", "审阅模式下前台不能编辑；请让后台 Agent 修改", "The review UI is read-only; ask the background agent to make changes.")
	errCommentTask                           = defineError("comment_task", "评论任务", "Comment task")
	errSessionNotFound                       = defineError("session_not_found", "会话不存在", "Session not found.")
	errSessionNameRequired                   = defineError("session_name_required", "会话名称不能为空", "Session name is required.")
	errSessionOutsideAllowedDirectory        = defineError("session_outside_allowed_directory", "会话不在允许的目录中", "Session is outside the allowed directory.")
	errSessionFileTooLarge                   = defineError("session_file_too_large", "会话文件过大", "Session file is too large.")
	errSessionHasNoUsableConversation        = defineError("session_has_no_usable_conversation", "会话中没有可用的对话内容", "Session has no usable conversation.")
	errInvalidSessionPath                    = defineError("invalid_session_path", "无效的会话路径", "Invalid session path.")
	errOnlyJSONLSessionFilesSupported        = defineError("only_jsonl_session_files_supported", "只支持 .jsonl 会话文件", "Only .jsonl session files are supported.")
	errSelectSession                         = defineError("select_session", "请选择会话", "Select a session.")
	errUnnamedSession                        = defineError("unnamed_session", "未命名会话", "Unnamed session")
	errConsensusContentCannotEmpty           = defineError("consensus_content_cannot_empty", "共识内容不能为空", "Consensus content cannot be empty.")
	errConsensusContentTooLong               = defineError("consensus_content_too_long", "共识内容过长", "Consensus content is too long.")
	errNoDiscussionSessionsDistillYet        = defineError("no_discussion_sessions_distill_yet", "还没有可提炼的讨论会话", "No discussion sessions to distill yet.")
	errConfigurePiAgentDistillDiscussion     = defineError("configure_pi_agent_distill_discussion", "未配置可用的 pi Agent，无法提炼讨论共识", "Configure a pi agent to distill discussion consensus.")
	errDiscussionSessionNotFound             = defineError("discussion_session_not_found", "讨论会话不存在", "Discussion session not found.")
	errDiscussionSessionDoesNotBelong        = defineError("discussion_session_does_not_belong", "讨论会话不属于当前文档", "Discussion session does not belong to this document.")
	errConsensusExtractionReturnedNoContent  = defineError("consensus_extraction_returned_no_content", "共识提炼没有返回内容", "Consensus extraction returned no content.")
	errConsensusExtractionTimedOut           = defineError("consensus_extraction_timed_out", "共识提炼超时", "Consensus extraction timed out.")
	errAgentRunStopped                       = defineError("agent_run_stopped", "Agent 运行被中止", "Agent run was stopped.")
	errAgentStopped                          = defineError("agent_stopped", "Agent 已停止", "Agent stopped.")
	errTimedOutWaitingAgent                  = defineError("timed_out_waiting_agent", "等待 Agent 超时", "Timed out waiting for the agent.")
	errBackgroundAgentNotRunning             = defineError("background_agent_not_running", "后台 Agent 未启动", "Background agent is not running.")
	errAgentCommandEmpty                     = defineError("agent_command_empty", "Agent 启动命令为空", "Agent command is empty.")
	errTooManyAgentArguments                 = defineError("too_many_agent_arguments", "Agent 参数过多", "Too many agent arguments.")
	errAutomaticRunsRequireAgentCommand      = defineError("automatic_runs_require_agent_command", "自动运行需要填写 Agent 启动命令", "Automatic runs require an agent command.")
	errAgentTypeDoesNotSupport               = defineError("agent_type_does_not_support", "当前 Agent 类型不支持嵌入自动运行；可切换外部 Runner", "This agent type does not support embedded automatic runs; use an external runner.")
	errEmbeddedAutomaticRunsCurrentlySupport = defineError("embedded_automatic_runs_currently_support", "嵌入自动运行目前仅支持 pi Agent；外部 Runner 可使用其他 Agent", "Embedded automatic runs currently support only the pi agent; use an external runner for other agents.")
	errAIOptimizationRequiresConfiguredPi    = defineError("ai_optimization_requires_configured_pi", "AI 优化当前需要可用的 pi Agent，请先在设置中配置", "AI optimization requires a configured pi agent.")
	errMethodNotAllowed                      = defineError("method_not_allowed", "不支持该请求方法", "Method not allowed.")
	errForbiddenHost                         = defineError("forbidden_host", "不允许的 Host", "Forbidden host.")
	errInternalServer                        = defineError("internal_server_error", "服务器内部错误", "Internal server error.")
	errAIOptimizationTimedOut                = defineError("ai_optimization_timed_out", "AI 优化超时", "AI optimization timed out.")
	errAIReturnedNoOptimizationResult        = defineError("ai_returned_no_optimization_result", "AI 没有返回优化结果", "AI returned no optimization result.")
	errWriteRoughPromptFirst                 = defineError("write_rough_prompt_first", "请先写一个大概的提示词", "Write a rough prompt first.")
	errSSHUsernameRequired                   = defineError("ssh_username_required", "SSH 用户名不能为空", "SSH username is required.")
	errInvalidSSHPort                        = defineError("invalid_ssh_port", "SSH 端口无效", "Invalid SSH port.")
	errRemoteHostRequired                    = defineError("remote_host_required", "远程主机不能为空", "Remote host is required.")
	errRemoteDirectoryRequired               = defineError("remote_directory_required", "远程目录不能为空", "Remote directory is required.")
	errRemotePathNotDirectory                = defineError("remote_path_not_directory", "远程路径不是目录", "Remote path is not a directory.")
	errRemotePathIsDirectory                 = defineError("remote_path_is_directory", "远程路径是目录", "Remote path is a directory.")
	errRemoteFileTooLarge                    = defineError("remote_file_too_large", "远程文件过大", "Remote file is too large.")
	errRemoteDirectoryNotConnected           = defineError("remote_directory_not_connected", "远程目录未连接", "Remote directory is not connected.")
	errRemoteConnectionClosed                = defineError("remote_connection_closed", "远程连接已关闭", "Remote connection is closed.")
	errRemoteBrowsingSessionExpired          = defineError("remote_browsing_session_expired", "远程浏览会话已过期，请重新连接", "Remote browsing session expired; reconnect.")
	errCouldNotResolveRemoteHome             = defineError("could_not_resolve_remote_home", "无法解析远程主目录", "Could not resolve the remote home directory.")
	errNoPermissionAccessDirectory           = defineError("no_permission_access_directory", "没有权限访问该目录", "No permission to access this directory.")
	errNoSSHAuthenticationMethodAvailable    = defineError("no_ssh_authentication_method_available", "没有可用的 SSH 认证方式：请启动 ssh-agent，或填写未加密私钥路径", "No SSH authentication method available. Start ssh-agent or provide an unencrypted private key.")
	errKnownHostsNotFoundAccept              = defineError("known_hosts_not_found_accept", "未找到 known_hosts：请先在终端执行 ssh %s@%s 接受主机指纹", "known_hosts not found. Run ssh %s@%s in a terminal to accept the host fingerprint first.")
	errImportingSessionTypeNotSupported      = defineError("importing_session_type_not_supported", "暂不支持导入 %s 会话", "Importing %s sessions is not supported yet.")
	errCouldNotReadRemoteDirectory           = defineError("could_not_read_remote_directory", "无法读取远程目录", "Could not read the remote directory.")
	errCouldNotReadDocument                  = defineError("could_not_read_document", "无法读取文档", "Could not read the document.")
	errCodeRepositoryPathRequired            = defineError("code_repository_path_required", "代码仓库路径不能为空", "Code repository path is required.")
	errCodeFilePathRequired                  = defineError("code_file_path_required", "代码文件路径不能为空", "Code file path is required.")
	errCodeFilePathMustInside                = defineError("code_file_path_must_inside", "代码文件路径必须位于仓库内", "Code file path must be inside the repository.")
	errNotRegularFile                        = defineError("not_regular_file", "不是普通文件", "Not a regular file.")
	errCodeFileExceeds1MiB                   = defineError("code_file_exceeds_1_mib", "代码文件超过 1 MiB", "Code file exceeds 1 MiB.")
	errCodeFileTooLargeUseSearch             = defineError("code_file_too_large_use_search", "代码文件超过 1 MiB，请使用搜索定位或分段读取", "Code file exceeds 1 MiB; use search or read it in chunks.")
	errBinaryFilesCannotRead                 = defineError("binary_files_cannot_read", "二进制文件不支持读取", "Binary files cannot be read.")
	errSearchQueryCannotEmpty                = defineError("search_query_cannot_empty", "搜索内容不能为空", "Search query cannot be empty.")
	errGitHubRepositoryURLRequired           = defineError("github_repository_url_required", "GitHub 仓库地址不能为空", "GitHub repository URL is required.")
	errOnlyGitHubComRepositoryURLs           = defineError("only_github_com_repository_urls", "只支持 github.com 仓库地址", "Only github.com repository URLs are supported.")
	errGitHubURLMustGitHubCom                = defineError("github_url_must_github_com", "GitHub 地址格式应为 github.com/owner/repo", "GitHub URL must be github.com/owner/repo.")
	errInvalidBranchName                     = defineError("invalid_branch_name", "分支名无效", "Invalid branch name.")
	errLocalRepositoryHasUncommittedChanges  = defineError("local_repository_has_uncommitted_changes", "本地仓库有未提交改动，请先提交或 stash 后再切换分支", "The local repository has uncommitted changes; commit or stash them before switching branches.")
	errNoLSPLanguageServerEnabled            = defineError("no_lsp_language_server_enabled", "当前文件类型没有启用 LSP 语言服务器", "No LSP language server is enabled for this file type.")
	errLSPServerCommandEmpty                 = defineError("lsp_server_command_empty", "LSP 服务器命令为空", "LSP server command is empty.")
	errLanguageServerExited                  = defineError("language_server_exited", "语言服务器已退出", "Language server exited.")
	errCouldNotLocateParagraphComment        = defineError("could_not_locate_paragraph_comment", "无法定位当前评论所在的段落，未做回滚", "Could not locate the paragraph for this comment; nothing was rolled back.")
	errEditRoundHasNoEarlier                 = defineError("edit_round_has_no_earlier", "这一轮修改没有可恢复的修改前内容", "This edit round has no earlier content to restore.")
	errRollbackCommentMissing                = defineError("rollback_comment_missing", "文档已局部回滚，但评论记录已不存在", "The document was partially rolled back, but the comment no longer exists.")
	errParagraphRollBackHasConflicting       = defineError("paragraph_roll_back_has_conflicting", "回滚段落发生冲突，未做回滚", "The paragraph to roll back has conflicting changes; nothing was rolled back.")
	errDocumentNoLongerExistsCannot          = defineError("document_no_longer_exists_cannot", "当前文档不存在，无法回滚", "The document no longer exists and cannot be rolled back.")
	errCouldNotLocateEditedParagraph         = defineError("could_not_locate_edited_paragraph", "无法定位这一轮修改后的段落，未做回滚", "Could not locate the edited paragraph; nothing was rolled back.")
	errEditRoundNotFound                     = defineError("edit_round_not_found", "找不到这一轮修改", "Edit round not found.")
	errEditRoundHasNoLater                   = defineError("edit_round_has_no_later", "这一轮修改没有可恢复的修改后快照", "This edit round has no later snapshot to restore.")
	errCommandEmpty                          = defineError("command_empty", "命令为空", "Command is empty.")
	errRollbackDirectionMustBeforeAfter      = defineError("rollback_direction_must_before_after", "回滚方向必须是 before 或 after", "Rollback direction must be before or after.")
	errCouldNotLocateOriginalParagraph       = defineError("could_not_locate_original_paragraph", "无法定位这一轮修改前的段落，未做回滚", "Could not locate the original paragraph; nothing was rolled back.")
	errCouldNotReadDirectoryDetail           = defineDetailError("could_not_read_directory_detail", "无法读取目录：", "Could not read directory: ")
	errCouldNotReadFileDetail                = defineDetailError("could_not_read_file_detail", "无法读取文件：", "Could not read file: ")
	errReadFileError                         = defineDetailError("read_file_error", "无法读取文件", "Could not read file.")
	errFailedSaveSettings                    = defineDetailError("failed_save_settings", "保存设置失败：", "Failed to save settings: ")
	errFailedWriteFile                       = defineDetailError("failed_write_file", "写入文件失败：", "Failed to write file: ")
	errDeleteFailed                          = defineDetailError("delete_failed", "删除失败：", "Delete failed: ")
	errCouldNotAddDirectory                  = defineDetailError("could_not_add_directory", "无法添加该目录：", "Could not add directory: ")
	errConsensusExtractionFailed             = defineDetailError("consensus_extraction_failed", "共识提炼失败：", "Consensus extraction failed: ")
	errFailedImportSession                   = defineDetailError("failed_import_session", "导入会话失败：", "Failed to import session: ")
	errFailedReadSessions                    = defineDetailError("failed_read_sessions", "读取会话失败：", "Failed to read sessions: ")
	errFailedReadLinkedCommentVersions       = defineDetailError("failed_read_linked_comment_versions", "读取版本关联评论失败：", "Failed to read linked comment versions: ")
	errRollbackWriteFailed                   = defineDetailError("rollback_write_failed", "回滚写入失败：", "Rollback write failed: ")
	errExternalAgentFailed                   = defineDetailError("external_agent_failed", "外部 Agent 失败：", "External agent failed: ")
	errAIOptimizationFailed                  = defineDetailError("ai_optimization_failed", "AI 优化失败：", "AI optimization failed: ")
	errSSHConnectionFailed                   = defineDetailError("ssh_connection_failed", "SSH 连接失败：", "SSH connection failed: ")
	errSFTPConnectionFailed                  = defineDetailError("sftp_connection_failed", "SFTP 连接失败：", "SFTP connection failed: ")
	errCouldNotReadSSHPrivate                = defineDetailError("could_not_read_ssh_private", "读取 SSH 私钥失败：", "Could not read SSH private key: ")
	errCouldNotParseSSHPrivate               = defineDetailError("could_not_parse_ssh_private", "解析 SSH 私钥失败（加密私钥请使用 ssh-agent）：", "Could not parse SSH private key (use ssh-agent for encrypted keys): ")
	errCouldNotReadKnownHosts                = defineDetailError("could_not_read_known_hosts", "读取 known_hosts 失败：", "Could not read known_hosts: ")
	errRemoteDirectoryUnavailable            = defineDetailError("remote_directory_unavailable", "远程目录不可用：", "Remote directory unavailable: ")
	errFailedLinkCodeRepository              = defineDetailError("failed_link_code_repository", "关联代码仓库失败：", "Failed to link code repository: ")
	errFailedCloneGitHubRepository           = defineDetailError("failed_clone_github_repository", "克隆 GitHub 仓库失败：", "Failed to clone GitHub repository: ")
	errFailedFetchBranches                   = defineDetailError("failed_fetch_branches", "拉取分支失败：", "Failed to fetch branches: ")
	errFailedSwitchBranch                    = defineDetailError("failed_switch_branch", "切换分支失败：", "Failed to switch branch: ")
	errLocalBranchNotFound                   = defineDetailError("local_branch_not_found", "本地分支不存在：", "Local branch not found: ")
	errFailedSwitchLocalBranch               = defineDetailError("failed_switch_local_branch", "切换本地分支失败：", "Failed to switch local branch: ")
	errNotFound                              = defineDetailError("not_found", "未找到 ", "Not found: ")
	errRollbackRecordFailed                  = defineDetailError("rollback_record_failed", "文档已局部回滚，但写入回滚记录失败：", "The document was partially rolled back, but saving the rollback record failed: ")
)
