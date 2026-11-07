package organizations

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/sagargolait/insightiq/services/api/internal/modules/auth"
	"github.com/sagargolait/insightiq/services/api/internal/modules/authorization"
)

var (
	ErrInvalidInput         = errors.New("invalid input")
	ErrForbidden            = errors.New("not allowed")
	ErrOrganizationNotFound = errors.New("organization not found")
	ErrMemberNotFound       = errors.New("member not found")
)

const invitationTTL = 7 * 24 * time.Hour

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// requireMember hides organizations the actor does not belong to behind
// ErrOrganizationNotFound, so IDs cannot be probed for existence.
func (s *Service) requireMember(ctx context.Context, orgID, userID uuid.UUID) (Membership, error) {
	m, err := s.repo.GetMembership(ctx, orgID, userID)
	if errors.Is(err, ErrNotFound) {
		return Membership{}, ErrOrganizationNotFound
	}
	return m, err
}

// requirePermission is requireMember plus a permission-matrix check:
// non-members get ErrOrganizationNotFound (404), members lacking the
// permission get ErrForbidden (403).
func (s *Service) requirePermission(
	ctx context.Context,
	orgID, userID uuid.UUID,
	permission authorization.Permission,
) (Membership, error) {
	m, err := s.requireMember(ctx, orgID, userID)
	if err != nil {
		return Membership{}, err
	}
	if !authorization.Can(m.Role, permission) {
		return Membership{}, ErrForbidden
	}
	return m, nil
}

func (s *Service) CreateOrganization(
	ctx context.Context,
	actor auth.User,
	req CreateOrganizationRequest,
) (OrganizationWithRole, error) {
	name := strings.TrimSpace(req.Name)
	slug := strings.ToLower(strings.TrimSpace(req.Slug))

	if name == "" || len(name) > 150 {
		return OrganizationWithRole{}, ErrInvalidInput
	}

	if len(slug) < 3 || len(slug) > 100 || !slugPattern.MatchString(slug) {
		return OrganizationWithRole{}, ErrInvalidInput
	}

	org, err := s.repo.CreateOrganization(ctx, name, slug, actor.ID)
	if err != nil {
		return OrganizationWithRole{}, err
	}

	return OrganizationWithRole{Organization: org, Role: RoleOwner}, nil
}

func (s *Service) ListOrganizations(ctx context.Context, actor auth.User) ([]OrganizationWithRole, error) {
	return s.repo.ListOrganizationsForUser(ctx, actor.ID)
}

func (s *Service) GetOrganization(
	ctx context.Context,
	actor auth.User,
	orgID uuid.UUID,
) (OrganizationWithRole, error) {
	m, err := s.requirePermission(ctx, orgID, actor.ID, authorization.ViewOrganization)
	if err != nil {
		return OrganizationWithRole{}, err
	}

	org, err := s.repo.GetOrganization(ctx, orgID)
	if errors.Is(err, ErrNotFound) {
		return OrganizationWithRole{}, ErrOrganizationNotFound
	}
	if err != nil {
		return OrganizationWithRole{}, err
	}

	return OrganizationWithRole{Organization: org, Role: m.Role}, nil
}

func (s *Service) ListMembers(ctx context.Context, actor auth.User, orgID uuid.UUID) ([]Member, error) {
	if _, err := s.requirePermission(ctx, orgID, actor.ID, authorization.ViewMembers); err != nil {
		return nil, err
	}

	return s.repo.ListMembers(ctx, orgID)
}

func (s *Service) ChangeMemberRole(
	ctx context.Context,
	actor auth.User,
	orgID uuid.UUID,
	targetID uuid.UUID,
	req ChangeRoleRequest,
) (Member, error) {
	if !authorization.IsValidRole(req.Role) {
		return Member{}, ErrInvalidInput
	}

	actorM, err := s.requirePermission(ctx, orgID, actor.ID, authorization.ManageMembers)
	if err != nil {
		return Member{}, err
	}

	target, err := s.repo.GetMembership(ctx, orgID, targetID)
	if errors.Is(err, ErrNotFound) {
		return Member{}, ErrMemberNotFound
	}
	if err != nil {
		return Member{}, err
	}

	// Creating an owner or changing an existing owner needs ManageOwners.
	if (target.Role == RoleOwner || req.Role == RoleOwner) &&
		!authorization.Can(actorM.Role, authorization.ManageOwners) {
		return Member{}, ErrForbidden
	}

	member, err := s.repo.UpdateMemberRole(ctx, orgID, targetID, req.Role)
	if errors.Is(err, ErrNotFound) {
		return Member{}, ErrMemberNotFound
	}

	return member, err
}

