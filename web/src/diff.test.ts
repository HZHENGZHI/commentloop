import { describe, expect, it } from "vitest";
import { diffLines } from "./diff";

describe("diffLines", () => {
  it("marks added, removed and unchanged lines", () => {
    const diff = diffLines("a\nb\nc", "a\nB\nc");
    expect(diff.filter((line) => line.kind === "same").map((line) => line.text)).toEqual(["a", "c"]);
    expect(diff.some((line) => line.kind === "remove" && line.text === "b")).toBe(true);
    expect(diff.some((line) => line.kind === "add" && line.text === "B")).toBe(true);
  });
});
