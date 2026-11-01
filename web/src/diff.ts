import { diffArrays } from "diff";

export type DiffLine = {
  kind: "same" | "add" | "remove";
  text: string;
};

const MAX_DIFF_LINES = 1200;

/** Line diff for version previews. The diff package computes the LCS; very
 * large documents fall back to a prefix/suffix split to keep the modal
 * responsive. */
export function diffLines(before: string, after: string): DiffLine[] {
  const a = before.split("\n");
  const b = after.split("\n");
  if (a.length > MAX_DIFF_LINES || b.length > MAX_DIFF_LINES) {
    return coarseDiff(a, b);
  }
  const out: DiffLine[] = [];
  for (const part of diffArrays(a, b)) {
    const kind: DiffLine["kind"] = part.added
      ? "add"
      : part.removed
        ? "remove"
        : "same";
    for (const text of part.value) {
      out.push({ kind, text });
    }
  }
  return out;
}

function coarseDiff(a: string[], b: string[]): DiffLine[] {
  let prefix = 0;
  while (prefix < a.length && prefix < b.length && a[prefix] === b[prefix]) {
    prefix += 1;
  }
  let suffix = 0;
  while (
    suffix < a.length - prefix &&
    suffix < b.length - prefix &&
    a[a.length - 1 - suffix] === b[b.length - 1 - suffix]
  ) {
    suffix += 1;
  }
  const out: DiffLine[] = [];
  for (let i = 0; i < prefix; i += 1) out.push({ kind: "same", text: a[i]! });
  for (let i = prefix; i < a.length - suffix; i += 1) {
    out.push({ kind: "remove", text: a[i]! });
  }
  for (let i = prefix; i < b.length - suffix; i += 1) {
    out.push({ kind: "add", text: b[i]! });
  }
  for (let i = a.length - suffix; i < a.length; i += 1) {
    out.push({ kind: "same", text: a[i]! });
  }
  return out;
}
