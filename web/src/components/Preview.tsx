import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type MouseEvent,
  type RefObject,
} from "react";
import type { Comment } from "../types";
import {
  findQuoteRange,
  isMarkdownPath,
  rawUrl,
  resolveRelative,
} from "../utils";

interface PreviewProps {
  html: string;
  currentPath: string | null;
  comments: Comment[];
  containerRef: RefObject<HTMLDivElement | null>;
  onScrollRatio: (ratio: number) => void;
  onOpenDoc: (path: string) => void;
  onRequestComment: (quote: string) => void;
}

export function Preview({
  html,
  currentPath,
  comments,
  containerRef,
  onScrollRatio,
  onOpenDoc,
  onRequestComment,
}: PreviewProps) {
  const articleRef = useRef<HTMLElement>(null);
  const scrollPos = useRef(0);
  const [selection, setSelection] = useState<{
    quote: string;
    top: number;
    left: number;
  } | null>(null);

  const handleScroll = () => {
    setSelection(null);
    const el = containerRef.current;
    if (!el) return;
    scrollPos.current = el.scrollTop;
    const max = el.scrollHeight - el.clientHeight;
    onScrollRatio(max <= 0 ? 0 : el.scrollTop / max);
  };

  const captureSelection = () => {
    window.setTimeout(() => {
      const sel = window.getSelection();
      const article = articleRef.current;
      if (!sel || sel.isCollapsed || sel.rangeCount === 0 || !article) {
        setSelection(null);
        return;
      }
      const range = sel.getRangeAt(0);
      if (!article.contains(range.commonAncestorContainer)) {
        setSelection(null);
        return;
      }
      const quote = sel.toString().trim();
      if (!quote) {
        setSelection(null);
        return;
      }
      const rect = range.getBoundingClientRect();
      setSelection({
        quote: quote.slice(0, 4000),
        top: Math.min(window.innerHeight - 44, rect.bottom + 8),
        left: Math.min(
          window.innerWidth - 104,
          Math.max(8, rect.left + rect.width / 2 - 42),
        ),
      });
    }, 0);
  };

  useEffect(() => {
    const onSelectionChange = () => {
      const sel = window.getSelection();
      if (!sel || sel.isCollapsed) setSelection(null);
    };
    document.addEventListener("selectionchange", onSelectionChange);
    return () => document.removeEventListener("selectionchange", onSelectionChange);
  }, []);

  // Inline highlights for comment anchors (Chrome / Safari Custom Highlight API).
  useLayoutEffect(() => {
    const article = articleRef.current;
    const css = CSS as unknown as { highlights?: Map<string, unknown> };
    const HighlightCtor = (
      window as unknown as { Highlight?: new (...ranges: Range[]) => unknown }
    ).Highlight;
    if (!article || !css?.highlights || !HighlightCtor) return;

    const openRanges: Range[] = [];
    const resolvedRanges: Range[] = [];
    for (const comment of comments) {
      const range = findQuoteRange(article, comment.anchor || comment.quote);
      if (!range) continue;
      (comment.resolved ? resolvedRanges : openRanges).push(range);
    }
    const apply = (name: string, ranges: Range[]) => {
      if (ranges.length) {
        css.highlights?.set(name, new HighlightCtor(...ranges));
      } else {
        css.highlights?.delete(name);
      }
    };
    apply("commentloop-comment-open", openRanges);
    apply("commentloop-comment-resolved", resolvedRanges);
    return () => {
      css.highlights?.delete("commentloop-comment-open");
      css.highlights?.delete("commentloop-comment-resolved");
    };
  }, [html, comments, currentPath]);

  useLayoutEffect(() => {
    const el = containerRef.current;
    if (el) el.scrollTop = scrollPos.current;
  }, [html, containerRef]);

  // Make links work for both normal clicks and middle-click/open-in-new-tab.
  useEffect(() => {
    const article = articleRef.current;
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

      const resolved = resolveRelative(currentPath, original);
      if (!resolved) return;
      if (isMarkdownPath(resolved)) {
        anchor.href = `?file=${encodeURIComponent(resolved)}`;
      } else {
        anchor.href = rawUrl(resolved);
      }
      anchor.target = "_blank";
      anchor.rel = "noopener noreferrer";
    });

    // Add copy buttons to highlighted code blocks.
    article.querySelectorAll<HTMLPreElement>("pre").forEach((pre) => {
      if (pre.querySelector(":scope > .commentloop-copy")) return;
      pre.classList.add("commentloop-pre");
      const button = document.createElement("button");
      button.type = "button";
      button.className = "commentloop-copy";
      button.textContent = "复制";
      button.addEventListener("click", (event) => {
        event.preventDefault();
        event.stopPropagation();
        const text =
          pre.querySelector("code")?.textContent ?? pre.textContent ?? "";
        void copyText(text, button);
      });
      pre.appendChild(button);
    });
  }, [html, currentPath]);

  const handleClick = (event: MouseEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement | null;
    const anchor = target?.closest<HTMLAnchorElement>("a");
    if (!anchor) return;

    const original =
      anchor.dataset.mdHref ?? anchor.getAttribute("href") ?? "";
    if (!original || original.startsWith("#")) return;

    if (isExternalHref(original)) {
      event.preventDefault();
      window.open(original, "_blank", "noopener,noreferrer");
      return;
    }
    if (/^[a-z][a-z0-9+.-]*:/i.test(original)) {
      // Block javascript:, data:, file: and other unexpected schemes.
      event.preventDefault();
      return;
    }

    event.preventDefault();
    const resolved = resolveRelative(currentPath, original);
    if (!resolved) return;
    if (isMarkdownPath(resolved)) {
      onOpenDoc(resolved);
    } else {
      window.open(rawUrl(resolved), "_blank", "noopener,noreferrer");
    }
  };

  return (
    <div
      className="preview-scroll"
      ref={containerRef}
      onScroll={handleScroll}
      onClick={handleClick}
      onMouseUp={captureSelection}
      onKeyUp={captureSelection}
    >
      {html.trim() ? (
        <article
          ref={articleRef}
          className="markdown-body preview-article"
          dangerouslySetInnerHTML={{ __html: html }}
        />
      ) : (
        <div className="preview-blank">
          <p className="preview-blank-title">空白文档</p>
          <p>开始输入 Markdown，预览会实时出现在这里。</p>
        </div>
      )}
      {selection ? (
        <button
          type="button"
          className="selection-comment-btn"
          style={{ top: selection.top, left: selection.left }}
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => {
            onRequestComment(selection.quote);
            window.getSelection()?.removeAllRanges();
            setSelection(null);
          }}
        >
          评论
        </button>
      ) : null}
    </div>
  );
}

function isExternalHref(href: string): boolean {
  return /^(https?:|mailto:)/i.test(href) || href.startsWith("//");
}

async function copyText(text: string, button: HTMLButtonElement) {
  try {
    await navigator.clipboard.writeText(text);
  } catch {
    const area = document.createElement("textarea");
    area.value = text;
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    try {
      document.execCommand("copy");
    } finally {
      area.remove();
    }
  }
  button.textContent = "已复制";
  window.setTimeout(() => {
    button.textContent = "复制";
  }, 1500);
}
