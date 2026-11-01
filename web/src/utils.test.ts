import { describe, expect, it } from "vitest";
import { expandQuoteToBlock, parseMarkdownHeadings } from "./utils";

describe("parseMarkdownHeadings", () => {
  it("parses heading levels and skips fenced code", () => {
    const headings = parseMarkdownHeadings(
      "# 标题\n\n正文\n\n```md\n# not-a-heading\n```\n\n## 子标题\n### 三级",
    );
    expect(headings).toEqual([
      { level: 1, text: "标题" },
      { level: 2, text: "子标题" },
      { level: 3, text: "三级" },
    ]);
  });

  it("returns an empty list for plain text", () => {
    expect(parseMarkdownHeadings("hello\nworld")).toEqual([]);
  });
});

describe("expandQuoteToBlock", () => {
  it("expands a selection to its blank-line block", () => {
    const content = "# One\n\nfirst paragraph\nsecond line\n\nnext block\n";
    expect(expandQuoteToBlock(content, "second line")).toBe(
      "first paragraph\nsecond line",
    );
  });

  it("falls back to the original quote when it is missing", () => {
    expect(expandQuoteToBlock("hello", "missing")).toBe("missing");
  });
});
