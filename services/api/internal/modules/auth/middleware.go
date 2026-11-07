package auth

import (
	"context"
	"errors"
	"net/http"
)

const SessionCookieName = "insightiq_session"

type contextKey struct{}

var userContextKey = contextKey{}

func WithUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

func UserFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userContextKey).(User)
	return user, ok
}

// RequireSession rejects requests without a valid session cookie and
// stores the authenticated user in the request context.
func (h *Handler) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil || cookie.Value == "" {
			writeError(w, http.StatusUnauthorized,
				"UNAUTHENTICATED", "Authentication required")
			return
		}

		user, err := h.service.ValidateSession(r.Context(), cookie.Value)
		if err != nil {
			if errors.Is(err, ErrInvalidSession) {
				clearSessionCookie(w)
				writeError(w, http.StatusUnauthorized,
					"UNAUTHENTICATED", "Authentication required")
				return
			}

			writeError(w, http.StatusInternalServerError,
				"INTERNAL_ERROR", "Unable to validate session")
			return
		}

		next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Development only; configure for production.
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
