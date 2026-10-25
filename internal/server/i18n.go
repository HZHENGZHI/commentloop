package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// apiErrorWriter buffers only error responses so the JSON `error` field can be
// localized without touching every handler. Successful responses are streamed
// through unchanged.
type apiErrorWriter struct {
	http.ResponseWriter
	status  int
	lang    string
	capture bool
	body    bytes.Buffer
}

func (w *apiErrorWriter) WriteHeader(status int) {
	w.status = status
	if status >= 400 {
		w.capture = true
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *apiErrorWriter) Write(p []byte) (int, error) {
	if w.capture {
		return w.body.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

func (w *apiErrorWriter) finish() {
	if !w.capture {
		return
	}
	body := w.body.Bytes()
	if w.lang == "en" {
		body = localizeErrorJSON(body, w.status)
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.ResponseWriter.WriteHeader(w.status)
	_, _ = w.ResponseWriter.Write(body)
}

func localizeErrorJSON(body []byte, status int) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	message, ok := payload["error"].(string)
	if !ok || message == "" {
		return body
	}
	payload["error"] = localizeErrorMessage(message, status)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return encoded
}

func localizeErrorMessage(message string, status int) string {
	if translated, ok := apiErrorTranslations[message]; ok {
		return translated
	}
	for prefix, translated := range apiErrorPrefixes {
		if strings.HasPrefix(message, prefix) {
			return translated + strings.TrimPrefix(message, prefix)
		}
	}
	if strings.IndexFunc(message, func(r rune) bool { return r >= 0x4e00 && r <= 0x9fff }) >= 0 {
		switch status {
		case http.StatusBadRequest:
			return "Request failed. Check the input and try again."
		case http.StatusForbidden:
			return "This action is not allowed."
		case http.StatusNotFound:
			return "The requested resource was not found."
		default:
			return "Request failed."
		}
	}
	return message
}

var apiErrorTranslations = map[string]string{
	"接口不存在":                     "API endpoint not found.",
	"请求格式错误":                    "Invalid request.",
	"路径无效或不是 Markdown 文件":       "Invalid path or not a Markdown file.",
	"路径无效":                      "Invalid path.",
	"路径不是目录":                    "Path is not a directory.",
	"路径是目录":                     "Path is a directory.",
	"评论不存在":                     "Comment not found.",
	"评论内容不能为空":                  "Comment cannot be empty.",
	"评论内容过长":                    "Comment is too long.",
	"回复内容不能为空":                  "Reply cannot be empty.",
	"回复内容过长":                    "Reply is too long.",
	"关联的评论不存在或不属于当前文档":          "The linked comment does not exist or belongs to another document.",
	"请选择要评论的文本":                 "Select the text to comment on.",
	"文档不存在":                     "Document not found.",
	"文档路径不匹配":                   "Document path does not match.",
	"尚未打开文档":                    "No document is open.",
	"无法读取文档，不能定位评论位置":           "Could not read the document to locate the comment.",
	"文档过大（上限 8 MiB）":            "Document is too large (limit: 8 MiB).",
	"文档过大，实时预览已暂停":              "Document is too large; live preview is paused.",
	"文件不存在":                     "File not found.",
	"文件已存在":                     "File already exists.",
	"同名文件已存在":                   "A file with this name already exists.",
	"文件过大":                      "File is too large.",
	"无法访问文件":                    "Could not access the file.",
	"无法读取文件":                    "Could not read the file.",
	"无法读取目录：":                   "Could not read directory: ",
	"目录不存在":                     "Directory not found.",
	"目录路径为空":                    "Directory path is empty.",
	"请输入目录路径":                   "Enter a directory path.",
	"目录已在工作区中":                  "Directory is already in the workspace.",
	"该远程目录已在工作区中":               "That remote directory is already in the workspace.",
	"目录已添加，但写入本地配置失败":           "Directory was added, but the local configuration could not be saved.",
	"最多只能添加 %d 个目录":             "Too many directories.",
	"模板名称和内容不能为空":               "Template name and body are required.",
	"模板内容过长":                    "Template content is too long.",
	"模板不存在":                     "Template not found.",
	"提示词模板不存在":                  "Prompt template not found.",
	"内置模板不能删除，只能恢复默认":           "Built-in templates cannot be deleted; restore the default instead.",
	"缺少模板 ID":                   "Missing template ID.",
	"缺少文档路径":                    "Missing document path.",
	"缺少目录 id":                   "Missing directory ID.",
	"缺少代码文件路径":                  "Missing code file path.",
	"行号和列号必须从 1 开始":             "Line and column must start at 1.",
	"代码仓库不存在":                   "Code repository not found.",
	"关联不存在":                     "Link not found.",
	"分支名不能为空":                   "Branch name is required.",
	"只有 GitHub 仓库支持分支列表":        "Only GitHub repositories support branch listing.",
	"只有 GitHub 仓库支持切换分支":        "Only GitHub repositories support branch switching.",
	"任务不存在":                     "Job not found.",
	"任务不存在或不属于该 Runner":         "Job not found or does not belong to this runner.",
	"runnerId 和 agentType 不能为空": "runnerId and agentType are required.",
	"外部 Agent 任务失败":             "External agent job failed.",
	"Agent 任务令牌无效或已过期":          "Agent job token is invalid or expired.",
	"跨站请求已被拒绝":                  "Cross-site request rejected.",
	"当前任务不能访问这个代码仓库":            "This task cannot access this code repository.",
	"当前任务模板不允许修改文档":             "This task template does not allow document edits.",
	"当前任务模板不允许回复评论":             "This task template does not allow comment replies.",
	"当前任务模板不允许读取代码仓库":           "This task template does not allow code repository reads.",
	"当前任务只能修改关联文档":              "This task may only edit its linked document.",
	"当前任务只能读取关联文档":              "This task may only read its linked document.",
	"当前任务只能回复关联文档的评论":           "This task may only reply to comments on its linked document.",
	"当前任务只能读取关联文档的评论":           "This task may only read comments on its linked document.",
	"受管 Agent 任务不能删除文档":         "Managed agent jobs cannot delete documents.",
	"受管 Agent 任务不能新建文档":         "Managed agent jobs cannot create documents.",
	"受管 Agent 任务必须指定关联文档":       "Managed agent jobs must specify a linked document.",
	"审阅模式下前台不能修改文件；后台 Agent 使用 MCP/CLI 写入": "The review UI is read-only; the background agent writes through MCP/CLI.",
	"审阅模式下前台不能编辑；请让后台 Agent 修改":            "The review UI is read-only; ask the background agent to make changes.",
	"评论任务":                     "Comment task",
	"会话不存在":                    "Session not found.",
	"会话名称不能为空":                 "Session name is required.",
	"会话不在允许的目录中":               "Session is outside the allowed directory.",
	"会话文件过大":                   "Session file is too large.",
	"会话中没有可用的对话内容":             "Session has no usable conversation.",
	"无效的会话路径":                  "Invalid session path.",
	"只支持 .jsonl 会话文件":          "Only .jsonl session files are supported.",
	"请选择会话":                    "Select a session.",
	"未命名会话":                    "Unnamed session",
	"共识内容不能为空":                 "Consensus content cannot be empty.",
	"共识内容过长":                   "Consensus content is too long.",
	"还没有可提炼的讨论会话":              "No discussion sessions to distill yet.",
	"未配置可用的 pi Agent，无法提炼讨论共识": "Configure a pi agent to distill discussion consensus.",
	"讨论会话不存在":                  "Discussion session not found.",
	"讨论会话不属于当前文档":              "Discussion session does not belong to this document.",
	"共识提炼没有返回内容":               "Consensus extraction returned no content.",
	"共识提炼超时":                   "Consensus extraction timed out.",
	"Agent 运行被中止":              "Agent run was stopped.",
	"Agent 已停止":                "Agent stopped.",
	"等待 Agent 超时":              "Timed out waiting for the agent.",
	"后台 Agent 未启动":             "Background agent is not running.",
	"Agent 启动命令为空":             "Agent command is empty.",
	"Agent 参数过多":               "Too many agent arguments.",
	"自动运行需要填写 Agent 启动命令":      "Automatic runs require an agent command.",
	"当前 Agent 类型不支持嵌入自动运行；可切换外部 Runner":          "This agent type does not support embedded automatic runs; use an external runner.",
	"嵌入自动运行目前仅支持 pi Agent；外部 Runner 可使用其他 Agent": "Embedded automatic runs currently support only the pi agent; use an external runner for other agents.",
	"AI 优化当前需要可用的 pi Agent，请先在设置中配置":             "AI optimization requires a configured pi agent.",
	"AI 优化超时":         "AI optimization timed out.",
	"AI 没有返回优化结果":     "AI returned no optimization result.",
	"请先写一个大概的提示词":     "Write a rough prompt first.",
	"SSH 用户名不能为空":     "SSH username is required.",
	"SSH 端口无效":        "Invalid SSH port.",
	"远程主机不能为空":        "Remote host is required.",
	"远程目录不能为空":        "Remote directory is required.",
	"远程路径不是目录":        "Remote path is not a directory.",
	"远程路径是目录":         "Remote path is a directory.",
	"远程文件过大":          "Remote file is too large.",
	"远程目录未连接":         "Remote directory is not connected.",
	"远程连接已关闭":         "Remote connection is closed.",
	"远程浏览会话已过期，请重新连接": "Remote browsing session expired; reconnect.",
	"无法解析远程主目录":       "Could not resolve the remote home directory.",
	"没有权限访问该目录":       "No permission to access this directory.",
	"没有可用的 SSH 认证方式：请启动 ssh-agent，或填写未加密私钥路径":  "No SSH authentication method available. Start ssh-agent or provide an unencrypted private key.",
	"未找到 known_hosts：请先在终端执行 ssh %s@%s 接受主机指纹": "known_hosts not found. Accept the host fingerprint with ssh first.",
	"暂不支持导入 %s 会话": "Importing this session type is not supported.",
	"无法读取远程目录":     "Could not read the remote directory.",
	"无法读取文档":       "Could not read the document.",
}

var apiErrorPrefixes = map[string]string{
	"无法读取目录：":      "Could not read directory: ",
	"无法读取文件：":      "Could not read file: ",
	"无法读取文件":       "Could not read file.",
	"保存设置失败：":      "Failed to save settings: ",
	"写入文件失败：":      "Failed to write file: ",
	"删除失败：":        "Delete failed: ",
	"无法添加该目录：":     "Could not add directory: ",
	"共识提炼失败：":      "Consensus extraction failed: ",
	"导入会话失败：":      "Failed to import session: ",
	"读取会话失败：":      "Failed to read sessions: ",
	"读取版本关联评论失败：":  "Failed to read linked comment versions: ",
	"回滚写入失败：":      "Rollback write failed: ",
	"外部 Agent 失败：": "External agent failed: ",
	"AI 优化失败：":     "AI optimization failed: ",
	"SSH 连接失败：":    "SSH connection failed: ",
	"SFTP 连接失败：":   "SFTP connection failed: ",
	"读取 SSH 私钥失败：": "Could not read SSH private key: ",
	"解析 SSH 私钥失败（加密私钥请使用 ssh-agent）：": "Could not parse SSH private key (use ssh-agent for encrypted keys): ",
	"读取 known_hosts 失败：":              "Could not read known_hosts: ",
	"远程目录不可用：":                        "Remote directory unavailable: ",
}
