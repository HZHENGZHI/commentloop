import {
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type MouseEvent,
} from "react";
import { Button, Empty, Input, Segmented, Select, Tooltip } from "antd";
import { useTranslation } from "react-i18next";
import { renderPreview } from "../api";
import { isMarkdownPath, rawUrl, resolveRelative } from "../utils";
import type {
  Comment,
  CommentMessage,
  Discussion,
  DocReview,
  PromptTemplate,
} from "../types";
import { LinkCommentsModal } from "./LinkCommentsModal";
import { ReviewChecklist } from "./ReviewChecklist";
import { DiscussionRefPicker } from "./DiscussionRefPicker";
import {
  IconCheck,
  IconChevronDown,
  IconChevronRight,
  IconDownload,
  IconHistory,
  IconLink,
  IconLocate,
  IconMessage,
  IconSearch,
  IconSparkles,
  IconTrash,
  IconX,
} from "./Icons";
import { SeverityBadge } from "./ReviewStatus";

interface CommentsPanelProps {
  comments: Comment[];
  docPath?: string;
  onOpenDoc: (path: string) => void;
  onClose: () => void;
  onJump: (comment: Comment) => void;
  onResolve: (comment: Comment, resolved: boolean) => void;
  onDelete: (comment: Comment) => void;
  onReply: (
    comment: Comment,
    body: string,
    discussionIds: string[] | undefined,
    promptTemplateId: string,
  ) => Promise<void>;
  discussions?: Discussion[];
  discussionsLoaded?: boolean;
  promptTemplates?: PromptTemplate[];
  onGroup: (comment: Comment, memberIds: string[]) => Promise<void>;
  onRollback: (
    comment: Comment,
    message: CommentMessage,
    round: number,
    side: "before" | "after",
  ) => void;
  onTrigger: (comment: Comment) => Promise<void>;
  review?: DocReview;
  reviewing?: boolean;
  onRequestReview: () => void;
  onSuggestion: (comment: Comment, accept: boolean) => void;
  onReaction: (comment: Comment, reaction: string) => void;
  onNewDocumentComment: () => void;
  onCopyLink: (comment: Comment) => void;
  onExport: () => void;
  highlightId?: string | null;
  agentName?: string;
}

