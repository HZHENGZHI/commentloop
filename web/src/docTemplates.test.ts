import { describe, expect, it } from "vitest";
import { DOC_TEMPLATES } from "./docTemplates";

describe("DOC_TEMPLATES", () => {
  it("has blank and structured templates", () => {
    expect(DOC_TEMPLATES[0]?.id).toBe("blank");
    expect(DOC_TEMPLATES[0]?.content).toBe("");
    expect(DOC_TEMPLATES.map((item) => item.id)).toEqual(
      expect.arrayContaining(["prd", "adr", "weekly"]),
    );
    for (const template of DOC_TEMPLATES.slice(1)) {
      expect(template.content.trim().length).toBeGreaterThan(0);
    }
  });
});
