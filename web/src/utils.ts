import type { DocStats } from "./types";

export function baseName(path: string): string {
  const parts = path.split("/");
  return parts[parts.length - 1] ?? path;
}

export function dirName(path: string | null): string {
  if (!path) return "";
  const parts = path.split("/");
  parts.pop();
  return parts.join("/");
}

export function isMarkdownPath(path: string): boolean {
  return /\.(md|markdown|mdown|mkd)$/i.test(path);
}

export function resolveRelative(
  currentPath: string | null,
  href: string,
): string {
  let cleaned = href.split("#")[0]?.split("?")[0] ?? "";
  try {
    cleaned = decodeURIComponent(cleaned);
  } catch {
    // Keep the raw value when it is not valid percent-encoding.
  }
  const absolute = cleaned.startsWith("/");
  const parts = absolute || !currentPath ? [] : dirName(currentPath).split("/");
  for (const segment of cleaned.replace(/^\/+/, "").split("/")) {
    if (!segment || segment === ".") continue;
    if (segment === "..") {
      parts.pop();
      continue;
    }
    parts.push(segment);
  }
  return parts.filter(Boolean).join("/");
}

export function rawUrl(path: string): string {
  return `/raw/${path.split("/").map(encodeURIComponent).join("/")}`;
}

export function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

export function parentDir(path: string): string {
  const trimmed = path.replace(/\/+$/, "");
  if (!trimmed || trimmed === "/") return "/";
  const slash = trimmed.lastIndexOf("/");
  return slash <= 0 ? "/" : trimmed.slice(0, slash);
}

export function withMarkdownExtension(name: string): string {
  const trimmed = name.trim().replace(/^\/+/, "");
  if (isMarkdownPath(trimmed)) return trimmed;
  return `${trimmed}.md`;
}

export function computeStats(text: string): DocStats {
  const chars = Array.from(text).length;
  const lines = text.length === 0 ? 1 : text.split(/\r\n|\r|\n/).length;
  const cjk = text.match(/[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]/g);
  const latin = text.match(/[A-Za-z0-9_][A-Za-z0-9_'-]*/g);
  return {
    chars,
    lines,
    words: (cjk?.length ?? 0) + (latin?.length ?? 0),
  };
}

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function textNodes(root: HTMLElement): { node: Text; start: number; end: number }[] {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      const parent = node.parentElement;
      if (!parent) return NodeFilter.FILTER_REJECT;
      if (parent.closest(".commentloop-copy")) return NodeFilter.FILTER_REJECT;
      if (parent.closest("script,style")) return NodeFilter.FILTER_REJECT;
      return NodeFilter.FILTER_ACCEPT;
    },
  });
  const out: { node: Text; start: number; end: number }[] = [];
  let offset = 0;
  let current = walker.nextNode();
  while (current) {
    const text = current as Text;
    const length = text.data.length;
    out.push({ node: text, start: offset, end: offset + length });
    offset += length;
    current = walker.nextNode();
  }
  return out;
}

interface TextIndex {
  full: string;
  positions: { node: Text; offset: number }[];
}

function isSpace(code: number): boolean {
  return (
    code === 32 ||
    (code >= 9 && code <= 13) ||
    code === 160 ||
    code === 0x1680 ||
    (code >= 0x2000 && code <= 0x200a) ||
    code === 0x2028 ||
    code === 0x2029 ||
    code === 0x202f ||
    code === 0x205f ||
    code === 0x3000 ||
    code === 0xfeff
  );
}

// Whitespace-collapsed text of `root` with a map back to DOM positions.
function buildTextIndex(root: HTMLElement): TextIndex {
  const parts: string[] = [];
  const positions: { node: Text; offset: number }[] = [];
  let inWhitespace = false;
  for (const item of textNodes(root)) {
    const data = item.node.data;
    inWhitespace = false;
    for (let i = 0; i < data.length; i += 1) {
      if (isSpace(data.charCodeAt(i))) {
        if (!inWhitespace) {
          parts.push(" ");
          positions.push({ node: item.node, offset: i });
        }
        inWhitespace = true;
      } else {
        parts.push(data[i]!);
        positions.push({ node: item.node, offset: i });
        inWhitespace = false;
      }
    }
  }
  return { full: parts.join(""), positions };
}

function rangeInIndex(index: TextIndex, quote: string): Range | null {
  const needle = quote.replace(/\s+/g, " ").trim();
  if (!needle) return null;
  const start = index.full.indexOf(needle);
  if (start < 0) return null;
  const startPos = index.positions[start];
  const endPos = index.positions[start + needle.length - 1];
  if (!startPos || !endPos) return null;
  try {
    const range = document.createRange();
    range.setStart(startPos.node, startPos.offset);
    range.setEnd(endPos.node, endPos.offset + 1);
    return range;
  } catch {
    return null;
  }
}

export function findQuoteRange(
  root: HTMLElement | null,
  quote: string,
): Range | null {
  if (!root || !quote) return null;
  return rangeInIndex(buildTextIndex(root), quote);
}

/** Resolves many quotes against one text index (one DOM walk in total). */
export function findQuoteRanges(
  root: HTMLElement | null,
  quotes: string[],
): (Range | null)[] {
  if (!root || quotes.length === 0) return quotes.map(() => null);
  const index = buildTextIndex(root);
  return quotes.map((quote) => (quote ? rangeInIndex(index, quote) : null));
}

export function scrollToQuote(
  container: HTMLElement | null,
  quote: string,
): boolean {
  if (!container) return false;
  const article = container.querySelector<HTMLElement>(".preview-article");
  const range = findQuoteRange(article, quote);
  if (!range) return false;
  const rect = range.getBoundingClientRect();
  const box = container.getBoundingClientRect();
  const top = container.scrollTop + rect.top - box.top - 90;
  container.scrollTo({ top: Math.max(0, top), behavior: "smooth" });
  return true;
}

/** Expand a selected quote to the Markdown paragraph (blank-line block) that
 * contains it; used for block-level comments. */
export function expandQuoteToBlock(content: string, quote: string): string {
  const needle = quote.trim();
  if (!needle) return quote;
  const index = content.indexOf(needle);
  if (index < 0) return quote;
  const before = content.lastIndexOf("\n\n", index);
  const start = before < 0 ? 0 : before + 2;
  const after = content.indexOf("\n\n", index + needle.length);
  const end = after < 0 ? content.length : after;
  return content.slice(start, end).trim();
}

export interface MarkdownHeading {
  level: number;
  text: string;
}

/** Parse ATX headings outside fenced code blocks for the outline menu. */
export function parseMarkdownHeadings(content: string): MarkdownHeading[] {
  const headings: MarkdownHeading[] = [];
  let fenced = false;
  for (const line of content.split("\n")) {
    if (/^\s*```/.test(line)) {
      fenced = !fenced;
      continue;
    }
    if (fenced) continue;
    const match = /^(#{1,6})\s+(.+?)\s*#*\s*$/.exec(line);
    if (match) headings.push({ level: match[1]!.length, text: match[2]!.trim() });
  }
  return headings;
}
