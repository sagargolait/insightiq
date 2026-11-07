package authorization

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sagargolait/insightiq/services/api/internal/modules/auth"
)

// ErrNotMember is returned by a MembershipLookup when the user has no
// membership in the organization (or the organization does not exist).
var ErrNotMember = errors.New("not a member of this organization")

// MembershipLookup resolves a user's role in an organization.
type MembershipLookup interface {
	MemberRole(ctx context.Context, orgID, userID uuid.UUID) (string, error)
}

// Principal is the authorized actor for an organization-scoped request.
// Downstream handlers read it with PrincipalFromContext and use OrgID as the
// tenant for database.WithTenantTx.
type Principal struct {
	User  auth.User
	OrgID uuid.UUID
	Role  string
}

// Can reports whether the principal's role grants permission.
func (p Principal) Can(permission Permission) bool {
	return Can(p.Role, permission)
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

type Middleware struct {
	memberships MembershipLookup
}

func NewMiddleware(memberships MembershipLookup) *Middleware {
	return &Middleware{memberships: memberships}
}

// RequirePermission authorizes an organization-scoped route. It must run
// after auth.RequireSession and inside a chi route that defines orgParam.
//
//   - no authenticated user       → 401 UNAUTHENTICATED
//   - malformed org ID / non-member → 404 ORGANIZATION_NOT_FOUND
//     (non-members cannot probe which organizations exist)
//   - member lacking permission   → 403 FORBIDDEN
//
// On success the Principal is stored in the request context.
func (m *Middleware) RequirePermission(orgParam string, permission Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := auth.UserFromContext(r.Context())
			if !ok {
				writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
				return
			}

			orgID, err := uuid.Parse(chi.URLParam(r, orgParam))
			if err != nil {
				writeError(w, http.StatusNotFound, "ORGANIZATION_NOT_FOUND", "Organization not found")
				return
			}

			role, err := m.memberships.MemberRole(r.Context(), orgID, user.ID)
			if errors.Is(err, ErrNotMember) {
				writeError(w, http.StatusNotFound, "ORGANIZATION_NOT_FOUND", "Organization not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Something went wrong")
				return
			}

			if !Can(role, permission) {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "You do not have permission to do this")
				return
			}

			principal := Principal{User: user, OrgID: orgID, Role: role}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

// PostgresMemberships implements MembershipLookup. memberships is not an
// RLS table, so this runs on the pool without a tenant context.
type PostgresMemberships struct {
	db *pgxpool.Pool
}

func NewPostgresMemberships(db *pgxpool.Pool) *PostgresMemberships {
	return &PostgresMemberships{db: db}
}

func (p *PostgresMemberships) MemberRole(ctx context.Context, orgID, userID uuid.UUID) (string, error) {
	var role string
	err := p.db.QueryRow(ctx, `
		SELECT m.role
		FROM memberships m
		JOIN organizations o ON o.id = m.organization_id
		WHERE m.organization_id = $1
		  AND m.user_id = $2
		  AND o.status = 'active'
	`, orgID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotMember
	}
	return role, err
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var body errorBody
	body.Error.Code = code
	body.Error.Message = message

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
