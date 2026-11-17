import { describe, expect, it } from "vitest";
import { prettyJsonText } from "../src/lib/prettyJson";

describe("prettyJsonText", () => {
  it("indents without changing numbers or strings", () => {
    expect(prettyJsonText('{"Price":100.50,"Note":"a, {b}: \\"c\\"","Tags":[],"X":[1,2]}')).toBe(
      '{\n  "Price": 100.50,\n  "Note": "a, {b}: \\"c\\"",\n  "Tags": [],\n  "X": [\n    1,\n    2\n  ]\n}',
    );
  });

  it("keeps broken JSON readable", () => {
    expect(prettyJsonText('{"Qty":')).toBe('{\n  "Qty": ');
  });
});
