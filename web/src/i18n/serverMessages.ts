import i18n from "./index";

const EXACT: Record<string, string> = {
  "Agent 正在读取文档和评论…": "Agent is reading the document and comments…",
  "Agent 正在写入文档…": "Agent is writing the document…",
  "Agent 正在回复评论…": "Agent is replying to the comment…",
  "Agent 正在读取代码…": "Agent is reading code…",
  "Agent 正在检查工作区…": "Agent is checking the workspace…",
  "外部 Agent 正在处理评论…": "External agent is processing the comment…",
  "外部 Agent 已完成任务": "External agent finished the task.",
  "外部 Agent 任务失败": "External agent task failed.",
  "正在导入会话…": "Importing session…",
  "正在移除旧共识…": "Removing the old consensus…",
  "讨论已删除，共识已清空": "Discussion removed; consensus cleared.",
  "讨论已删除，正在从剩余讨论重建共识…": "Discussion removed; rebuilding consensus from the remaining sessions…",
  "正在整理讨论内容…": "Organizing discussion…",
  "Agent 正在提炼共识…": "Agent is extracting consensus…",
  "正在保存新共识…": "Saving new consensus…",
  "检测到会话内容变化，正在重建共识…": "Session content changed; rebuilding consensus…",
  "检测到会话有新内容，准备更新共识…": "New session content detected; preparing consensus update…",
  "正在分析评论…": "Analyzing the comment…",
  "正在生成修改内容…": "Generating the edit…",
  "正在整理修改结果…": "Preparing the result…",
  "正在读取文档…": "Reading the document…",
  "正在读取评论…": "Reading comments…",
  "正在读取代码…": "Reading code…",
  "正在浏览代码文件…": "Browsing code files…",
  "正在查看代码仓库…": "Checking the code repository…",
  "正在搜索代码…": "Searching code…",
  "正在写入文档…": "Writing the document…",
  "正在回复评论…": "Replying to the comment…",
  "正在调用 ": "Calling ",
  "工具执行失败，正在调整方案…": "Tool call failed; adjusting the approach…",
  "pi RPC 已退出": "pi RPC exited.",
  "pi RPC 进程已退出": "pi RPC process exited.",
  "Agent 运行被中止": "Agent run stopped.",
  "Agent 已停止": "Agent stopped.",
  "等待 Agent 超时": "Timed out waiting for the agent.",
  "后台 Agent 未启动": "Background agent is not running.",
  "任务不存在": "Job not found.",
  "评论任务": "Comment task",
  "无法解析消息": "Could not parse the message.",
  "路径无效": "Invalid path.",
  "无法读取文件": "Could not read the file.",
  "审阅模式下前台不能编辑；请让后台 Agent 修改": "The review UI is read-only; ask the background agent to make changes.",
  "尚未打开文档": "No document is open.",
  "文档路径不匹配": "Document path does not match.",
  "文档过大，实时预览已暂停": "The document is too large; live preview is paused.",
  "未知消息类型": "Unknown message type.",
  "目录已添加，但写入本地配置失败": "Directory added, but the local configuration could not be saved.",
};

const PREFIXES: Array<[string, string]> = [
  ["已创建外部 Agent 任务 ", "Created external agent job "],
  ["已通知 ", "Notified "],
  ["共识已更新到 v", "Consensus updated to v"],
  ["共识提炼失败：", "Consensus extraction failed: "],
  ["外部 Agent 失败：", "External agent failed: "],
  ["导入会话失败：", "Failed to import session: "],
  ["读取会话失败：", "Failed to read sessions: "],
  ["已连接，但读取远程文件失败：", "Connected, but failed to read remote files: "],
];

export function localizeServerMessage(
  message: string,
  status: string = "working",
): string {
  if (!message || i18n.language !== "en") return message;
  if (EXACT[message]) return EXACT[message];
  for (const [prefix, translated] of PREFIXES) {
    if (message.startsWith(prefix)) {
      return translated + message.slice(prefix.length);
    }
  }
  const processing = message.match(/^(.+) 正在处理评论…$/);
  if (processing) return `${processing[1]} is processing the comment…`;
  const completed = message.match(/^(.+) 已完成一轮修改，等待你确认$/);
  if (completed) return `${completed[1]} finished a round; waiting for your review.`;
  const failed = message.match(/^(.+) 处理失败：(.*)$/s);
  if (failed) return `${failed[1]} failed: ${failed[2]}`;
  const started = message.match(/^(.+) 启动失败：(.*)$/s);
  if (started) return `${started[1]} failed to start: ${started[2]}`;
  if (/[\u4e00-\u9fff]/.test(message)) {
    return status === "error"
      ? "Agent failed."
      : status === "done"
        ? "Agent finished."
        : status === "warning"
          ? "Completed with a warning."
          : "Agent is working…";
  }
  return message;
}
