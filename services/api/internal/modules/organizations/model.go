package organizations

import (
	"time"

	"github.com/google/uuid"

	"github.com/sagargolait/insightiq/services/api/internal/modules/authorization"
)

// Role names are defined by the authorization module; these aliases keep
// call sites in this package short.
const (
	RoleOwner   = authorization.RoleOwner
	RoleAdmin   = authorization.RoleAdmin
	RoleAnalyst = authorization.RoleAnalyst
	RoleViewer  = authorization.RoleViewer
)

type Organization struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type OrganizationWithRole struct {
	Organization
	Role string `json:"role"`
}

type Membership struct {
	OrganizationID uuid.UUID
	UserID         uuid.UUID
	Role           string
}

type Member struct {
	UserID   uuid.UUID `json:"user_id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

type Invitation struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	Email          string     `json:"email"`
	Role           string     `json:"role"`
	InvitedBy      *uuid.UUID `json:"invited_by"`
	ExpiresAt      time.Time  `json:"expires_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

// CreatedInvitation is returned once, at creation; the raw token is never
// stored and cannot be fetched again.
type CreatedInvitation struct {
	Invitation
	Token string `json:"token"`
}

type NewInvitation struct {
	OrganizationID uuid.UUID
	Email          string
	Role           string
	TokenHash      string
	InvitedBy      uuid.UUID
	ExpiresAt      time.Time
}

type CreateOrganizationRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type ChangeRoleRequest struct {
	Role string `json:"role"`
}

type CreateInvitationRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type AcceptInvitationRequest struct {
	Token string `json:"token"`
}
