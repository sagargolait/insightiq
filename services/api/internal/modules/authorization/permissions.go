// Package authorization holds the single source of truth for what each
// organization role may do. Handlers and services ask Can(role, permission)
// instead of comparing role strings themselves.
package authorization

// Organization roles, ordered from most to least privileged.
const (
	RoleOwner   = "owner"
	RoleAdmin   = "admin"
	RoleAnalyst = "analyst"
	RoleViewer  = "viewer"
)

// Permission names one action a member can take inside an organization.
type Permission string

const (
	ViewOrganization   Permission = "organization:view"
	DeleteOrganization Permission = "organization:delete"

	ViewMembers   Permission = "members:view"
	ManageMembers Permission = "members:manage" // invite, remove, change roles
	ManageOwners  Permission = "members:manage_owners"

	ViewDashboards   Permission = "dashboards:view"
	CreateDashboards Permission = "dashboards:create"
	EditDashboards   Permission = "dashboards:edit"
	DeleteDashboards Permission = "dashboards:delete"

	ViewDataSources   Permission = "datasources:view"
	ManageDataSources Permission = "datasources:manage" // create, edit, delete, test, refresh schema
	RunQueries        Permission = "analytics:run"

	ManageIntegrations Permission = "integrations:manage"
)

// matrix is the permission table. A permission missing from a role's set is
// denied, so a new permission is closed by default until granted here.
var matrix = map[string]map[Permission]struct{}{
	RoleOwner: set(
		ViewOrganization, DeleteOrganization,
		ViewMembers, ManageMembers, ManageOwners,
		ViewDashboards, CreateDashboards, EditDashboards, DeleteDashboards,
		ViewDataSources, ManageDataSources, RunQueries,
		ManageIntegrations,
	),
	RoleAdmin: set(
		ViewOrganization,
		ViewMembers, ManageMembers,
		ViewDashboards, CreateDashboards, EditDashboards, DeleteDashboards,
		ViewDataSources, ManageDataSources, RunQueries,
		ManageIntegrations,
	),
	RoleAnalyst: set(
		ViewOrganization,
		ViewMembers,
		ViewDashboards, CreateDashboards, EditDashboards,
		ViewDataSources, RunQueries,
	),
	RoleViewer: set(
		ViewOrganization,
		ViewMembers,
		ViewDashboards,
		ViewDataSources,
	),
}

func set(perms ...Permission) map[Permission]struct{} {
	m := make(map[Permission]struct{}, len(perms))
	for _, p := range perms {
		m[p] = struct{}{}
	}
	return m
}

// IsValidRole reports whether role is one of the known organization roles.
func IsValidRole(role string) bool {
	_, ok := matrix[role]
	return ok
}

// Can reports whether role grants permission. Unknown roles get nothing.
func Can(role string, permission Permission) bool {
	_, ok := matrix[role][permission]
	return ok
}