func (s *Service) RemoveMember(
	ctx context.Context,
	actor auth.User,
	orgID uuid.UUID,
	targetID uuid.UUID,
) error {
	actorM, err := s.requireMember(ctx, orgID, actor.ID)
	if err != nil {
		return err
	}

	// Anyone may leave; removing someone else needs a manager.
	if targetID != actor.ID {
		if !authorization.Can(actorM.Role, authorization.ManageMembers) {
			return ErrForbidden
		}

		target, err := s.repo.GetMembership(ctx, orgID, targetID)
		if errors.Is(err, ErrNotFound) {
			return ErrMemberNotFound
		}
		if err != nil {
			return err
		}

		if target.Role == RoleOwner && !authorization.Can(actorM.Role, authorization.ManageOwners) {
			return ErrForbidden
		}
	}

	err = s.repo.RemoveMember(ctx, orgID, targetID)
	if errors.Is(err, ErrNotFound) {
		return ErrMemberNotFound
	}

	return err
}

func (s *Service) CreateInvitation(
	ctx context.Context,
	actor auth.User,
	orgID uuid.UUID,
	req CreateInvitationRequest,
) (CreatedInvitation, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))

	if len(email) > 320 || !authorization.IsValidRole(req.Role) {
		return CreatedInvitation{}, ErrInvalidInput
	}

	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return CreatedInvitation{}, ErrInvalidInput
	}

	actorM, err := s.requirePermission(ctx, orgID, actor.ID, authorization.ManageMembers)
	if err != nil {
		return CreatedInvitation{}, err
	}

	if req.Role == RoleOwner && !authorization.Can(actorM.Role, authorization.ManageOwners) {
		return CreatedInvitation{}, ErrForbidden
	}

	token, err := auth.GenerateSessionToken()
	if err != nil {
		return CreatedInvitation{}, fmt.Errorf("generate invitation token: %w", err)
	}

	invitation, err := s.repo.CreateInvitation(ctx, NewInvitation{
		OrganizationID: orgID,
		Email:          email,
		Role:           req.Role,
		TokenHash:      auth.HashSessionToken(token),
		InvitedBy:      actor.ID,
		ExpiresAt:      s.now().UTC().Add(invitationTTL),
	})
	if err != nil {
		return CreatedInvitation{}, err
	}

	return CreatedInvitation{Invitation: invitation, Token: token}, nil
}

func (s *Service) ListInvitations(ctx context.Context, actor auth.User, orgID uuid.UUID) ([]Invitation, error) {
	if _, err := s.requirePermission(ctx, orgID, actor.ID, authorization.ManageMembers); err != nil {
		return nil, err
	}

	return s.repo.ListPendingInvitations(ctx, orgID)
}

func (s *Service) RevokeInvitation(
	ctx context.Context,
	actor auth.User,
	orgID uuid.UUID,
	invitationID uuid.UUID,
) error {
	if _, err := s.requirePermission(ctx, orgID, actor.ID, authorization.ManageMembers); err != nil {
		return err
	}

	return s.repo.RevokeInvitation(ctx, orgID, invitationID)
}

func (s *Service) AcceptInvitation(
	ctx context.Context,
	actor auth.User,
	req AcceptInvitationRequest,
) (OrganizationWithRole, error) {
	token := strings.TrimSpace(req.Token)
	if token == "" {
		return OrganizationWithRole{}, ErrInvitationNotFound
	}

	return s.repo.AcceptInvitation(
		ctx,
		auth.HashSessionToken(token),
		actor.ID,
		strings.ToLower(actor.Email),
	)
}
