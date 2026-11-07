package organizations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sagargolait/insightiq/services/api/internal/modules/auth"
)

type fakeInvitation struct {
	Invitation
	tokenHash string
	accepted  bool
	revoked   bool
}

// fakeRepository keeps memberships in memory and enforces the same
// invariants as the Postgres repository (last owner, duplicates).
type fakeRepository struct {
	orgs        map[uuid.UUID]Organization
	members     map[uuid.UUID]map[uuid.UUID]string
	emails      map[uuid.UUID]string
	invitations []*fakeInvitation
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		orgs:    map[uuid.UUID]Organization{},
		members: map[uuid.UUID]map[uuid.UUID]string{},
		emails:  map[uuid.UUID]string{},
	}
}

func (f *fakeRepository) CreateOrganization(_ context.Context, name, slug string, ownerID uuid.UUID) (Organization, error) {
	for _, o := range f.orgs {
		if o.Slug == slug {
			return Organization{}, ErrSlugTaken
		}
	}

	org := Organization{ID: uuid.New(), Name: name, Slug: slug, Status: "active"}
	f.orgs[org.ID] = org
	f.members[org.ID] = map[uuid.UUID]string{ownerID: RoleOwner}
	return org, nil
}

func (f *fakeRepository) ListOrganizationsForUser(_ context.Context, userID uuid.UUID) ([]OrganizationWithRole, error) {
	var out []OrganizationWithRole
	for id, members := range f.members {
		if role, ok := members[userID]; ok {
			out = append(out, OrganizationWithRole{Organization: f.orgs[id], Role: role})
		}
	}
	return out, nil
}

func (f *fakeRepository) GetOrganization(_ context.Context, orgID uuid.UUID) (Organization, error) {
	org, ok := f.orgs[orgID]
	if !ok {
		return Organization{}, ErrNotFound
	}
	return org, nil
}

func (f *fakeRepository) GetMembership(_ context.Context, orgID, userID uuid.UUID) (Membership, error) {
	role, ok := f.members[orgID][userID]
	if !ok {
		return Membership{}, ErrNotFound
	}
	return Membership{OrganizationID: orgID, UserID: userID, Role: role}, nil
}

func (f *fakeRepository) ListMembers(_ context.Context, orgID uuid.UUID) ([]Member, error) {
	var out []Member
	for userID, role := range f.members[orgID] {
		out = append(out, Member{UserID: userID, Email: f.emails[userID], Role: role})
	}
	return out, nil
}

func (f *fakeRepository) ensureNotLastOwner(orgID, userID uuid.UUID) error {
	if f.members[orgID][userID] != RoleOwner {
		return nil
	}
	owners := 0
	for _, role := range f.members[orgID] {
		if role == RoleOwner {
			owners++
		}
	}
	if owners <= 1 {
		return ErrLastOwner
	}
	return nil
}

func (f *fakeRepository) UpdateMemberRole(_ context.Context, orgID, userID uuid.UUID, role string) (Member, error) {
	if _, ok := f.members[orgID][userID]; !ok {
		return Member{}, ErrNotFound
	}
	if role != RoleOwner {
		if err := f.ensureNotLastOwner(orgID, userID); err != nil {
			return Member{}, err
		}
	}
	f.members[orgID][userID] = role
	return Member{UserID: userID, Role: role}, nil
}

func (f *fakeRepository) RemoveMember(_ context.Context, orgID, userID uuid.UUID) error {
	if _, ok := f.members[orgID][userID]; !ok {
		return ErrNotFound
	}
	if err := f.ensureNotLastOwner(orgID, userID); err != nil {
		return err
	}
	delete(f.members[orgID], userID)
	return nil
}

func (f *fakeRepository) CreateInvitation(_ context.Context, inv NewInvitation) (Invitation, error) {
	for userID := range f.members[inv.OrganizationID] {
		if f.emails[userID] == inv.Email {
			return Invitation{}, ErrAlreadyMember
		}
	}
	for _, i := range f.invitations {
		if i.OrganizationID == inv.OrganizationID && i.Email == inv.Email && !i.accepted && !i.revoked {
			return Invitation{}, ErrInvitationExists
		}
	}

	invitedBy := inv.InvitedBy
	created := &fakeInvitation{
		Invitation: Invitation{
			ID:             uuid.New(),
			OrganizationID: inv.OrganizationID,
			Email:          inv.Email,
			Role:           inv.Role,
			InvitedBy:      &invitedBy,
			ExpiresAt:      inv.ExpiresAt,
		},
		tokenHash: inv.TokenHash,
	}
	f.invitations = append(f.invitations, created)
	return created.Invitation, nil
}

func (f *fakeRepository) ListPendingInvitations(_ context.Context, orgID uuid.UUID) ([]Invitation, error) {
	var out []Invitation
	for _, i := range f.invitations {
		if i.OrganizationID == orgID && !i.accepted && !i.revoked {
			out = append(out, i.Invitation)
		}
	}
	return out, nil
}

