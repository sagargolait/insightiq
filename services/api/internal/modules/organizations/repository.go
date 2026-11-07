package organizations

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrSlugTaken           = errors.New("organization slug already taken")
	ErrLastOwner           = errors.New("organization must keep at least one owner")
	ErrAlreadyMember       = errors.New("user is already a member")
	ErrInvitationExists    = errors.New("an open invitation already exists for this email")
	ErrInvitationNotFound  = errors.New("invitation not found or no longer valid")
	ErrEmailMismatch       = errors.New("invitation was sent to a different email")
	ErrMemberHasDependents = errors.New("member still owns data in this organization")
)

type Repository interface {
	CreateOrganization(ctx context.Context, name, slug string, ownerID uuid.UUID) (Organization, error)
	ListOrganizationsForUser(ctx context.Context, userID uuid.UUID) ([]OrganizationWithRole, error)
	GetOrganization(ctx context.Context, orgID uuid.UUID) (Organization, error)
	GetMembership(ctx context.Context, orgID, userID uuid.UUID) (Membership, error)
	ListMembers(ctx context.Context, orgID uuid.UUID) ([]Member, error)
	UpdateMemberRole(ctx context.Context, orgID, userID uuid.UUID, role string) (Member, error)
	RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error
	CreateInvitation(ctx context.Context, inv NewInvitation) (Invitation, error)
	ListPendingInvitations(ctx context.Context, orgID uuid.UUID) ([]Invitation, error)
	RevokeInvitation(ctx context.Context, orgID, invitationID uuid.UUID) error
	AcceptInvitation(ctx context.Context, tokenHash string, userID uuid.UUID, email string) (OrganizationWithRole, error)
}

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) CreateOrganization(
	ctx context.Context,
	name string,
	slug string,
	ownerID uuid.UUID,
) (Organization, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Organization{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var org Organization

	err = tx.QueryRow(ctx, `
        INSERT INTO organizations (name, slug)
        VALUES ($1, $2)
        RETURNING id, name, slug, status, created_at
    `, name, slug).Scan(&org.ID, &org.Name, &org.Slug, &org.Status, &org.CreatedAt)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName == "organizations_slug_key" {
			return Organization{}, ErrSlugTaken
		}
		return Organization{}, fmt.Errorf("insert organization: %w", err)
	}

	if _, err := tx.Exec(ctx, `
        INSERT INTO memberships (organization_id, user_id, role)
        VALUES ($1, $2, 'owner')
    `, org.ID, ownerID); err != nil {
		return Organization{}, fmt.Errorf("insert owner membership: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Organization{}, fmt.Errorf("commit: %w", err)
	}

	return org, nil
}

func (r *PostgresRepository) ListOrganizationsForUser(
	ctx context.Context,
	userID uuid.UUID,
) ([]OrganizationWithRole, error) {
	rows, err := r.db.Query(ctx, `
        SELECT o.id, o.name, o.slug, o.status, o.created_at, m.role
        FROM memberships m
        JOIN organizations o ON o.id = m.organization_id
        WHERE m.user_id = $1
          AND o.status = 'active'
        ORDER BY o.name
    `, userID)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}

	orgs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (OrganizationWithRole, error) {
		var o OrganizationWithRole
		err := row.Scan(&o.ID, &o.Name, &o.Slug, &o.Status, &o.CreatedAt, &o.Role)
		return o, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan organizations: %w", err)
	}

	return orgs, nil
}

func (r *PostgresRepository) GetOrganization(
	ctx context.Context,
	orgID uuid.UUID,
) (Organization, error) {
	var org Organization

	err := r.db.QueryRow(ctx, `
        SELECT id, name, slug, status, created_at
        FROM organizations
        WHERE id = $1
    `, orgID).Scan(&org.ID, &org.Name, &org.Slug, &org.Status, &org.CreatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrNotFound
	}
	if err != nil {
		return Organization{}, fmt.Errorf("get organization: %w", err)
	}

	return org, nil
}

// GetMembership only matches active organizations, so a suspended
// organization looks the same as one the user does not belong to.
func (r *PostgresRepository) GetMembership(
	ctx context.Context,
	orgID uuid.UUID,
	userID uuid.UUID,
) (Membership, error) {
	m := Membership{OrganizationID: orgID, UserID: userID}

	err := r.db.QueryRow(ctx, `
        SELECT m.role
        FROM memberships m
        JOIN organizations o ON o.id = m.organization_id
        WHERE m.organization_id = $1
          AND m.user_id = $2
          AND o.status = 'active'
    `, orgID, userID).Scan(&m.Role)

	if errors.Is(err, pgx.ErrNoRows) {
		return Membership{}, ErrNotFound
	}
	if err != nil {
		return Membership{}, fmt.Errorf("get membership: %w", err)
	}

	return m, nil
}

