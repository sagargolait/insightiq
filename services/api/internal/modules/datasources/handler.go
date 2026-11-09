package datasources

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

// Routes is mounted at /orgs/{orgID}/data-sources behind auth.RequireSession.
// orgID is read from the parent route and used as the RLS tenant.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.Create)
	r.Get("/", h.List)

	r.Route("/{sourceID}", func(r chi.Router) {
		r.Get("/", h.Get)
		r.Patch("/", h.Update)
		r.Delete("/", h.Delete)
		r.Post("/test", h.Test)
		r.Post("/schema/refresh", h.RefreshSchema)
		r.Get("/schema", h.GetSchema)
		r.Post("/query", h.Query)
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
	case errors.Is(err, ErrEmptyQuery),
		errors.Is(err, ErrMultipleStatement),
		errors.Is(err, ErrNotReadOnly),
		errors.Is(err, ErrForbiddenTable):
		writeError(w, http.StatusUnprocessableEntity, "INVALID_QUERY", queryReason(err))
	case errors.Is(err, ErrOrganizationNotFound):
		writeError(w, http.StatusNotFound, "ORGANIZATION_NOT_FOUND", "Organization not found")
	case errors.Is(err, ErrDataSourceNotFound):
		writeError(w, http.StatusNotFound, "DATA_SOURCE_NOT_FOUND", "Data source not found")
	case errors.Is(err, ErrForbidden):
		writeError(w, http.StatusForbidden, "FORBIDDEN", "You do not have permission to do this")
	case errors.Is(err, ErrNameTaken):
		writeError(w, http.StatusConflict, "NAME_TAKEN", "A data source with this name already exists")
	case errors.Is(err, ErrNotActive):
		writeError(w, http.StatusConflict, "NOT_ACTIVE", "The data source must pass a connection test first")
	case errors.Is(err, ErrNoCredentials):
		writeError(w, http.StatusConflict, "NO_CREDENTIALS", "The data source has no stored credentials")
	case errors.Is(err, ErrConnectionFailed):
		writeError(w, http.StatusBadGateway, "CONNECTION_FAILED", "Could not connect to the data source")
	case errors.Is(err, ErrResultTooLarge):
		writeError(w, http.StatusUnprocessableEntity, "RESULT_TOO_LARGE", "The query result is too large; narrow it down")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Something went wrong")
	}
}

// queryReason maps a validation sentinel to a user-facing reason without
// leaking query internals.
func queryReason(err error) string {
	switch {
	case errors.Is(err, ErrEmptyQuery):
		return "The query is empty"
	case errors.Is(err, ErrMultipleStatement):
		return "Only a single statement is allowed"
	case errors.Is(err, ErrForbiddenTable):
		return "The query references a restricted schema"
	default:
		return "Only read-only SELECT queries are allowed"
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

// orgID is the parent route param (/orgs/{orgID}/data-sources).
func (h *Handler) orgID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	return pathUUID(w, r, "orgID", ErrOrganizationNotFound)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := h.orgID(w, r)
	if !ok {
		return
	}

	var req CreateDataSourceRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	source, err := h.service.Create(r.Context(), user, orgID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": source})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := h.orgID(w, r)
	if !ok {
		return
	}

	sources, err := h.service.List(r.Context(), user, orgID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": sources})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := h.orgID(w, r)
	if !ok {
		return
	}
	sourceID, ok := pathUUID(w, r, "sourceID", ErrDataSourceNotFound)
	if !ok {
		return
	}

	source, err := h.service.Get(r.Context(), user, orgID, sourceID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": source})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := h.orgID(w, r)
	if !ok {
		return
	}
	sourceID, ok := pathUUID(w, r, "sourceID", ErrDataSourceNotFound)
	if !ok {
		return
	}

	var req UpdateDataSourceRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	source, err := h.service.Update(r.Context(), user, orgID, sourceID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": source})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := h.orgID(w, r)
	if !ok {
		return
	}
	sourceID, ok := pathUUID(w, r, "sourceID", ErrDataSourceNotFound)
	if !ok {
		return
	}

	if err := h.service.Delete(r.Context(), user, orgID, sourceID); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Test(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := h.orgID(w, r)
	if !ok {
		return
	}
	sourceID, ok := pathUUID(w, r, "sourceID", ErrDataSourceNotFound)
	if !ok {
		return
	}

	source, err := h.service.TestConnection(r.Context(), user, orgID, sourceID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": source})
}

func (h *Handler) RefreshSchema(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := h.orgID(w, r)
	if !ok {
		return
	}
	sourceID, ok := pathUUID(w, r, "sourceID", ErrDataSourceNotFound)
	if !ok {
		return
	}

	cols, err := h.service.RefreshSchema(r.Context(), user, orgID, sourceID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": cols})
}

func (h *Handler) GetSchema(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := h.orgID(w, r)
	if !ok {
		return
	}
	sourceID, ok := pathUUID(w, r, "sourceID", ErrDataSourceNotFound)
	if !ok {
		return
	}

	cols, err := h.service.GetSchema(r.Context(), user, orgID, sourceID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": cols})
}

func (h *Handler) Query(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := h.orgID(w, r)
	if !ok {
		return
	}
	sourceID, ok := pathUUID(w, r, "sourceID", ErrDataSourceNotFound)
	if !ok {
		return
	}

	var req QueryRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	result, err := h.service.RunQuery(r.Context(), user, orgID, sourceID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}