func (f *fakeRepository) RevokeInvitation(_ context.Context, orgID, invitationID uuid.UUID) error {
	for _, i := range f.invitations {
		if i.ID == invitationID && i.OrganizationID == orgID && !i.accepted && !i.revoked {
			i.revoked = true
			return nil
		}
	}
	return ErrInvitationNotFound
}

func (f *fakeRepository) AcceptInvitation(_ context.Context, tokenHash string, userID uuid.UUID, email string) (OrganizationWithRole, error) {
	for _, i := range f.invitations {
		if i.tokenHash != tokenHash || i.accepted || i.revoked || !i.ExpiresAt.After(time.Now()) {
			continue
		}
		if i.Email != email {
			return OrganizationWithRole{}, ErrEmailMismatch
		}
		if _, ok := f.members[i.OrganizationID][userID]; ok {
			return OrganizationWithRole{}, ErrAlreadyMember
		}
		f.members[i.OrganizationID][userID] = i.Role
		i.accepted = true
		return OrganizationWithRole{Organization: f.orgs[i.OrganizationID], Role: i.Role}, nil
	}
	return OrganizationWithRole{}, ErrInvitationNotFound
}

type fixture struct {
	repo    *fakeRepository
	service *Service
	orgID   uuid.UUID
	owner   auth.User
}

func (fx *fixture) addMember(t *testing.T, email, role string) auth.User {
	t.Helper()
	user := auth.User{ID: uuid.New(), Email: email, Status: "active"}
	fx.repo.emails[user.ID] = email
	fx.repo.members[fx.orgID][user.ID] = role
	return user
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	repo := newFakeRepository()
	service := NewService(repo)
	owner := auth.User{ID: uuid.New(), Email: "owner@example.com", Status: "active"}
	repo.emails[owner.ID] = owner.Email

	org, err := service.CreateOrganization(context.Background(), owner, CreateOrganizationRequest{
		Name: "Acme",
		Slug: "acme",
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}

	return &fixture{repo: repo, service: service, orgID: org.ID, owner: owner}
}

func TestCreateOrganizationMakesCreatorOwner(t *testing.T) {
	fx := newFixture(t)

	m, err := fx.repo.GetMembership(context.Background(), fx.orgID, fx.owner.ID)
	if err != nil || m.Role != RoleOwner {
		t.Fatalf("expected creator to be owner, got %q (%v)", m.Role, err)
	}
}

func TestCreateOrganizationValidatesSlug(t *testing.T) {
	fx := newFixture(t)

	for _, slug := range []string{"ab", "Has Spaces", "-leading", "trailing-", "double--dash"} {
		_, err := fx.service.CreateOrganization(context.Background(), fx.owner, CreateOrganizationRequest{
			Name: "Org",
			Slug: slug,
		})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("slug %q: expected ErrInvalidInput, got %v", slug, err)
		}
	}
}

func TestNonMemberSeesNotFound(t *testing.T) {
	fx := newFixture(t)
	outsider := auth.User{ID: uuid.New(), Email: "outsider@example.com"}

	_, err := fx.service.GetOrganization(context.Background(), outsider, fx.orgID)
	if !errors.Is(err, ErrOrganizationNotFound) {
		t.Fatalf("expected ErrOrganizationNotFound, got %v", err)
	}
}

