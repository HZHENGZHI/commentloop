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

export function findQuoteRange(
  root: HTMLElement | null,
  quote: string,
): Range | null {
  if (!root || !quote) return null;
  const nodes = textNodes(root);
  let full = "";
  const positions: { node: Text; offset: number }[] = [];
  for (const item of nodes) {
    const data = item.node.data;
    let inWhitespace = false;
    for (let i = 0; i < data.length; i += 1) {
      const char = data[i]!;
      if (/\s/.test(char)) {
        if (!inWhitespace) {
          full += " ";
          positions.push({ node: item.node, offset: i });
        }
        inWhitespace = true;
      } else {
        full += char;
        positions.push({ node: item.node, offset: i });
        inWhitespace = false;
      }
    }
  }
  const needle = quote.replace(/\s+/g, " ").trim();
  if (!needle) return null;
  const index = full.indexOf(needle);
  if (index < 0) return null;
  const endIndex = index + needle.length - 1;
  const startPos = positions[index];
  const endPos = positions[endIndex];
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
