export type DiffLine = {
  kind: "same" | "add" | "remove";
  text: string;
};

const MAX_DIFF_LINES = 1200;

/** Minimal line diff for version previews; large documents fall back to a
 * prefix/suffix split instead of an O(n*m) table. */
export function diffLines(before: string, after: string): DiffLine[] {
  const a = before.split("\n");
  const b = after.split("\n");
  if (a.length > MAX_DIFF_LINES || b.length > MAX_DIFF_LINES) {
    return coarseDiff(a, b);
  }
  const table: Uint16Array[] = Array.from(
    { length: a.length + 1 },
    () => new Uint16Array(b.length + 1),
  );
  for (let i = a.length - 1; i >= 0; i -= 1) {
    for (let j = b.length - 1; j >= 0; j -= 1) {
      table[i]![j] =
        a[i] === b[j]
          ? table[i + 1]![j + 1]! + 1
          : Math.max(table[i + 1]![j]!, table[i]![j + 1]!);
    }
  }
  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      out.push({ kind: "same", text: a[i]! });
      i += 1;
      j += 1;
    } else if (table[i + 1]![j]! >= table[i]![j + 1]!) {
      out.push({ kind: "remove", text: a[i]! });
      i += 1;
    } else {
      out.push({ kind: "add", text: b[j]! });
      j += 1;
    }
  }
  while (i < a.length) {
    out.push({ kind: "remove", text: a[i]! });
    i += 1;
  }
  while (j < b.length) {
    out.push({ kind: "add", text: b[j]! });
    j += 1;
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
