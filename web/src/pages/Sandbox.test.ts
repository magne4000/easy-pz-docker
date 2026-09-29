import { describe, expect, it } from "vitest";
import type { Schemas } from "@/api/client";
import { choices, problem } from "./Sandbox";

const entry = (e: Partial<Schemas["SandboxEntryView"]>): Schemas["SandboxEntryView"] => ({
  key: "K",
  value: "1",
  kind: "int",
  description: "",
  default: "",
  options: [],
  readOnly: false,
  ...e,
});

describe("problem", () => {
  it("checks numbers against kind and range", () => {
    const f = entry({ kind: "float", min: 0, max: 4 });
    expect(problem(f, "2.5")).toBeNull();
    expect(problem(f, "4.5")).toMatch(/between 0 and 4/);
    expect(problem(f, "")).toMatch(/number/);
    expect(problem(entry({ kind: "int" }), "1.5")).toMatch(/whole/);
    expect(problem(entry({ kind: "string" }), "anything")).toBeNull();
  });
});

describe("choices", () => {
  it("labels undescribed options and keeps an unlisted current value", () => {
    const e = entry({
      value: "0",
      options: [
        { value: 1, label: "None" },
        { value: 2, label: "" },
      ],
    });
    expect(choices(e, "0")).toEqual([
      { value: "0", label: "0 (not described)" },
      { value: "1", label: "1 — None" },
      { value: "2", label: "2 (not described)" },
    ]);
    expect(choices(entry({}), "1")).toEqual([]);
  });
});