func (r *PostgresRepository) ListMembers(
	ctx context.Context,
	orgID uuid.UUID,
) ([]Member, error) {
	rows, err := r.db.Query(ctx, `
        SELECT u.id, u.name, u.email, m.role, m.created_at
        FROM memberships m
        JOIN users u ON u.id = m.user_id
        WHERE m.organization_id = $1
        ORDER BY m.created_at
    `, orgID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}

	members, err := pgx.CollectRows(rows, scanMember)
	if err != nil {
		return nil, fmt.Errorf("scan members: %w", err)
	}

	return members, nil
}

func scanMember(row pgx.CollectableRow) (Member, error) {
	var m Member
	err := row.Scan(&m.UserID, &m.Name, &m.Email, &m.Role, &m.JoinedAt)
	return m, err
}

// lockOrganization serialises membership changes per organization, so two
// owners demoting each other at the same time cannot both succeed.
func lockOrganization(ctx context.Context, tx pgx.Tx, orgID uuid.UUID) error {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
        SELECT id FROM organizations WHERE id = $1 FOR UPDATE
    `, orgID).Scan(&id)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock organization: %w", err)
	}

	return nil
}

// ensureNotLastOwner fails when userID is currently the only owner.
func ensureNotLastOwner(ctx context.Context, tx pgx.Tx, orgID, userID uuid.UUID) error {
	var currentRole string
	err := tx.QueryRow(ctx, `
        SELECT role FROM memberships
        WHERE organization_id = $1 AND user_id = $2
    `, orgID, userID).Scan(&currentRole)

	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get member role: %w", err)
	}

	if currentRole != RoleOwner {
		return nil
	}

	var owners int
	if err := tx.QueryRow(ctx, `
        SELECT COUNT(*) FROM memberships
        WHERE organization_id = $1 AND role = 'owner'
    `, orgID).Scan(&owners); err != nil {
		return fmt.Errorf("count owners: %w", err)
	}

	if owners <= 1 {
		return ErrLastOwner
	}

	return nil
}

func (r *PostgresRepository) UpdateMemberRole(
	ctx context.Context,
	orgID uuid.UUID,
	userID uuid.UUID,
	role string,
) (Member, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Member{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := lockOrganization(ctx, tx, orgID); err != nil {
		return Member{}, err
	}

	if role != RoleOwner {
		if err := ensureNotLastOwner(ctx, tx, orgID, userID); err != nil {
			return Member{}, err
		}
	}

	rows, err := tx.Query(ctx, `
        WITH updated AS (
            UPDATE memberships
            SET role = $3
            WHERE organization_id = $1 AND user_id = $2
            RETURNING user_id, role, created_at
        )
        SELECT u.id, u.name, u.email, updated.role, updated.created_at
        FROM updated
        JOIN users u ON u.id = updated.user_id
    `, orgID, userID, role)
	if err != nil {
		return Member{}, fmt.Errorf("update role: %w", err)
	}

	member, err := pgx.CollectExactlyOneRow(rows, scanMember)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, ErrNotFound
	}
	if err != nil {
		return Member{}, fmt.Errorf("update role: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Member{}, fmt.Errorf("commit: %w", err)
	}

	return member, nil
}

func (r *PostgresRepository) RemoveMember(
	ctx context.Context,
	orgID uuid.UUID,
	userID uuid.UUID,
) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := lockOrganization(ctx, tx, orgID); err != nil {
		return err
	}

	if err := ensureNotLastOwner(ctx, tx, orgID, userID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
        DELETE FROM memberships
        WHERE organization_id = $1 AND user_id = $2
    `, orgID, userID); err != nil {
		// conversations reference memberships without ON DELETE CASCADE.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrMemberHasDependents
		}
		return fmt.Errorf("delete membership: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

func (r *PostgresRepository) CreateInvitation(
	ctx context.Context,
	inv NewInvitation,
) (Invitation, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Invitation{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var isMember bool
	if err := tx.QueryRow(ctx, `
        SELECT EXISTS (
            SELECT 1 FROM memberships m
            JOIN users u ON u.id = m.user_id
            WHERE m.organization_id = $1 AND u.email = $2
        )
    `, inv.OrganizationID, inv.Email).Scan(&isMember); err != nil {
		return Invitation{}, fmt.Errorf("check membership: %w", err)
	}
	if isMember {
		return Invitation{}, ErrAlreadyMember
	}

	// Expired invitations would otherwise block a fresh one through the
	// partial unique index.
	if _, err := tx.Exec(ctx, `
        UPDATE invitations
        SET revoked_at = NOW()
        WHERE organization_id = $1
          AND email = $2
          AND accepted_at IS NULL
          AND revoked_at IS NULL
          AND expires_at <= NOW()
    `, inv.OrganizationID, inv.Email); err != nil {
		return Invitation{}, fmt.Errorf("expire old invitations: %w", err)
	}

	var created Invitation

	err = tx.QueryRow(ctx, `
        INSERT INTO invitations (organization_id, email, role, token_hash, invited_by, expires_at)
        VALUES ($1, $2, $3, $4, $5, $6)
        RETURNING id, organization_id, email, role, invited_by, expires_at, created_at
    `,
		inv.OrganizationID,
		inv.Email,
		inv.Role,
		inv.TokenHash,
		inv.InvitedBy,
		inv.ExpiresAt,
	).Scan(
		&created.ID,
		&created.OrganizationID,
		&created.Email,
		&created.Role,
		&created.InvitedBy,
		&created.ExpiresAt,
		&created.CreatedAt,
	)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName == "idx_invitations_pending" {
			return Invitation{}, ErrInvitationExists
		}
		return Invitation{}, fmt.Errorf("insert invitation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Invitation{}, fmt.Errorf("commit: %w", err)
	}

	return created, nil
}

func (r *PostgresRepository) ListPendingInvitations(
	ctx context.Context,
	orgID uuid.UUID,
) ([]Invitation, error) {
	rows, err := r.db.Query(ctx, `
        SELECT id, organization_id, email, role, invited_by, expires_at, created_at
        FROM invitations
        WHERE organization_id = $1
          AND accepted_at IS NULL
          AND revoked_at IS NULL
          AND expires_at > NOW()
        ORDER BY created_at DESC
    `, orgID)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}

	invitations, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Invitation, error) {
		var i Invitation
		err := row.Scan(&i.ID, &i.OrganizationID, &i.Email, &i.Role, &i.InvitedBy, &i.ExpiresAt, &i.CreatedAt)
		return i, err
	})
	if err != nil {
		return nil, fmt.Errorf("scan invitations: %w", err)
	}

	return invitations, nil
}

