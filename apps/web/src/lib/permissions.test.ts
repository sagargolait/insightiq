import { describe, expect, it } from "vitest";
import { assignableRoles, can, ROLES, type Permission, type Role } from "./permissions";

// Pins the full table. Must match services/api/internal/modules/authorization/
// permissions.go (and its permissions_test.go) — update all three together.
const expected: Record<Role, Permission[]> = {
  owner: [
    "organization:view",
    "organization:delete",
    "members:view",
    "members:manage",
    "members:manage_owners",
    "dashboards:view",
    "dashboards:create",
    "dashboards:edit",
    "integrations:manage",
  ],
  admin: [
    "organization:view",
    "members:view",
    "members:manage",
    "dashboards:view",
    "dashboards:create",
    "dashboards:edit",
    "integrations:manage",
  ],
  analyst: ["organization:view", "members:view", "dashboards:view", "dashboards:create", "dashboards:edit"],
  viewer: ["organization:view", "members:view", "dashboards:view"],
};

const allPermissions = expected.owner;

describe("can", () => {
  for (const role of ROLES) {
    for (const perm of allPermissions) {
      const want = expected[role].includes(perm);
      it(`${role} ${want ? "has" : "lacks"} ${perm}`, () => {
        expect(can(role, perm)).toBe(want);
      });
    }
  }

  it("denies undefined and unknown roles", () => {
    expect(can(undefined, "organization:view")).toBe(false);
    expect(can("superuser" as Role, "organization:view")).toBe(false);
  });
});

describe("assignableRoles", () => {
  it("lets owners assign every role", () => {
    expect(assignableRoles("owner")).toEqual(["owner", "admin", "analyst", "viewer"]);
  });

  it("hides owner from non-owners", () => {
    expect(assignableRoles("admin")).toEqual(["admin", "analyst", "viewer"]);
  });
});
