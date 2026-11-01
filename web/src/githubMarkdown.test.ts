import { describe, expect, it } from "vitest";
import { buildGitHubMarkdown } from "./githubMarkdown";
import type { Comment, DocReview } from "./types";

const review: DocReview = {
  path: "r1/review.md",
  status: "in_review",
  summary: "Overall direction is fine.",
  summaryUpdatedAt: "2026-01-01T00:00:00Z",
  updatedAt: "2026-01-01T00:00:00Z",
};

const comments: Comment[] = [
  {
    id: "c1",
    path: "r1/review.md",
    quote: "old text",
    anchor: "old text",
    groupId: "",
    prefix: "",
    suffix: "",
    line: 5,
    start: 0,
    end: 0,
    resolved: false,
    orphaned: false,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    severity: "blocking",
    messages: [
      {
        id: "m1",
        author: "agent",
        body: "Please tighten this.",
        createdAt: "2026-01-01T00:00:00Z",
      },
    ],
    suggestion: {
      quote: "old text",
      text: "new text",
      mode: "replace",
      status: "pending",
    },
  },
  {
    id: "c2",
    path: "r1/review.md",
    quote: "",
    groupId: "",
    prefix: "",
    suffix: "",
    line: 0,
    start: 0,
    end: 0,
    resolved: false,
    orphaned: false,
    createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z",
    scope: "document",
    messages: [
      {
        id: "m2",
        author: "user",
        body: "Whole-document note.",
        createdAt: "2026-01-01T00:00:00Z",
      },
    ],
  },
];

describe("buildGitHubMarkdown", () => {
  it("includes status, summary, inline and full-document comments", () => {
    const markdown = buildGitHubMarkdown("r1/review.md", review, comments);
    expect(markdown).toContain("Status: in_review");
    expect(markdown).toContain("Overall direction is fine.");
    expect(markdown).toContain("Line 5");
    expect(markdown).toContain("blocking");
    expect(markdown).toContain("Please tighten this.");
    expect(markdown).toContain("Suggestion (replace)");
    expect(markdown).toContain("Full document");
    expect(markdown).toContain("Whole-document note.");
  });
});
