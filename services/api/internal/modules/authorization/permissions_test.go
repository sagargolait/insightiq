package authorization

import "testing"

// TestPermissionMatrix pins the full role × permission table. Changing who
// may do what must be a deliberate edit here as well as in the matrix.
func TestPermissionMatrix(t *testing.T) {
	const y, n = true, false

	roles := []string{RoleOwner, RoleAdmin, RoleAnalyst, RoleViewer}

	want := map[Permission][4]bool{
		//                    owner admin analyst viewer
		ViewOrganization:   {y, y, y, y},
		ViewMembers:        {y, y, y, y},
		ViewDashboards:     {y, y, y, y},
		CreateDashboards:   {y, y, y, n},
		EditDashboards:     {y, y, y, n},
		DeleteDashboards:   {y, y, n, n},
		ManageMembers:      {y, y, n, n},
		ManageIntegrations: {y, y, n, n},
		ManageOwners:       {y, n, n, n},
		DeleteOrganization: {y, n, n, n},
	}

	for perm, row := range want {
		for i, role := range roles {
			if got := Can(role, perm); got != row[i] {
				t.Errorf("Can(%s, %s) = %t, want %t", role, perm, got, row[i])
			}
		}
	}

	// Every permission granted anywhere must appear in the table above, so a
	// new permission cannot ship without its row being reviewed.
	for role, perms := range matrix {
		for perm := range perms {
			if _, ok := want[perm]; !ok {
				t.Errorf("role %s grants %s, which is missing from this test", role, perm)
			}
		}
	}
}

func TestUnknownRoleAndPermissionAreDenied(t *testing.T) {
	if Can("superadmin", ViewOrganization) {
		t.Error("unknown role must be denied")
	}
	if Can("", ViewOrganization) {
		t.Error("empty role must be denied")
	}
	if Can(RoleOwner, Permission("billing:manage")) {
		t.Error("unknown permission must be denied, even for owner")
	}
}

func TestIsValidRole(t *testing.T) {
	for _, role := range []string{RoleOwner, RoleAdmin, RoleAnalyst, RoleViewer} {
		if !IsValidRole(role) {
			t.Errorf("%s should be valid", role)
		}
	}
	for _, role := range []string{"", "Owner", "superadmin"} {
		if IsValidRole(role) {
			t.Errorf("%q should be invalid", role)
		}
	}
}
