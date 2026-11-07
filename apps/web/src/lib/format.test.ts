import { describe, expect, it } from "vitest";
import { initials, safeNext, slugify, SLUG_PATTERN } from "./format";

describe("initials", () => {
  it("takes first letters of up to two words", () => {
    expect(initials("ada lovelace byron")).toBe("AL");
    expect(initials("  Grace  ")).toBe("G");
  });

  it("falls back to ? for blank names", () => {
    expect(initials("   ")).toBe("?");
  });
});

describe("slugify", () => {
  it("produces API-valid slugs", () => {
    for (const input of ["Acme Corp", "  --Hello, World!--  ", "Café Ünïcode", "a__b"]) {
      expect(slugify(input)).toMatch(SLUG_PATTERN);
    }
    expect(slugify("Acme Corp")).toBe("acme-corp");
    expect(slugify("Café")).toBe("cafe");
  });

  it("caps length at 100", () => {
    expect(slugify("a".repeat(150))).toHaveLength(100);
  });
});

describe("safeNext", () => {
  it("allows same-site relative paths", () => {
    expect(safeNext("/orgs/123?tab=a")).toBe("/orgs/123?tab=a");
  });

  it.each([null, undefined, "", "https://evil.com", "//evil.com", "/\\evil.com", "orgs"])(
    "rejects %s",
    (next) => {
      expect(safeNext(next)).toBe("/orgs");
    },
  );
});
