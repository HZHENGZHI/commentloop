import type { Comment, DocReview } from "./types";
import { baseName } from "./utils";

export function buildGitHubMarkdown(
  path: string,
  review: DocReview | undefined,
  comments: Comment[],
): string {
  const lines = [
    `## Commentloop review — ${baseName(path)}`,
    "",
    `Path: \`${path}\``,
    `Status: ${review?.status ?? "unknown"}`,
  ];
  if (review?.summary) {
    lines.push("", `**Summary:** ${review.summary}`);
  }
  lines.push("", "### Comments");
  for (const comment of comments) {
    const where =
      comment.scope === "document" ? "Full document" : `Line ${comment.line}`;
    const meta = [
      comment.severity ? `\`${comment.severity}\`` : "",
      comment.resolved ? "resolved" : "",
    ]
      .filter(Boolean)
      .join(" · ");
    lines.push("", `- **${where}**${meta ? ` — ${meta}` : ""}`);
    if (comment.quote) {
      lines.push(`  > ${comment.quote.replace(/\n/g, "\n  > ")}`);
    }
    for (const message of comment.messages) {
      lines.push("", `  **${message.author}:** ${message.body}`);
    }
    if (comment.suggestion) {
      lines.push(
        "",
        `  Suggestion (${comment.suggestion.mode}):`,
        "  ```diff",
        `  - ${comment.suggestion.quote.replace(/\n/g, "\n  - ")}`,
        `  + ${comment.suggestion.text.replace(/\n/g, "\n  + ")}`,
        "  ```",
      );
    }
  }
  return lines.join("\n");
}
