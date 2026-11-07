package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

type Handler struct {
	service *AuthService
}

func NewHandler(service *AuthService) *Handler {
	return &Handler{service: service}
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	RequestID string       `json:"request_id,omitempty"`
	Fields    []FieldError `json:"fields,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{
		Error: errorDetail{
			Code:    code,
			Message: message,
		},
	})
}

func writeValidationError(w http.ResponseWriter, verr *ValidationError) {
	writeJSON(w, http.StatusUnprocessableEntity, errorResponse{
		Error: errorDetail{
			Code:    "VALIDATION_ERROR",
			Message: "One or more fields are invalid",
			Fields:  verr.Fields,
		},
	})
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req RegisterRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest,
			"INVALID_REQUEST", "Invalid request body")
		return
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest,
			"INVALID_REQUEST", "Request must contain one JSON object")
		return
	}

	user, err := h.service.Register(r.Context(), req)

	if err != nil {
		var verr *ValidationError
		switch {
		case errors.As(err, &verr):
			writeValidationError(w, verr)

		case errors.Is(err, ErrInvalidInput):
			writeError(w, http.StatusUnprocessableEntity,
				"VALIDATION_ERROR", "Please provide valid registration details")

		case errors.Is(err, ErrEmailAlreadyExists):
			writeError(w, http.StatusConflict,
				"EMAIL_ALREADY_EXISTS", "An account with this email already exists")

		default:
			writeError(w, http.StatusInternalServerError,
				"INTERNAL_ERROR", "Unable to register user")
		}
		return
	}

	writeJSON(w, http.StatusCreated, RegisterResponse{
		Data: user,
	})
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest,
			"INVALID_REQUEST", "Invalid request body")
		return
	}

	user, token, err := h.service.Login(r.Context(), req)

	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized,
				"INVALID_CREDENTIALS", "Invalid email or password")
			return
		}

		writeError(w, http.StatusInternalServerError,
			"INTERNAL_ERROR", "Unable to log in")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Development only; configure for production.
		SameSite: http.SameSiteLaxMode,
		MaxAge:   7 * 24 * 60 * 60,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"data": user,
	})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized,
			"UNAUTHENTICATED", "Authentication required")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": user,
	})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		if err := h.service.Logout(r.Context(), cookie.Value); err != nil {
			writeError(w, http.StatusInternalServerError,
				"INTERNAL_ERROR", "Unable to log out")
			return
		}
	}

	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