func TestViewerCannotInvite(t *testing.T) {
	fx := newFixture(t)
	viewer := fx.addMember(t, "viewer@example.com", RoleViewer)

	_, err := fx.service.CreateInvitation(context.Background(), viewer, fx.orgID, CreateInvitationRequest{
		Email: "new@example.com",
		Role:  RoleViewer,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestAdminCannotGrantOrChangeOwner(t *testing.T) {
	fx := newFixture(t)
	admin := fx.addMember(t, "admin@example.com", RoleAdmin)
	analyst := fx.addMember(t, "analyst@example.com", RoleAnalyst)
	ctx := context.Background()

	if _, err := fx.service.CreateInvitation(ctx, admin, fx.orgID, CreateInvitationRequest{
		Email: "new@example.com", Role: RoleOwner,
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("invite as owner: expected ErrForbidden, got %v", err)
	}

	if _, err := fx.service.ChangeMemberRole(ctx, admin, fx.orgID, analyst.ID, ChangeRoleRequest{
		Role: RoleOwner,
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("promote to owner: expected ErrForbidden, got %v", err)
	}

	if _, err := fx.service.ChangeMemberRole(ctx, admin, fx.orgID, fx.owner.ID, ChangeRoleRequest{
		Role: RoleViewer,
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("demote owner: expected ErrForbidden, got %v", err)
	}

	if err := fx.service.RemoveMember(ctx, admin, fx.orgID, fx.owner.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("remove owner: expected ErrForbidden, got %v", err)
	}
}

func TestLastOwnerCannotLeaveOrBeDemoted(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()

	if err := fx.service.RemoveMember(ctx, fx.owner, fx.orgID, fx.owner.ID); !errors.Is(err, ErrLastOwner) {
		t.Errorf("leave: expected ErrLastOwner, got %v", err)
	}

	if _, err := fx.service.ChangeMemberRole(ctx, fx.owner, fx.orgID, fx.owner.ID, ChangeRoleRequest{
		Role: RoleAdmin,
	}); !errors.Is(err, ErrLastOwner) {
		t.Errorf("demote: expected ErrLastOwner, got %v", err)
	}
}

func TestOwnerCanLeaveWhenAnotherOwnerExists(t *testing.T) {
	fx := newFixture(t)
	fx.addMember(t, "second@example.com", RoleOwner)

	if err := fx.service.RemoveMember(context.Background(), fx.owner, fx.orgID, fx.owner.ID); err != nil {
		t.Fatalf("expected owner to leave, got %v", err)
	}
}

func TestMemberCanLeaveButNotRemoveOthers(t *testing.T) {
	fx := newFixture(t)
	analyst := fx.addMember(t, "analyst@example.com", RoleAnalyst)
	viewer := fx.addMember(t, "viewer@example.com", RoleViewer)
	ctx := context.Background()

	if err := fx.service.RemoveMember(ctx, analyst, fx.orgID, viewer.ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("remove other: expected ErrForbidden, got %v", err)
	}

	if err := fx.service.RemoveMember(ctx, analyst, fx.orgID, analyst.ID); err != nil {
		t.Errorf("leave: expected success, got %v", err)
	}
}

func TestInvitationFlow(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()

	created, err := fx.service.CreateInvitation(ctx, fx.owner, fx.orgID, CreateInvitationRequest{
		Email: "  New.Person@Example.com ",
		Role:  RoleAnalyst,
	})
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	if created.Email != "new.person@example.com" {
		t.Errorf("expected normalised email, got %q", created.Email)
	}
	if created.Token == "" || fx.repo.invitations[0].tokenHash == created.Token {
		t.Fatal("expected raw token returned and only its hash stored")
	}

	wrongUser := auth.User{ID: uuid.New(), Email: "someone.else@example.com"}
	if _, err := fx.service.AcceptInvitation(ctx, wrongUser, AcceptInvitationRequest{
		Token: created.Token,
	}); !errors.Is(err, ErrEmailMismatch) {
		t.Fatalf("expected ErrEmailMismatch, got %v", err)
	}

	invitee := auth.User{ID: uuid.New(), Email: "new.person@example.com"}
	org, err := fx.service.AcceptInvitation(ctx, invitee, AcceptInvitationRequest{Token: created.Token})
	if err != nil {
		t.Fatalf("accept invitation: %v", err)
	}
	if org.ID != fx.orgID || org.Role != RoleAnalyst {
		t.Fatalf("unexpected result: %+v", org)
	}

	if _, err := fx.service.AcceptInvitation(ctx, invitee, AcceptInvitationRequest{
		Token: created.Token,
	}); !errors.Is(err, ErrInvitationNotFound) {
		t.Fatalf("second accept: expected ErrInvitationNotFound, got %v", err)
	}
}

func TestDuplicateAndMemberInvitationsRejected(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	req := CreateInvitationRequest{Email: "dup@example.com", Role: RoleViewer}

	if _, err := fx.service.CreateInvitation(ctx, fx.owner, fx.orgID, req); err != nil {
		t.Fatalf("first invite: %v", err)
	}
	if _, err := fx.service.CreateInvitation(ctx, fx.owner, fx.orgID, req); !errors.Is(err, ErrInvitationExists) {
		t.Errorf("second invite: expected ErrInvitationExists, got %v", err)
	}

	if _, err := fx.service.CreateInvitation(ctx, fx.owner, fx.orgID, CreateInvitationRequest{
		Email: fx.owner.Email, Role: RoleViewer,
	}); !errors.Is(err, ErrAlreadyMember) {
		t.Errorf("invite member: expected ErrAlreadyMember, got %v", err)
	}
}

func TestRevokedInvitationCannotBeAccepted(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()

	created, err := fx.service.CreateInvitation(ctx, fx.owner, fx.orgID, CreateInvitationRequest{
		Email: "late@example.com", Role: RoleViewer,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := fx.service.RevokeInvitation(ctx, fx.owner, fx.orgID, created.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	invitee := auth.User{ID: uuid.New(), Email: "late@example.com"}
	if _, err := fx.service.AcceptInvitation(ctx, invitee, AcceptInvitationRequest{
		Token: created.Token,
	}); !errors.Is(err, ErrInvitationNotFound) {
		t.Fatalf("expected ErrInvitationNotFound, got %v", err)
	}
}
