// Mirror of services/api/internal/modules/authorization/permissions.go.
// UI-only: hides controls a role can't use. The API remains the authority —
// keep this table in sync with the Go matrix.

export const ROLES = ["owner", "admin", "analyst", "viewer"] as const;
export type Role = (typeof ROLES)[number];

export type Permission =
  | "organization:view"
  | "organization:delete"
  | "members:view"
  | "members:manage"
  | "members:manage_owners"
  | "dashboards:view"
  | "dashboards:create"
  | "dashboards:edit"
  | "integrations:manage";

const matrix: Record<Role, ReadonlySet<Permission>> = {
  owner: new Set<Permission>([
    "organization:view",
    "organization:delete",
    "members:view",
    "members:manage",
    "members:manage_owners",
    "dashboards:view",
    "dashboards:create",
    "dashboards:edit",
    "integrations:manage",
  ]),
  admin: new Set<Permission>([
    "organization:view",
    "members:view",
    "members:manage",
    "dashboards:view",
    "dashboards:create",
    "dashboards:edit",
    "integrations:manage",
  ]),
  analyst: new Set<Permission>([
    "organization:view",
    "members:view",
    "dashboards:view",
    "dashboards:create",
    "dashboards:edit",
  ]),
  viewer: new Set<Permission>(["organization:view", "members:view", "dashboards:view"]),
};

export function can(role: Role | undefined, permission: Permission): boolean {
  return role ? matrix[role]?.has(permission) ?? false : false;
}

/** Roles an actor may assign (owner requires members:manage_owners). */
export function assignableRoles(actor: Role): Role[] {
  return ROLES.filter((r) => r !== "owner" || can(actor, "members:manage_owners"));
}

export const ROLE_LABEL: Record<Role, string> = {
  owner: "Owner",
  admin: "Admin",
  analyst: "Analyst",
  viewer: "Viewer",
};

export const ROLE_DESCRIPTION: Record<Role, string> = {
  owner: "Full control, including other owners and deleting the workspace.",
  admin: "Manage members, invitations, dashboards and integrations.",
  analyst: "Create and edit dashboards. Read-only on members.",
  viewer: "View dashboards and members.",
};
