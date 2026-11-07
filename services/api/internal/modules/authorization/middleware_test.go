package authorization

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/sagargolait/insightiq/services/api/internal/modules/auth"
)

type fakeMemberships map[[2]uuid.UUID]string

func (f fakeMemberships) MemberRole(_ context.Context, orgID, userID uuid.UUID) (string, error) {
	role, ok := f[[2]uuid.UUID{orgID, userID}]
	if !ok {
		return "", ErrNotMember
	}
	return role, nil
}

type failingMemberships struct{}

func (failingMemberships) MemberRole(context.Context, uuid.UUID, uuid.UUID) (string, error) {
	return "", errors.New("db down")
}

func newTestRouter(lookup MembershipLookup, perm Permission, seen *Principal) http.Handler {
	r := chi.NewRouter()
	r.With(NewMiddleware(lookup).RequirePermission("orgID", perm)).
		Get("/orgs/{orgID}/thing", func(w http.ResponseWriter, r *http.Request) {
			p, ok := PrincipalFromContext(r.Context())
			if !ok {
				http.Error(w, "no principal", http.StatusInternalServerError)
				return
			}
			*seen = p
			w.WriteHeader(http.StatusNoContent)
		})
	return r
}

func do(h http.Handler, user *auth.User, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if user != nil {
		req = req.WithContext(auth.WithUser(req.Context(), *user))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRequirePermission(t *testing.T) {
	orgA, orgB := uuid.New(), uuid.New()
	owner := auth.User{ID: uuid.New()}
	viewer := auth.User{ID: uuid.New()}
	outsider := auth.User{ID: uuid.New()}

	lookup := fakeMemberships{
		{orgA, owner.ID}:    RoleOwner,
		{orgA, viewer.ID}:   RoleViewer,
		{orgB, outsider.ID}: RoleOwner, // owns B, not a member of A
	}

	pathA := "/orgs/" + orgA.String() + "/thing"

	cases := []struct {
		name string
		user *auth.User
		path string
		perm Permission
		want int
	}{
		{"unauthenticated", nil, pathA, ViewDashboards, http.StatusUnauthorized},
		{"malformed org id", &owner, "/orgs/nope/thing", ViewDashboards, http.StatusNotFound},
		{"unknown org", &owner, "/orgs/" + uuid.NewString() + "/thing", ViewDashboards, http.StatusNotFound},
		{"owner of other org swaps id", &outsider, pathA, ViewDashboards, http.StatusNotFound},
		{"viewer can view", &viewer, pathA, ViewDashboards, http.StatusNoContent},
		{"viewer cannot create", &viewer, pathA, CreateDashboards, http.StatusForbidden},
		{"viewer cannot manage members", &viewer, pathA, ManageMembers, http.StatusForbidden},
		{"owner can delete org", &owner, pathA, DeleteOrganization, http.StatusNoContent},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var seen Principal
			rec := do(newTestRouter(lookup, tc.perm, &seen), tc.user, tc.path)
			if rec.Code != tc.want {
				t.Fatalf("want %d, got %d: %s", tc.want, rec.Code, rec.Body)
			}
			if rec.Code == http.StatusNoContent {
				if seen.OrgID != orgA || seen.User.ID != tc.user.ID {
					t.Fatalf("principal not populated correctly: %+v", seen)
				}
			}
		})
	}
}

func TestRequirePermissionLookupError(t *testing.T) {
	var seen Principal
	user := auth.User{ID: uuid.New()}
	rec := do(newTestRouter(failingMemberships{}, ViewDashboards, &seen),
		&user, "/orgs/"+uuid.NewString()+"/thing")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", rec.Code)
	}
}
