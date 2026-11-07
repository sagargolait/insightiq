package organizations

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/sagargolait/insightiq/services/api/internal/modules/auth"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Routes is mounted at /orgs behind auth.RequireSession.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.Create)
	r.Get("/", h.List)

	r.Route("/{orgID}", func(r chi.Router) {
		r.Get("/", h.Get)

		r.Get("/members", h.ListMembers)
		r.Patch("/members/{userID}", h.ChangeMemberRole)
		r.Delete("/members/{userID}", h.RemoveMember)

		r.Post("/invitations", h.CreateInvitation)
		r.Get("/invitations", h.ListInvitations)
		r.Delete("/invitations/{invitationID}", h.RevokeInvitation)
	})

	return r
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: errorDetail{Code: code, Message: message}})
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeError(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "Please provide valid details")
	case errors.Is(err, ErrOrganizationNotFound):
		writeError(w, http.StatusNotFound, "ORGANIZATION_NOT_FOUND", "Organization not found")
	case errors.Is(err, ErrMemberNotFound):
		writeError(w, http.StatusNotFound, "MEMBER_NOT_FOUND", "Member not found")
	case errors.Is(err, ErrInvitationNotFound):
		writeError(w, http.StatusNotFound, "INVITATION_NOT_FOUND", "Invitation not found or no longer valid")
	case errors.Is(err, ErrForbidden):
		writeError(w, http.StatusForbidden, "FORBIDDEN", "You do not have permission to do this")
	case errors.Is(err, ErrEmailMismatch):
		writeError(w, http.StatusForbidden, "EMAIL_MISMATCH", "This invitation was sent to a different email")
	case errors.Is(err, ErrSlugTaken):
		writeError(w, http.StatusConflict, "SLUG_TAKEN", "This organization slug is already taken")
	case errors.Is(err, ErrLastOwner):
		writeError(w, http.StatusConflict, "LAST_OWNER", "An organization must keep at least one owner")
	case errors.Is(err, ErrAlreadyMember):
		writeError(w, http.StatusConflict, "ALREADY_MEMBER", "This user is already a member")
	case errors.Is(err, ErrInvitationExists):
		writeError(w, http.StatusConflict, "INVITATION_EXISTS", "An open invitation already exists for this email")
	case errors.Is(err, ErrMemberHasDependents):
		writeError(w, http.StatusConflict, "MEMBER_HAS_DATA", "This member still owns data in the organization")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Something went wrong")
	}
}

// decodeJSON enforces one JSON object, no unknown fields, at most 1 MB.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return false
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Request must contain one JSON object")
		return false
	}

	return true
}

func currentUser(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Authentication required")
	}
	return user, ok
}

// pathUUID treats a malformed ID like a missing resource.
func pathUUID(w http.ResponseWriter, r *http.Request, name string, notFound error) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeServiceError(w, notFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}

	var req CreateOrganizationRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	org, err := h.service.CreateOrganization(r.Context(), user, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"data": org})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}

	orgs, err := h.service.ListOrganizations(r.Context(), user)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": orgs})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}

	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}

	org, err := h.service.GetOrganization(r.Context(), user, orgID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": org})
}

func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}

	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}

	members, err := h.service.ListMembers(r.Context(), user, orgID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": members})
}

func (h *Handler) ChangeMemberRole(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}

	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}

	targetID, ok := pathUUID(w, r, "userID", ErrMemberNotFound)
	if !ok {
		return
	}

	var req ChangeRoleRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	member, err := h.service.ChangeMemberRole(r.Context(), user, orgID, targetID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": member})
}

func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}

	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}

	targetID, ok := pathUUID(w, r, "userID", ErrMemberNotFound)
	if !ok {
		return
	}

	if err := h.service.RemoveMember(r.Context(), user, orgID, targetID); err != nil {
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}

	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}

	var req CreateInvitationRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	invitation, err := h.service.CreateInvitation(r.Context(), user, orgID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"data": invitation})
}

func (h *Handler) ListInvitations(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}

	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}

	invitations, err := h.service.ListInvitations(r.Context(), user, orgID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": invitations})
}

func (h *Handler) RevokeInvitation(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}

	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}

	invitationID, ok := pathUUID(w, r, "invitationID", ErrInvitationNotFound)
	if !ok {
		return
	}

	if err := h.service.RevokeInvitation(r.Context(), user, orgID, invitationID); err != nil {
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}

	var req AcceptInvitationRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	org, err := h.service.AcceptInvitation(r.Context(), user, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": org})
}
