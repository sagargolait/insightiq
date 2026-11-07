package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrEmailAlreadyExists = errors.New("email already exists")

type UserRepository interface {
	CreateUser(
		ctx context.Context,
		name string,
		email string,
		passwordHash string,
	) (User, error)
}

type AuthRepository interface {
	FindUserByEmail(ctx context.Context, email string) (User, string, error)
	CreateSession(ctx context.Context, userID uuid.UUID, tokenHash string, expiresAt time.Time) error
	FindUserBySessionHash(ctx context.Context, tokenHash string) (User, error)
	RevokeSession(ctx context.Context, tokenHash string) error
}

type PostgresUserRepository struct {
	db *pgxpool.Pool
}

func NewPostgresUserRepository(db *pgxpool.Pool) *PostgresUserRepository {
	return &PostgresUserRepository{db: db}
}

func (r *PostgresUserRepository) CreateUser(
	ctx context.Context,
	name string,
	email string,
	passwordHash string,
) (User, error) {
	var user User

	query := `
		INSERT INTO users (name, email, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, name, email, status, created_at
	`

	err := r.db.QueryRow(
		ctx,
		query,
		name,
		email,
		passwordHash,
	).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.Status,
		&user.CreatedAt,
	)

	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName == "users_email_key" {
			return User{}, ErrEmailAlreadyExists
		}

		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, fmt.Errorf("create user returned no row")
		}

		return User{}, fmt.Errorf("insert user: %w", err)
	}

	return user, nil
}

func (r *PostgresUserRepository) FindUserByEmail(
	ctx context.Context,
	email string,
) (User, string, error) {
	var user User
	var passwordHash string

	query := `
        SELECT id, name, email, status, created_at, password_hash
        FROM users
        WHERE email = $1
    `

	err := r.db.QueryRow(ctx, query, email).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.Status,
		&user.CreatedAt,
		&passwordHash,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrInvalidCredentials
	}

	if err != nil {
		return User{}, "", fmt.Errorf("find user: %w", err)
	}

	return user, passwordHash, nil
}

func (r *PostgresUserRepository) CreateSession(
	ctx context.Context,
	userID uuid.UUID,
	tokenHash string,
	expiresAt time.Time,
) error {
	query := `
        INSERT INTO sessions (user_id, token_hash, expires_at)
        VALUES ($1, $2, $3)
    `

	_, err := r.db.Exec(ctx, query, userID, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	return nil
}

// FindUserBySessionHash returns the owner of a session only while the
// session is unrevoked, unexpired and the user is still active.
func (r *PostgresUserRepository) FindUserBySessionHash(
	ctx context.Context,
	tokenHash string,
) (User, error) {
	var user User

	query := `
        SELECT u.id, u.name, u.email, u.status, u.created_at
        FROM sessions s
        JOIN users u ON u.id = s.user_id
        WHERE s.token_hash = $1
          AND s.revoked_at IS NULL
          AND s.expires_at > NOW()
          AND u.status = 'active'
    `

	err := r.db.QueryRow(ctx, query, tokenHash).Scan(
		&user.ID,
		&user.Name,
		&user.Email,
		&user.Status,
		&user.CreatedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrInvalidSession
	}

	if err != nil {
		return User{}, fmt.Errorf("find session user: %w", err)
	}

	return user, nil
}

func (r *PostgresUserRepository) RevokeSession(
	ctx context.Context,
	tokenHash string,
) error {
	query := `
        UPDATE sessions
        SET revoked_at = NOW()
        WHERE token_hash = $1
          AND revoked_at IS NULL
    `

	_, err := r.db.Exec(ctx, query, tokenHash)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}

	return nil
}
