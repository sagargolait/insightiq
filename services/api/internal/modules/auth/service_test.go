package auth

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

type fakeUserRepository struct {
	createdName         string
	createdEmail        string
	createdPasswordHash string
	createErr           error
}

func (f *fakeUserRepository) CreateUser(
	_ context.Context,
	name, email, passwordHash string,
) (User, error) {
	if f.createErr != nil {
		return User{}, f.createErr
	}

	f.createdName = name
	f.createdEmail = email
	f.createdPasswordHash = passwordHash

	return User{
		Name:   name,
		Email:  email,
		Status: "active",
	}, nil
}

func TestRegisterNormalizesEmailAndHashesPassword(t *testing.T) {
	repo := &fakeUserRepository{}
	service := NewAuthService(repo)

	password := "StrongPassword123!"

	user, err := service.Register(context.Background(), RegisterRequest{
		Name:     "  Sagar Golait  ",
		Email:    "  SAGAR@example.com ",
		Password: password,
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if user.Name != "Sagar Golait" {
		t.Errorf("unexpected name: %s", user.Name)
	}

	if user.Email != "sagar@example.com" {
		t.Errorf("unexpected email: %s", user.Email)
	}

	if repo.createdPasswordHash == password {
		t.Fatal("password was stored without hashing")
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(repo.createdPasswordHash),
		[]byte(password),
	); err != nil {
		t.Fatal("stored password hash does not match original password")
	}
}

func TestRegisterRejectsShortPassword(t *testing.T) {
	repo := &fakeUserRepository{}
	service := NewAuthService(repo)

	_, err := service.Register(context.Background(), RegisterRequest{
		Name:     "Sagar",
		Email:    "sagar@example.com",
		Password: "short",
	})

	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("expected *ValidationError, got %v", err)
	}

	if len(verr.Fields) != 1 || verr.Fields[0].Field != "password" {
		t.Fatalf("expected a single password field error, got %+v", verr.Fields)
	}

	if repo.createdPasswordHash != "" {
		t.Fatal("repository should not be called for invalid input")
	}
}

func TestRegisterReturnsDuplicateEmailError(t *testing.T) {
	repo := &fakeUserRepository{
		createErr: ErrEmailAlreadyExists,
	}

	service := NewAuthService(repo)

	_, err := service.Register(context.Background(), RegisterRequest{
		Name:     "Sagar",
		Email:    "sagar@example.com",
		Password: "StrongPassword123!",
	})

	if !errors.Is(err, ErrEmailAlreadyExists) {
		t.Fatalf("expected duplicate email error, got %v", err)
	}
}