function formatTime(value: string, locale: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString(locale, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function isExternalHref(href: string): boolean {
  return /^(https?:|mailto:)/i.test(href) || href.startsWith("//");
}

export function CommentsPanel({
  comments,
  docPath,
  onOpenDoc,
  onClose,
  onJump,
  onResolve,
  onDelete,
  onReply,
  discussions = [],
  discussionsLoaded = false,
  promptTemplates = [],
  onGroup,
  onRollback,
  onTrigger,
  review,
  reviewing = false,
  onRequestReview,
  onSuggestion,
  onReaction,
  onNewDocumentComment,
  onCopyLink,
  onExport,
  highlightId = null,
  agentName = "Code Agent",
}: CommentsPanelProps) {
  const { t, i18n } = useTranslation();
  const [groupTarget, setGroupTarget] = useState<Comment | null>(null);
  const [replyTo, setReplyTo] = useState<string | null>(null);
  const [replyText, setReplyText] = useState("");
  const [replyError, setReplyError] = useState<string | null>(null);
  const [replyBusy, setReplyBusy] = useState(false);
  const [replyDiscussions, setReplyDiscussions] = useState<string[]>([]);
  const [replyDiscussionsReady, setReplyDiscussionsReady] = useState(false);
  const [replyTemplateId, setReplyTemplateId] = useState("");
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const [statusFilter, setStatusFilter] = useState<"all" | "awaiting" | "agent">(
    "all",
  );
  const [query, setQuery] = useState("");
  const [summaryHtml, setSummaryHtml] = useState("");
  const [summaryLoading, setSummaryLoading] = useState(false);
  const [summaryExpanded, setSummaryExpanded] = useState(false);
  const summaryRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const summary = review?.summary?.trim();
    if (!summary) {
      setSummaryHtml("");
      setSummaryLoading(false);
      setSummaryExpanded(false);
      return;
    }
    setSummaryExpanded(false);
    if (!docPath) {
      setSummaryHtml("");
      setSummaryLoading(false);
      return;
    }
    let alive = true;
    setSummaryLoading(true);
    renderPreview(docPath, summary, true)
      .then((result) => {
        if (alive) setSummaryHtml(result.html);
      })
      .catch(() => {
        if (alive) setSummaryHtml("");
      })
      .finally(() => {
        if (alive) setSummaryLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [docPath, review?.summary]);

  // Keep normal clicks on the in-app handler while making middle-click and
  // "open in new tab" resolve through the app's ?file= document URL.
  useEffect(() => {
    const article = summaryRef.current;
    if (!article) return;
    article.querySelectorAll<HTMLAnchorElement>("a[href]").forEach((anchor) => {
      const original = anchor.getAttribute("href") ?? "";
      if (!original || original.startsWith("#")) return;
      anchor.dataset.mdHref = original;
      if (isExternalHref(original)) {
        anchor.target = "_blank";
        anchor.rel = "noopener noreferrer";
        return;
      }
      if (/^[a-z][a-z0-9+.-]*:/i.test(original)) return;
      const resolved = resolveRelative(docPath ?? null, original);
      if (!resolved) return;
      anchor.href = isMarkdownPath(resolved)
        ? `?file=${encodeURIComponent(resolved)}`
        : rawUrl(resolved);
      anchor.target = "_blank";
      anchor.rel = "noopener noreferrer";
    });
  }, [summaryHtml, docPath]);

  const handleSummaryClick = (event: MouseEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement | null;
    const anchor = target?.closest<HTMLAnchorElement>("a");
    if (!anchor) return;
    const original =
      anchor.dataset.mdHref ?? anchor.getAttribute("href") ?? "";
    if (!original || original.startsWith("#")) return;
    if (
      original.startsWith("//") ||
      /^(https?:|mailto:)/i.test(original)
    ) {
      event.preventDefault();
      window.open(original, "_blank", "noopener,noreferrer");
      return;
    }
    if (/^[a-z][a-z0-9+.-]*:/i.test(original)) {
      event.preventDefault();
      return;
    }
    event.preventDefault();
    const resolved = resolveRelative(docPath ?? null, original);
    if (!resolved) return;
    if (isMarkdownPath(resolved)) {
      onOpenDoc(resolved);
    } else {
      window.open(rawUrl(resolved), "_blank", "noopener,noreferrer");
    }
  };

  useEffect(() => {
    if (!highlightId) return;
    const node = document.querySelector(
      `[data-comment-id="${CSS.escape(highlightId)}"]`,
    );
    if (!node) return;
    node.scrollIntoView({ block: "center", behavior: "smooth" });
    node.classList.add("highlighted");
    const timer = window.setTimeout(() => node.classList.remove("highlighted"), 2600);
    return () => window.clearTimeout(timer);
  }, [highlightId, comments, statusFilter, query]);
  // If the discussion list finishes loading while a reply form is open, seed
  // its selection from the thread's stored references so the checkboxes match
  // what the server will keep.
  useEffect(() => {
    if (!replyTo || replyDiscussionsReady || !discussionsLoaded) return;
    const target = comments.find((comment) => comment.id === replyTo);
    if (!target) return;
    const available = new Set(discussions.map((item) => item.id));
    setReplyDiscussions(
      (target.discussionIds ?? []).filter((id) => available.has(id)),
    );
    setReplyDiscussionsReady(true);
  }, [comments, discussions, discussionsLoaded, replyDiscussionsReady, replyTo]);
  const lastCommentAuthor = (comment: Comment) =>
    comment.messages[comment.messages.length - 1]?.author;
  const awaitingCount = comments.filter(
    (comment) =>
      !comment.resolved &&
      (lastCommentAuthor(comment) === "agent" ||
        lastCommentAuthor(comment) === "system"),
  ).length;
  const agentCount = comments.filter(
    (comment) => !comment.resolved && lastCommentAuthor(comment) === "user",
  ).length;
  const filteredByStatus = comments.filter((comment) => {
    if (statusFilter === "awaiting") {
      return (
        !comment.resolved &&
        (lastCommentAuthor(comment) === "agent" ||
          lastCommentAuthor(comment) === "system")
      );
    }
    if (statusFilter === "agent") {
      return !comment.resolved && lastCommentAuthor(comment) === "user";
    }
    return true;
  });
  const normalizedQuery = query.trim().toLowerCase();
  const visibleComments = normalizedQuery
    ? filteredByStatus.filter((comment) => {
        const text = [
          comment.quote,
          String(comment.line),
          ...comment.messages.map((message) => message.body),
        ]
          .join(" ")
          .toLowerCase();
        return text.includes(normalizedQuery);
      })
    : filteredByStatus;
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
      setReplyDiscussions([]);
      setReplyTemplateId("");
    }
  };

  const toggleAll = () => {
    if (allExpanded) {
      setExpanded(new Set());
      setReplyTo(null);
      setReplyText("");
      setReplyError(null);
      setReplyDiscussions([]);
      setReplyTemplateId("");
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
      setReplyError(t("请输入要继续修改的内容"));
      return;
    }
    setReplyBusy(true);
    setReplyError(null);
    try {
      await onReply(
        comment,
        replyText.trim(),
        replyDiscussionsReady ? replyDiscussions : undefined,
        replyTemplateId,
      );
      setReplyTo(null);
      setReplyText("");
      setReplyDiscussions([]);
      setReplyTemplateId("");
    } catch (err) {
      setReplyError(err instanceof Error ? err.message : t("回复失败"));
    } finally {
      setReplyBusy(false);
    }
  };
  return (
    <aside className="comments-panel">
      <div className="comments-head">
        <div className="comments-title">
          <IconMessage size={15} />
          {t("评论")}
          <span className="comments-count">
            {t("{{unresolved}} 未解决 / {{total}}", {
              unresolved,
              total: comments.length,
            })}
          </span>
        </div>
        <div className="comments-head-actions">
          <Tooltip title={t("导出评论")}>
            <Button
              size="small"
              type="text"
              onClick={onExport}
              icon={<IconDownload size={14} />}
              aria-label={t("导出评论")}
            />
          </Tooltip>
          <Button
            size="small"
            type="text"
            onClick={onNewDocumentComment}
            icon={<IconMessage size={13} />}
          >
            {t("全文评论")}
          </Button>
          {comments.length > 0 ? (
            <Button size="small" type="text" onClick={toggleAll}>
              {allExpanded ? t("全部收起") : t("全部展开")}
            </Button>
          ) : null}
          <Tooltip title={t("关闭评论")}>
            <Button
              size="small"
              type="text"
              onClick={onClose}
              aria-label={t("关闭评论")}
              icon={<IconX size={15} />}
            />
          </Tooltip>
        </div>
      </div>
      <div className="review-summary">
        <div className="review-summary-head">
          <span className="review-summary-title">
            <IconSparkles size={13} />
            {t("Agent 审阅")}
          </span>
          <Button
            size="small"
            type="primary"
            ghost
            loading={reviewing}
            onClick={onRequestReview}
            disabled={reviewing}
            icon={<IconSparkles size={12} />}
          >
            {reviewing ? t("审阅中…") : t("让 Agent 审一遍")}
          </Button>
        </div>
        {review?.summary ? (
          <>
            <div
              ref={summaryRef}
              className={`review-summary-body markdown-body${
                summaryExpanded ? " expanded" : ""
              }`}
              onClick={handleSummaryClick}
            >
              {summaryHtml ? (
                <div dangerouslySetInnerHTML={{ __html: summaryHtml }} />
              ) : (
                <div className="review-summary-fallback">
                  {summaryLoading ? t("正在渲染摘要…") : review.summary}
                </div>
              )}
            </div>
            {review.summary.length > 240 ? (
              <button
                type="button"
                className="review-summary-toggle"
                aria-expanded={summaryExpanded}
                onClick={() => setSummaryExpanded((value) => !value)}
              >
                {summaryExpanded ? t("收起摘要") : t("展开摘要")}
                {summaryExpanded ? (
                  <IconChevronDown size={12} />
                ) : (
                  <IconChevronRight size={12} />
                )}
              </button>
            ) : null}
          </>
        ) : (
          <div className="review-summary-empty">
            {t("让 Agent 通读全文，留下分级评论和审阅摘要。")}
          </div>
        )}
      </div>
      {docPath ? <ReviewChecklist path={docPath} /> : null}
      <div className="comments-filter">
        <Segmented
          size="small"
          block
          value={statusFilter}
          onChange={(value) =>
            setStatusFilter(value as "all" | "awaiting" | "agent")
          }
          options={[
            { value: "all", label: `${t("全部")} ${comments.length}` },
            { value: "awaiting", label: `${t("待我确认")} ${awaitingCount}` },
            { value: "agent", label: `${t("等 Agent")} ${agentCount}` },
          ]}
        />
      </div>
      <Input
        className="comments-search-input"
        allowClear
        prefix={<IconSearch size={12} />}
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        placeholder={t("搜索评论内容或行号")}
        aria-label={t("搜索评论内容或行号")}
      />
      <div className="comments-body">
        {comments.length === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={t("还没有评论，在预览中选择文字后点击评论即可添加。")}
          />
        ) : visibleComments.length === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={
              normalizedQuery
                ? t("没有匹配的评论")
                : t("该状态下没有评论")
            }
          />
        ) : (
          visibleComments.map((comment) => {
            const members = groupMembers(comment);
            const lastMessage = comment.messages[comment.messages.length - 1];
            const isExpanded =
              expanded.has(comment.id) || replyTo === comment.id;
            const lastAuthor =
              lastMessage?.author === "agent"
                ? agentName
                : lastMessage?.author === "system"
                  ? t("版本回滚")
                  : t("你");
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
                  <Button
                    size="small"
                    type="text"
                    className="comment-expand"
                    onClick={() => toggleExpanded(comment.id)}
                    aria-expanded={isExpanded}
                    aria-label={isExpanded ? t("收起评论") : t("展开评论")}
                    title={isExpanded ? t("收起评论") : t("展开评论")}
                    icon={
                      isExpanded ? (
                        <IconChevronDown size={12} />
                      ) : (
                        <IconChevronRight size={12} />
                      )
                    }
                  />
                  {comment.scope === "document" ? (
                    <span className="comment-scope-badge">{t("全文评论")}</span>
                  ) : comment.orphaned || comment.line === 0 ? (
                    <span className="comment-orphaned">{t("未定位")}</span>
                  ) : (
                    <span>{t("第 {{line}} 行", { line: comment.line })}</span>
                  )}
                  {comment.resolved ? (
                    <span className="comment-resolved-badge">{t("已解决")}</span>
                  ) : null}
                  {members.length > 0 ? (
                    <span className="comment-group-badge">
                      <IconLink size={9} />
                      {t("{{count}} 条关联", { count: members.length + 1 })}
                    </span>
                  ) : null}
                  {comment.severity ? (
                    <SeverityBadge severity={comment.severity} />
                  ) : null}
                </div>
                <div className="comment-actions">
                  {comment.scope !== "document" ? (
                    <Button
                      size="small"
                      type="text"
                      className="comment-action"
                      onClick={() => onJump(comment)}
                      title={t("在预览中定位")}
                      icon={<IconLocate size={11} />}
                    >
                      {t("定位")}
                    </Button>
                  ) : null}
                  <Button
                    size="small"
                    type="text"
                    className="comment-action"
                    onClick={() => setGroupTarget(comment)}
                    title={comment.groupId ? t("管理关联评论") : t("关联其他评论")}
                    aria-label={comment.groupId ? t("管理关联评论") : t("关联其他评论")}
                    icon={<IconLink size={11} />}
                  >
                    {t("关联评论")}
                  </Button>
                  <Button
                    size="small"
                    type="text"
                    className="comment-action"
                    onClick={() => onCopyLink(comment)}
                    title={t("复制评论链接")}
                    icon={<IconLink size={11} />}
                  >
                    {t("链接")}
                  </Button>
                  <Button
                    size="small"
                    type="text"
                    className="comment-action"
                    onClick={() => onResolve(comment, !comment.resolved)}
                    title={comment.resolved ? t("重新打开") : t("标记解决")}
                    icon={<IconCheck size={12} />}
                  >
                    {comment.resolved ? t("重新打开") : t("解决")}
                  </Button>
                  <Button
                    size="small"
                    type="text"
                    className="comment-action danger"
                    onClick={() => onDelete(comment)}
                    title={t("删除评论")}
                    aria-label={t("删除评论")}
                    icon={<IconTrash size={12} />}
                  />
                </div>
              </div>

              {comment.scope !== "document" ? (
                <blockquote
                  className="comment-anchor"
                  onClick={() => onJump(comment)}
                  title={t("点击定位")}
                >
                  {comment.quote}
                </blockquote>
              ) : null}

              <div className="comment-reactions">
                {["👍", "✅"].map((emoji) => (
                  <button
                    key={emoji}
                    type="button"
                    className={`comment-reaction${comment.reaction === emoji ? " active" : ""}`}
                    onClick={() =>
                      onReaction(comment, comment.reaction === emoji ? "" : emoji)
                    }
                    title={emoji === "👍" ? t("赞同") : t("确认")}
                    aria-pressed={comment.reaction === emoji}
                  >
                    {emoji}
                  </button>
                ))}
              </div>

              {comment.suggestion ? (
                <div className={`comment-suggestion ${comment.suggestion.status}`}>
                  <div className="comment-suggestion-head">
                    <span>
                      {comment.suggestion.mode === "append"
                        ? t("建议追加")
                        : t("建议修改")}
                    </span>
                    {comment.suggestion.status === "pending" ? null : (
                      <span className="comment-suggestion-status">
                        {comment.suggestion.status === "accepted"
                          ? t("已接受")
                          : t("已拒绝")}
                      </span>
                    )}
                  </div>
                  <div className="comment-suggestion-diff">
                    {comment.suggestion.mode === "replace" ? (
                      <div className="suggestion-line removed">
                        <span>−</span>
                        {comment.suggestion.quote}
                      </div>
                    ) : (
                      <div className="suggestion-line context">
                        {comment.suggestion.quote}
                      </div>
                    )}
                    <div className="suggestion-line added">
                      <span>+</span>
                      {comment.suggestion.text}
                    </div>
                  </div>
                  {comment.suggestion.status === "pending" ? (
                    <div className="comment-suggestion-actions">
                      <Button
                        size="small"
                        type="primary"
                        className="suggestion-accept"
                        onClick={() => onSuggestion(comment, true)}
                      >
                        {t("接受建议")}
                      </Button>
                      <Button
                        size="small"
                        className="suggestion-reject"
                        onClick={() => onSuggestion(comment, false)}
                      >
                        {t("拒绝")}
                      </Button>
                    </div>
                  ) : null}
                </div>
              ) : null}

              {isExpanded && members.length > 0 ? (
                <div className="comment-related">
                  <div className="comment-related-head">
                    <span>
                      <IconLink size={10} />
                      {t("关联 {{count}} 条评论", { count: members.length + 1 })}
                    </span>
                    <button
                      type="button"
                      className="comment-related-manage"
                      onClick={() => setGroupTarget(comment)}
                    >
                      {t("管理")}
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
                          {member.line > 0 ? t("第 {{line}} 行", { line: member.line }) : t("未定位")}
                        </span>
                        {member.resolved ? <em>{t("已解决")}</em> : null}
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
                              ? `${agentName}${roundForMessage > 1 ? ` · ${t("第 {{round}} 次修改", { round: roundForMessage })}` : ""}`
                              : message.author === "system"
                                ? t("版本回滚")
                                : t("你")}
                          </span>
                          <span>{formatTime(message.createdAt, i18n.language)}</span>
                        </div>
                        <div className="comment-message-body">{message.body}</div>
                        {message.before || message.after ? (
                          <div className="comment-change">
                            <div className="comment-change-head">{t("变更内容")}</div>
                            {message.before ? (
                              <div className="comment-diff before">
                                <div className="comment-diff-label">
                                  <span className="diff-badge">−</span>
                                  {message.author === "system" ? t("回滚前") : t("修改前")}
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
                                  {message.author === "system" ? t("恢复后") : t("修改后")}
                                </div>
                                <pre>{message.after}</pre>
                              </div>
                            ) : null}
                          </div>
                        ) : null}
                        {message.author === "agent" &&
                        (message.versionId || message.before) ? (
                          <div className="comment-version">
                            <span>{t("此轮快照")}</span>
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
                                  title={t("只把评论段落恢复为这一轮修改前的内容")}
                                >
                                  <IconHistory size={11} />
                                  {t("回滚前")}
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
                                  title={t("只把评论段落恢复为这一轮修改后的内容")}
                                >
                                  <IconHistory size={11} />
                                  {t("回滚后")}
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
                  aria-label={t("展开评论")}
                >
                  <span className="comment-collapsed-count">
                    {t("{{count}} 条讨论", { count: comment.messages.length })}
                  </span>
                  <span className="comment-collapsed-text">
                    {lastMessage
                      ? `${lastAuthor}: ${lastMessage.body.replace(/\s+/g, " ").trim()}`
                      : t("展开评论")}
                  </span>
                  <IconChevronRight size={11} />
                </button>
              ) : null}

              {comment.promptTemplateId ? (
                <div className="comment-discussion-refs">
                  {t("任务类型：")}
                  {promptTemplates.find((item) => item.id === comment.promptTemplateId)
                    ?.name ? t(promptTemplates.find((item) => item.id === comment.promptTemplateId)?.name || "") : t("自定义模板")}
                </div>
              ) : null}

              {comment.discussionIds?.length ? (
                <div className="comment-discussion-refs">
                  {t("已参考讨论：")}
                  {comment.discussionIds
                    .map((id) => discussions.find((item) => item.id === id)?.title)
                    .filter(Boolean)
                    .join(", ") || t("{{count}} 个会话", { count: comment.discussionIds.length })}
                </div>
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
                  ? t("已确认解决")
                  : lastMessage?.author === "user"
                    ? t("等待 {{agent}} 继续修改", { agent: agentName })
                    : lastMessage?.author === "system"
                      ? t("已局部回滚，等待你的确认")
                      : t("等待你的确认，或继续回复 Agent")}
              </div>

              {!comment.resolved &&
              lastMessage?.author === "user" &&
              replyTo !== comment.id ? (
                <Button
                  size="small"
                  className="comment-agent-trigger"
                  onClick={() => void onTrigger(comment)}
                >
                  {t("让 Agent 处理")}
                </Button>
              ) : null}

              {replyTo === comment.id ? (
                <form
                  className="comment-reply-form"
                  onSubmit={(event) => void submitReply(event, comment)}
                >
                  <Input.TextArea
                    value={replyText}
                    onChange={(event) => setReplyText(event.target.value)}
                    placeholder={t("说明还需要修改什么，发送给 {{agent}}…", {
                      agent: agentName,
                    })}
                    rows={3}
                    autoFocus
                  />
                  {promptTemplates.length > 0 ? (
                    <label className="comment-template-field">
                      <span>{t("任务类型")}</span>
                      <Select
                        value={replyTemplateId}
                        onChange={setReplyTemplateId}
                        options={promptTemplates.map((item) => ({
                          value: item.id,
                          label: `${item.name}${item.allowDocumentEdit ? "" : t("（只分析）")}`,
                        }))}
                      />
                    </label>
                  ) : null}
                  <DiscussionRefPicker
                    discussions={discussions}
                    selected={replyDiscussions}
                    onChange={(ids) => {
                      setReplyDiscussions(ids);
                      // Once the user edits the picker, send the explicit
                      // selection even if the list loaded after the form opened.
                      setReplyDiscussionsReady(true);
                    }}
                  />
                  {replyError ? (
                    <p className="comment-reply-error">{replyError}</p>
                  ) : null}
                  <div className="comment-reply-actions">
                    <Button
                      size="small"
                      type="text"
                      onClick={() => {
                        setReplyTo(null);
                        setReplyText("");
                        setReplyError(null);
                        setReplyDiscussions([]);
                        setReplyTemplateId("");
                      }}
                    >
                      {t("取消")}
                    </Button>
                    <Button
                      size="small"
                      type="primary"
                      htmlType="submit"
                      loading={replyBusy}
                      disabled={replyBusy}
                    >
                      {replyBusy ? t("发送中…") : t("发送给 Agent")}
                    </Button>
                  </div>
                </form>
              ) : (
                <Button
                  size="small"
                  className="comment-reply-trigger"
                  icon={<IconMessage size={12} />}
                  onClick={() => {
                    setExpanded((current) => new Set(current).add(comment.id));
                    setReplyTo(comment.id);
                    setReplyText("");
                    setReplyError(null);
                    // Start from the thread's current context: sending an empty
                    // list would otherwise clear the linked discussions the
                    // user never touched. Deleted discussions/templates are
                    // dropped because the server rejects stale ids.
                    const available = new Set(discussions.map((item) => item.id));
                    setReplyDiscussions(
                      discussionsLoaded
                        ? (comment.discussionIds ?? []).filter((id) => available.has(id))
                        : [],
                    );
                    setReplyDiscussionsReady(discussionsLoaded);
                    const commentTemplate = promptTemplates.find(
                      (item) => item.id === comment.promptTemplateId,
                    );
                    const preferredTemplate =
                      commentTemplate?.allowDocumentEdit
                        ? commentTemplate
                        : promptTemplates.find((item) => item.allowDocumentEdit) ??
                          commentTemplate ??
                          promptTemplates[0];
                    setReplyTemplateId(preferredTemplate?.id ?? "");
                  }}
                >
                  {comment.resolved ? t("有新的修改意见") : t("继续修改 / 回复")}
                </Button>
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
