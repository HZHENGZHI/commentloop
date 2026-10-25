import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type MouseEvent,
  type RefObject,
} from "react";
import { useTranslation } from "react-i18next";
import i18n from "../i18n";
import type { Comment, Theme } from "../types";
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
  theme: Theme;
}

export function Preview({
  html,
  currentPath,
  comments,
  containerRef,
  onScrollRatio,
  onOpenDoc,
  onRequestComment,
  theme,
}: PreviewProps) {
  const { t } = useTranslation();
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

  // Hydrate fenced Mermaid blocks into SVG. The Go renderer intentionally
  // leaves them as code blocks so a JS-disabled browser still shows the source.
  useEffect(() => {
    const article = articleRef.current;
    if (!article) return;
    let cancelled = false;
    const blocks = new Map<HTMLElement, string>();
    article.querySelectorAll<HTMLPreElement>("pre").forEach((pre) => {
      const code = pre.querySelector<HTMLElement>("code.language-mermaid");
      if (!code) return;
      const source = code.textContent ?? "";
      const block = document.createElement("div");
      block.className = "mermaid-block";
      block.dataset.source = source;
      block.dataset.renderedTheme = "";
      const diagram = document.createElement("div");
      diagram.className = "mermaid-diagram";
      block.appendChild(diagram);
      pre.replaceWith(block);
      blocks.set(block, source);
    });
    article.querySelectorAll<HTMLElement>(".mermaid-block").forEach((block) => {
      if (block.dataset.renderedTheme !== theme) {
        blocks.set(block, block.dataset.source ?? "");
      }
    });
    if (blocks.size === 0) return;
    void (async () => {
      try {
        const mermaid = (await import("mermaid")).default;
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: "strict",
          theme: "base",
          fontFamily:
            '-apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
          themeVariables:
            theme === "dark"
              ? {
                  background: "#12161b",
                  primaryColor: "#1d2838",
                  primaryTextColor: "#e6edf3",
                  primaryBorderColor: "#64748b",
                  secondaryColor: "#20262e",
                  secondaryTextColor: "#e6edf3",
                  secondaryBorderColor: "#475569",
                  tertiaryColor: "#171b21",
                  tertiaryTextColor: "#e6edf3",
                  lineColor: "#94a3b8",
                  textColor: "#e6edf3",
                  labelTextColor: "#e6edf3",
                  loopTextColor: "#e6edf3",
                  noteTextColor: "#e6edf3",
                  noteBkgColor: "#1d2838",
                  noteBorderColor: "#64748b",
                  activationBkgColor: "#20262e",
                  activationBorderColor: "#64748b",
                  sequenceNumberColor: "#e6edf3",
                  classText: "#e6edf3",
                  stateTextColor: "#e6edf3",
                  stateLabelColor: "#e6edf3",
                  transitionLabelColor: "#cbd5e1",
                  mainBkg: "#1d2838",
                  nodeBorder: "#64748b",
                  clusterBkg: "#111418",
                  clusterBorder: "#334155",
                  edgeLabelBackground: "#12161b",
                  titleColor: "#e6edf3",
                  actorBkg: "#1d2838",
                  actorBorder: "#64748b",
                  actorTextColor: "#e6edf3",
                  signalColor: "#94a3b8",
                  signalTextColor: "#e6edf3",
                }
              : {
                  background: "#ffffff",
                  primaryColor: "#f3f2ff",
                  primaryTextColor: "#1f2328",
                  primaryBorderColor: "#b9b7f4",
                  secondaryColor: "#f8f8fc",
                  secondaryTextColor: "#1f2328",
                  secondaryBorderColor: "#d5d3fa",
                  tertiaryColor: "#fbfbfd",
                  tertiaryTextColor: "#1f2328",
                  lineColor: "#6b7280",
                  textColor: "#1f2328",
                  labelTextColor: "#1f2328",
                  loopTextColor: "#1f2328",
                  noteTextColor: "#1f2328",
                  noteBkgColor: "#fafaff",
                  noteBorderColor: "#d5d3fa",
                  activationBkgColor: "#f8f8fc",
                  activationBorderColor: "#b9b7f4",
                  sequenceNumberColor: "#1f2328",
                  classText: "#1f2328",
                  stateTextColor: "#1f2328",
                  stateLabelColor: "#1f2328",
                  transitionLabelColor: "#374151",
                  mainBkg: "#f3f2ff",
                  nodeBorder: "#b9b7f4",
                  clusterBkg: "#fafaff",
                  clusterBorder: "#d5d3fa",
                  edgeLabelBackground: "#ffffff",
                  titleColor: "#1f2328",
                  actorBkg: "#f3f2ff",
                  actorBorder: "#b9b7f4",
                  actorTextColor: "#1f2328",
                  signalColor: "#6b7280",
                  signalTextColor: "#1f2328",
                },
          flowchart: {
            htmlLabels: false,
            curve: "basis",
            padding: 14,
            nodeSpacing: 34,
            rankSpacing: 46,
          },
        });
        let index = 0;
        for (const [block, source] of blocks) {
          if (cancelled) return;
          const diagram = block.querySelector<HTMLElement>(".mermaid-diagram");
          if (!diagram) continue;
          block.dataset.renderedTheme = theme;
          try {
            const { svg } = await mermaid.render(
              `commentloop-mermaid-${Date.now()}-${index++}`,
              source,
            );
            if (cancelled) return;
            diagram.innerHTML = svg;
            block.querySelector(".mermaid-error")?.remove();
          } catch (error) {
            if (cancelled) return;
            diagram.textContent = "";
            const failure = document.createElement("div");
            failure.className = "mermaid-error";
            failure.textContent =
              error instanceof Error
                ? error.message
                : i18n.t("Mermaid 渲染失败");
            const sourcePre = document.createElement("pre");
            sourcePre.className = "mermaid-error-source";
            sourcePre.textContent = source;
            diagram.append(failure, sourcePre);
          }
        }
      } catch (error) {
        // The source block remains visible if the dynamic import itself fails.
        console.error("commentloop: failed to load Mermaid", error);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [html, currentPath, theme]);

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
      if (pre.querySelector(":scope > code.language-mermaid")) return;
      pre.classList.add("commentloop-pre");
      const button = document.createElement("button");
      button.type = "button";
      button.className = "commentloop-copy";
      button.textContent = i18n.t("复制");
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
          <p className="preview-blank-title">{t("空白文档")}</p>
          <p>{t("开始输入 Markdown，预览会实时出现在这里。")}</p>
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
          {t("评论选中内容")}
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
  button.textContent = i18n.t("已复制");
  window.setTimeout(() => {
    button.textContent = i18n.t("复制");
  }, 1500);
}
