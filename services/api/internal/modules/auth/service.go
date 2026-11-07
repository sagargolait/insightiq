package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidInput = errors.New("invalid registration input")

type AuthService struct {
	repository UserRepository
	authRepo   AuthRepository
}

var ErrInvalidCredentials = errors.New("invalid email or password")

var ErrInvalidSession = errors.New("invalid or expired session")

func NewAuthService(repository UserRepository) *AuthService {
	authRepo, _ := repository.(AuthRepository)

	return &AuthService{
		repository: repository,
		authRepo:   authRepo,
	}
}

func (s *AuthService) Register(
	ctx context.Context,
	req RegisterRequest,
) (User, error) {
	name := strings.TrimSpace(req.Name)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	password := req.Password

	var verr ValidationError

	switch {
	case name == "":
		verr.add("name", "Name is required")
	case len(name) > 150:
		verr.add("name", "Name must be at most 150 characters")
	}

	switch {
	case email == "":
		verr.add("email", "Email is required")
	case len(email) > 320:
		verr.add("email", "Email must be at most 320 characters")
	default:
		parsedEmail, err := mail.ParseAddress(email)
		if err != nil || parsedEmail.Address != email {
			verr.add("email", "Email must be a valid address")
		}
	}

	switch {
	case len(password) < 12:
		verr.add("password", "Password must be at least 12 characters")
	case len(password) > 72:
		verr.add("password", "Password must be at most 72 characters")
	}

	if err := verr.orNil(); err != nil {
		return User{}, err
	}

	passwordHash, err := bcrypt.GenerateFromPassword(
		[]byte(password),
		12,
	)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}

	user, err := s.repository.CreateUser(
		ctx,
		name,
		email,
		string(passwordHash),
	)
	if err != nil {
		return User{}, err
	}

	return user, nil
}

func (s *AuthService) Login(
	ctx context.Context,
	req LoginRequest,
) (User, string, error) {
	email := strings.ToLower(strings.TrimSpace(req.Email))

	if email == "" || req.Password == "" {
		return User{}, "", ErrInvalidCredentials
	}

	if s.authRepo == nil {
		return User{}, "", errors.New("auth repository not configured")
	}

	user, passwordHash, err := s.authRepo.FindUserByEmail(ctx, email)
	if err != nil {
		return User{}, "", err
	}

	if user.Status != "active" {
		return User{}, "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(passwordHash),
		[]byte(req.Password),
	); err != nil {
		return User{}, "", ErrInvalidCredentials
	}

	token, err := GenerateSessionToken()
	if err != nil {
		return User{}, "", fmt.Errorf("generate session: %w", err)
	}

	expiresAt := time.Now().UTC().Add(7 * 24 * time.Hour)

	if err := s.authRepo.CreateSession(
		ctx,
		user.ID,
		HashSessionToken(token),
		expiresAt,
	); err != nil {
		return User{}, "", err
	}

	return user, token, nil
}

func (s *AuthService) ValidateSession(
	ctx context.Context,
	token string,
) (User, error) {
	// Tokens are 32 random bytes hex-encoded; anything else cannot match,
	// so reject it without a database round trip.
	if !isWellFormedToken(token) {
		return User{}, ErrInvalidSession
	}

	if s.authRepo == nil {
		return User{}, errors.New("auth repository not configured")
	}

	return s.authRepo.FindUserBySessionHash(ctx, HashSessionToken(token))
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	if !isWellFormedToken(token) {
		return nil
	}

	if s.authRepo == nil {
		return errors.New("auth repository not configured")
	}

	return s.authRepo.RevokeSession(ctx, HashSessionToken(token))
}
