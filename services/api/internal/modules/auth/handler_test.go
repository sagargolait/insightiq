package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeAuthRepository struct {
	fakeUserRepository
	sessions    map[string]User
	findErr     error
	revokedHash string
}

func (f *fakeAuthRepository) FindUserByEmail(context.Context, string) (User, string, error) {
	return User{}, "", ErrInvalidCredentials
}

func (f *fakeAuthRepository) CreateSession(context.Context, uuid.UUID, string, time.Time) error {
	return nil
}

func (f *fakeAuthRepository) FindUserBySessionHash(_ context.Context, tokenHash string) (User, error) {
	if f.findErr != nil {
		return User{}, f.findErr
	}

	user, ok := f.sessions[tokenHash]
	if !ok {
		return User{}, ErrInvalidSession
	}

	return user, nil
}

func (f *fakeAuthRepository) RevokeSession(_ context.Context, tokenHash string) error {
	f.revokedHash = tokenHash
	delete(f.sessions, tokenHash)
	return nil
}

func newSessionTestHandler(t *testing.T) (*Handler, *fakeAuthRepository, string, User) {
	t.Helper()

	token, err := GenerateSessionToken()
	if err != nil {
		t.Fatal(err)
	}

	user := User{ID: uuid.New(), Name: "Sagar", Email: "sagar@example.com", Status: "active"}
	repo := &fakeAuthRepository{
		sessions: map[string]User{HashSessionToken(token): user},
	}

	return NewHandler(NewAuthService(repo)), repo, token, user
}

func serveProtected(h *Handler, cookieValue string) (*httptest.ResponseRecorder, *User) {
	var seen *User

	protected := h.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, ok := UserFromContext(r.Context()); ok {
			seen = &user
		}
		w.WriteHeader(http.StatusOK)
	}))

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if cookieValue != "" {
		request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: cookieValue})
	}

	response := httptest.NewRecorder()
	protected.ServeHTTP(response, request)

	return response, seen
}

func TestRequireSessionAllowsValidSession(t *testing.T) {
	handler, _, token, user := newSessionTestHandler(t)

	response, seen := serveProtected(handler, token)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}

	if seen == nil || seen.ID != user.ID {
		t.Fatal("expected authenticated user in request context")
	}
}

func TestRequireSessionRejectsMissingCookie(t *testing.T) {
	handler, _, _, _ := newSessionTestHandler(t)

	response, seen := serveProtected(handler, "")

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}

	if seen != nil {
		t.Fatal("next handler should not run")
	}
}

func TestRequireSessionRejectsUnknownAndMalformedTokens(t *testing.T) {
	handler, _, _, _ := newSessionTestHandler(t)

	unknown, err := GenerateSessionToken()
	if err != nil {
		t.Fatal(err)
	}

	for _, value := range []string{unknown, "not-a-token"} {
		response, seen := serveProtected(handler, value)

		if response.Code != http.StatusUnauthorized {
			t.Fatalf("token %q: expected 401, got %d", value, response.Code)
		}

		if seen != nil {
			t.Fatalf("token %q: next handler should not run", value)
		}

		if cookie := response.Result().Cookies(); len(cookie) != 1 || cookie[0].MaxAge >= 0 {
			t.Fatalf("token %q: expected stale cookie to be cleared", value)
		}
	}
}

func TestRequireSessionReturns500OnRepositoryFailure(t *testing.T) {
	handler, repo, token, _ := newSessionTestHandler(t)
	repo.findErr = errors.New("database unavailable")

	response, _ := serveProtected(handler, token)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", response.Code)
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	handler, repo, token, _ := newSessionTestHandler(t)

	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
	response := httptest.NewRecorder()

	handler.Logout(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", response.Code)
	}

	if repo.revokedHash != HashSessionToken(token) {
		t.Fatal("expected session to be revoked by hash")
	}

	if after, _ := serveProtected(handler, token); after.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked session to be rejected, got %d", after.Code)
	}
}