func (r *PostgresRepository) RevokeInvitation(
	ctx context.Context,
	orgID uuid.UUID,
	invitationID uuid.UUID,
) error {
	tag, err := r.db.Exec(ctx, `
        UPDATE invitations
        SET revoked_at = NOW()
        WHERE id = $1
          AND organization_id = $2
          AND accepted_at IS NULL
          AND revoked_at IS NULL
    `, invitationID, orgID)
	if err != nil {
		return fmt.Errorf("revoke invitation: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrInvitationNotFound
	}

	return nil
}

func (r *PostgresRepository) AcceptInvitation(
	ctx context.Context,
	tokenHash string,
	userID uuid.UUID,
	email string,
) (OrganizationWithRole, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return OrganizationWithRole{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var (
		invitationID uuid.UUID
		orgID        uuid.UUID
		invitedEmail string
		role         string
	)

	// FOR UPDATE stops the same token being accepted twice concurrently.
	err = tx.QueryRow(ctx, `
        SELECT i.id, i.organization_id, i.email, i.role
        FROM invitations i
        JOIN organizations o ON o.id = i.organization_id
        WHERE i.token_hash = $1
          AND i.accepted_at IS NULL
          AND i.revoked_at IS NULL
          AND i.expires_at > NOW()
          AND o.status = 'active'
        FOR UPDATE OF i
    `, tokenHash).Scan(&invitationID, &orgID, &invitedEmail, &role)

	if errors.Is(err, pgx.ErrNoRows) {
		return OrganizationWithRole{}, ErrInvitationNotFound
	}
	if err != nil {
		return OrganizationWithRole{}, fmt.Errorf("find invitation: %w", err)
	}

	if invitedEmail != email {
		return OrganizationWithRole{}, ErrEmailMismatch
	}

	tag, err := tx.Exec(ctx, `
        INSERT INTO memberships (organization_id, user_id, role)
        VALUES ($1, $2, $3)
        ON CONFLICT (organization_id, user_id) DO NOTHING
    `, orgID, userID, role)
	if err != nil {
		return OrganizationWithRole{}, fmt.Errorf("insert membership: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return OrganizationWithRole{}, ErrAlreadyMember
	}

	if _, err := tx.Exec(ctx, `
        UPDATE invitations SET accepted_at = NOW() WHERE id = $1
    `, invitationID); err != nil {
		return OrganizationWithRole{}, fmt.Errorf("mark invitation accepted: %w", err)
	}

	var result OrganizationWithRole
	if err := tx.QueryRow(ctx, `
        SELECT id, name, slug, status, created_at
        FROM organizations WHERE id = $1
    `, orgID).Scan(&result.ID, &result.Name, &result.Slug, &result.Status, &result.CreatedAt); err != nil {
		return OrganizationWithRole{}, fmt.Errorf("load organization: %w", err)
	}
	result.Role = role

	if err := tx.Commit(ctx); err != nil {
		return OrganizationWithRole{}, fmt.Errorf("commit: %w", err)
	}

	return result, nil
}
