import { useState, type FormEvent } from "react";
import type { Comment, CommentMessage } from "../types";
import { LinkCommentsModal } from "./LinkCommentsModal";
import {
  IconCheck,
  IconChevronDown,
  IconChevronRight,
  IconHistory,
  IconLink,
  IconLocate,
  IconMessage,
  IconTrash,
  IconX,
} from "./Icons";

interface CommentsPanelProps {
  comments: Comment[];
  onClose: () => void;
  onJump: (comment: Comment) => void;
  onResolve: (comment: Comment, resolved: boolean) => void;
  onDelete: (comment: Comment) => void;
  onReply: (comment: Comment, body: string) => Promise<void>;
  onGroup: (comment: Comment, memberIds: string[]) => Promise<void>;
  onRollback: (
    comment: Comment,
    message: CommentMessage,
    round: number,
    side: "before" | "after",
  ) => void;
  onTrigger: (comment: Comment) => Promise<void>;
  agentName?: string;
}

function formatTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function CommentsPanel({
  comments,
  onClose,
  onJump,
  onResolve,
  onDelete,
  onReply,
  onGroup,
  onRollback,
  onTrigger,
  agentName = "Code Agent",
}: CommentsPanelProps) {
  const [groupTarget, setGroupTarget] = useState<Comment | null>(null);
  const [replyTo, setReplyTo] = useState<string | null>(null);
  const [replyText, setReplyText] = useState("");
  const [replyError, setReplyError] = useState<string | null>(null);
  const [replyBusy, setReplyBusy] = useState(false);
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const unresolved = comments.filter((comment) => !comment.resolved).length;
  const allExpanded =
    comments.length > 0 && comments.every((comment) => expanded.has(comment.id));

  const toggleExpanded = (id: string) => {
    const closing = expanded.has(id);
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
    if (closing && replyTo === id) {
      setReplyTo(null);
      setReplyText("");
      setReplyError(null);
    }
  };

  const toggleAll = () => {
    if (allExpanded) {
      setExpanded(new Set());
      setReplyTo(null);
      setReplyText("");
      setReplyError(null);
      return;
    }
    setExpanded(new Set(comments.map((comment) => comment.id)));
  };

  const groupMembers = (comment: Comment): Comment[] =>
    comment.groupId
      ? comments.filter(
          (item) => item.id !== comment.id && item.groupId === comment.groupId,
        )
      : [];

  const jumpToRelated = (target: Comment) => {
    setExpanded((current) => new Set(current).add(target.id));
    onJump(target);
    window.setTimeout(() => {
      const card = document.querySelector<HTMLElement>(
        `[data-comment-id="${target.id}"]`,
      );
      card?.scrollIntoView({ block: "center", behavior: "smooth" });
      card?.classList.add("comment-card-focus");
      window.setTimeout(() => card?.classList.remove("comment-card-focus"), 1200);
    }, 60);
  };

  const submitReply = async (event: FormEvent, comment: Comment) => {
    event.preventDefault();
    if (!replyText.trim()) {
      setReplyError("请输入要继续修改的内容");
      return;
    }
    setReplyBusy(true);
    setReplyError(null);
    try {
      await onReply(comment, replyText.trim());
      setReplyTo(null);
      setReplyText("");
    } catch (err) {
      setReplyError(err instanceof Error ? err.message : "回复失败");
    } finally {
      setReplyBusy(false);
    }
  };
  return (
    <aside className="comments-panel">
      <div className="comments-head">
        <div className="comments-title">
          <IconMessage size={15} />
          评论
          <span className="comments-count">
            {unresolved} 未解决 / {comments.length}
          </span>
        </div>
        <div className="comments-head-actions">
          {comments.length > 0 ? (
            <button
              type="button"
              className="comments-expand-all"
              onClick={toggleAll}
            >
              {allExpanded ? "全部收起" : "全部展开"}
            </button>
          ) : null}
          <button
            type="button"
            className="icon-btn small"
            onClick={onClose}
            aria-label="关闭评论"
          >
            <IconX size={15} />
          </button>
        </div>
      </div>
      <div className="comments-body">
        {comments.length === 0 ? (
          <div className="comments-empty">
            <IconMessage size={22} />
            <p>还没有评论</p>
            <span>在预览中选择文字，点击“评论”即可添加。</span>
          </div>
        ) : (
          comments.map((comment) => {
            const members = groupMembers(comment);
            const lastMessage = comment.messages[comment.messages.length - 1];
            const isExpanded =
              expanded.has(comment.id) || replyTo === comment.id;
            const lastAuthor =
              lastMessage?.author === "agent"
                ? "Code Agent"
                : lastMessage?.author === "system"
                  ? "版本回滚"
                  : "你";
            return (
            <article
              key={comment.id}
              data-comment-id={comment.id}
              className={`comment-card${comment.resolved ? " resolved" : ""}${
                comment.groupId ? " grouped" : ""
              }${isExpanded ? " expanded" : " collapsed"}`}
            >
              <div className="comment-card-head">
                <div className="comment-location">
                  <button
                    type="button"
                    className="comment-expand"
                    onClick={() => toggleExpanded(comment.id)}
                    aria-expanded={isExpanded}
                    aria-label={isExpanded ? "收起评论" : "展开评论"}
                    title={isExpanded ? "收起评论" : "展开评论"}
                  >
                    {isExpanded ? (
                      <IconChevronDown size={12} />
                    ) : (
                      <IconChevronRight size={12} />
                    )}
                  </button>
                  {comment.orphaned || comment.line === 0 ? (
                    <span className="comment-orphaned">未定位</span>
                  ) : (
                    <span>第 {comment.line} 行</span>
                  )}
                  {comment.resolved ? (
                    <span className="comment-resolved-badge">已解决</span>
                  ) : null}
                  {members.length > 0 ? (
                    <span className="comment-group-badge">
                      <IconLink size={9} />
                      {members.length + 1} 条关联
                    </span>
                  ) : null}
                </div>
                <div className="comment-actions">
                  <button
                    type="button"
                    className="comment-action"
                    onClick={() => onJump(comment)}
                    title="在预览中定位"
                  >
                    <IconLocate size={11} />
                    定位
                  </button>
                  <button
                    type="button"
                    className="comment-action"
                    onClick={() => setGroupTarget(comment)}
                    title={comment.groupId ? "管理关联评论" : "关联其他评论"}
                    aria-label={comment.groupId ? "管理关联评论" : "关联其他评论"}
                  >
                    <IconLink size={11} />
                    关联评论
                  </button>
                  <button
                    type="button"
                    className="comment-action"
                    onClick={() => onResolve(comment, !comment.resolved)}
                    title={comment.resolved ? "重新打开" : "标记解决"}
                  >
                    <IconCheck size={12} />
                    {comment.resolved ? "重新打开" : "解决"}
                  </button>
                  <button
                    type="button"
                    className="comment-action danger"
                    onClick={() => onDelete(comment)}
                    title="删除评论"
                    aria-label="删除评论"
                  >
                    <IconTrash size={12} />
                  </button>
                </div>
              </div>

              <blockquote
                className="comment-anchor"
                onClick={() => onJump(comment)}
                title="点击定位"
              >
                {comment.quote}
              </blockquote>

              {isExpanded && members.length > 0 ? (
                <div className="comment-related">
                  <div className="comment-related-head">
                    <span>
                      <IconLink size={10} />
                      关联 {members.length + 1} 条评论
                    </span>
                    <button
                      type="button"
                      className="comment-related-manage"
                      onClick={() => setGroupTarget(comment)}
                    >
                      管理
                    </button>
                  </div>
                  <div className="comment-related-list">
                    {members.map((member) => (
                      <button
                        key={member.id}
                        type="button"
                        className="comment-related-item"
                        onClick={() => jumpToRelated(member)}
                        title={member.quote}
                      >
                        <span>
                          {member.line > 0 ? `第 ${member.line} 行` : "未定位"}
                        </span>
                        {member.resolved ? <em>已解决</em> : null}
                        <strong>{member.quote.replace(/\s+/g, " ").trim()}</strong>
                      </button>
                    ))}
                  </div>
                </div>
              ) : null}

              {isExpanded ? (
              <div className="comment-thread">
                {(() => {
                  let agentRound = 0;
                  return comment.messages.map((message) => {
                    if (message.author === "agent") agentRound += 1;
                    const roundForMessage = agentRound;
                    return (
                      <div
                        key={message.id}
                        className={`comment-message ${message.author}`}
                      >
                        <div className="comment-message-meta">
                          <span className="comment-author">
                            {message.author === "agent"
                              ? `${agentName}${roundForMessage > 1 ? ` · 第 ${roundForMessage} 次修改` : ""}`
                              : message.author === "system"
                                ? "版本回滚"
                                : "你"}
                          </span>
                          <span>{formatTime(message.createdAt)}</span>
                        </div>
                        <div className="comment-message-body">{message.body}</div>
                        {message.before || message.after ? (
                          <div className="comment-change">
                            <div className="comment-change-head">变更内容</div>
                            {message.before ? (
                              <div className="comment-diff before">
                                <div className="comment-diff-label">
                                  <span className="diff-badge">−</span>
                                  {message.author === "system" ? "回滚前" : "修改前"}
                                </div>
                                <pre>{message.before}</pre>
                              </div>
                            ) : null}
                            {message.before && message.after ? (
                              <div
                                className="comment-change-arrow"
                                aria-hidden="true"
                              >
                                ↓
                              </div>
                            ) : null}
                            {message.after ? (
                              <div className="comment-diff after">
                                <div className="comment-diff-label">
                                  <span className="diff-badge">+</span>
                                  {message.author === "system" ? "恢复后" : "修改后"}
                                </div>
                                <pre>{message.after}</pre>
                              </div>
                            ) : null}
                          </div>
                        ) : null}
                        {message.author === "agent" &&
                        (message.versionId || message.before) ? (
                          <div className="comment-version">
                            <span>此轮快照</span>
                            <div className="comment-version-actions">
                              {message.before ? (
                                <button
                                  type="button"
                                  className="comment-version-btn"
                                  onClick={() =>
                                    onRollback(
                                      comment,
                                      message,
                                      roundForMessage,
                                      "before",
                                    )
                                  }
                                  title="只把评论段落恢复为这一轮修改前的内容"
                                >
                                  <IconHistory size={11} />
                                  回滚前
                                </button>
                              ) : null}
                              {message.versionId ? (
                                <button
                                  type="button"
                                  className="comment-version-btn"
                                  onClick={() =>
                                    onRollback(
                                      comment,
                                      message,
                                      roundForMessage,
                                      "after",
                                    )
                                  }
                                  title="只把评论段落恢复为这一轮修改后的内容"
                                >
                                  <IconHistory size={11} />
                                  回滚后
                                </button>
                              ) : null}
                            </div>
                          </div>
                        ) : null}
                      </div>
                    );
                  });
                })()}
              </div>
              ) : null}

              {!isExpanded ? (
                <button
                  type="button"
                  className="comment-collapsed"
                  onClick={() => toggleExpanded(comment.id)}
                  aria-label="展开评论"
                >
                  <span className="comment-collapsed-count">
                    {comment.messages.length} 条讨论
                  </span>
                  <span className="comment-collapsed-text">
                    {lastMessage
                      ? `${lastAuthor}：${lastMessage.body.replace(/\s+/g, " ").trim()}`
                      : "展开评论"}
                  </span>
                  <IconChevronRight size={11} />
                </button>
              ) : null}

              <div
                className={`comment-status ${
                  comment.resolved
                    ? "resolved"
                    : lastMessage?.author === "user"
                      ? "waiting"
                      : "review"
                }`}
              >
                {comment.resolved
                  ? "已确认解决"
                  : lastMessage?.author === "user"
                    ? `等待 ${agentName} 继续修改`
                    : lastMessage?.author === "system"
                      ? "已局部回滚，等待你的确认"
                      : "等待你的确认，或继续回复 Agent"}
              </div>

              {!comment.resolved &&
              lastMessage?.author === "user" &&
              replyTo !== comment.id ? (
                <button
                  type="button"
                  className="comment-agent-trigger"
                  onClick={() => void onTrigger(comment)}
                >
                  让 Agent 处理
                </button>
              ) : null}

              {replyTo === comment.id ? (
                <form
                  className="comment-reply-form"
                  onSubmit={(event) => void submitReply(event, comment)}
                >
                  <textarea
                    className="comment-reply-input"
                    value={replyText}
                    onChange={(event) => setReplyText(event.target.value)}
                    placeholder={`说明还需要修改什么，发送给 ${agentName}…`}
                    rows={3}
                    autoFocus
                  />
                  {replyError ? (
                    <p className="comment-reply-error">{replyError}</p>
                  ) : null}
                  <div className="comment-reply-actions">
                    <button
                      type="button"
                      className="comment-action"
                      onClick={() => {
                        setReplyTo(null);
                        setReplyText("");
                        setReplyError(null);
                      }}
                    >
                      取消
                    </button>
                    <button
                      type="submit"
                      className="btn primary comment-reply-submit"
                      disabled={replyBusy}
                    >
                      {replyBusy ? "发送中…" : "发送给 Agent"}
                    </button>
                  </div>
                </form>
              ) : (
                <button
                  type="button"
                  className="comment-reply-trigger"
                  onClick={() => {
                    setExpanded((current) => new Set(current).add(comment.id));
                    setReplyTo(comment.id);
                    setReplyText("");
                    setReplyError(null);
                  }}
                >
                  <IconMessage size={12} />
                  {comment.resolved ? "有新的修改意见" : "继续修改 / 回复"}
                </button>
              )}
            </article>
            );
          })
        )}
      </div>
      {groupTarget ? (
        <LinkCommentsModal
          comment={groupTarget}
          comments={comments}
          agentName={agentName}
          onClose={() => setGroupTarget(null)}
          onSave={(memberIds) => onGroup(groupTarget, memberIds)}
        />
      ) : null}
    </aside>
  );
}
