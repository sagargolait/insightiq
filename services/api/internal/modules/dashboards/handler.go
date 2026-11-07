package dashboards

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

// Routes is mounted at /orgs/{orgID}/dashboards behind auth.RequireSession.
// orgID is read from the parent route's URL param.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.Create)
	r.Get("/", h.List)

	r.Route("/{dashboardID}", func(r chi.Router) {
		r.Get("/", h.Get)
		r.Patch("/", h.Update)
		r.Delete("/", h.Delete)

		r.Post("/widgets", h.CreateWidget)
		r.Get("/widgets", h.ListWidgets)

		r.Route("/widgets/{widgetID}", func(r chi.Router) {
			r.Get("/", h.GetWidget)
			r.Patch("/", h.UpdateWidget)
			r.Delete("/", h.DeleteWidget)
		})
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
	case errors.Is(err, ErrDashboardNotFound):
		writeError(w, http.StatusNotFound, "DASHBOARD_NOT_FOUND", "Dashboard not found")
	case errors.Is(err, ErrWidgetNotFound):
		writeError(w, http.StatusNotFound, "WIDGET_NOT_FOUND", "Widget not found")
	case errors.Is(err, ErrForbidden):
		writeError(w, http.StatusForbidden, "FORBIDDEN", "You do not have permission to do this")
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

// Dashboard handlers ---------------------------------------------------------

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}

	var req CreateDashboardRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	dashboard, err := h.service.CreateDashboard(r.Context(), user, orgID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"data": dashboard})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}

	dashboards, err := h.service.ListDashboards(r.Context(), user, orgID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": dashboards})
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
	dashboardID, ok := pathUUID(w, r, "dashboardID", ErrDashboardNotFound)
	if !ok {
		return
	}

	dashboard, err := h.service.GetDashboard(r.Context(), user, orgID, dashboardID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": dashboard})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}
	dashboardID, ok := pathUUID(w, r, "dashboardID", ErrDashboardNotFound)
	if !ok {
		return
	}

	var req UpdateDashboardRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	dashboard, err := h.service.UpdateDashboard(r.Context(), user, orgID, dashboardID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": dashboard})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}
	dashboardID, ok := pathUUID(w, r, "dashboardID", ErrDashboardNotFound)
	if !ok {
		return
	}

	if err := h.service.DeleteDashboard(r.Context(), user, orgID, dashboardID); err != nil {
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Widget handlers ------------------------------------------------------------

func (h *Handler) CreateWidget(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}
	dashboardID, ok := pathUUID(w, r, "dashboardID", ErrDashboardNotFound)
	if !ok {
		return
	}

	var req CreateWidgetRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	widget, err := h.service.CreateWidget(r.Context(), user, orgID, dashboardID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"data": widget})
}

func (h *Handler) ListWidgets(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}
	dashboardID, ok := pathUUID(w, r, "dashboardID", ErrDashboardNotFound)
	if !ok {
		return
	}

	widgets, err := h.service.ListWidgets(r.Context(), user, orgID, dashboardID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": widgets})
}

func (h *Handler) GetWidget(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}
	dashboardID, ok := pathUUID(w, r, "dashboardID", ErrDashboardNotFound)
	if !ok {
		return
	}
	widgetID, ok := pathUUID(w, r, "widgetID", ErrWidgetNotFound)
	if !ok {
		return
	}

	widget, err := h.service.GetWidget(r.Context(), user, orgID, dashboardID, widgetID)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": widget})
}

func (h *Handler) UpdateWidget(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}
	dashboardID, ok := pathUUID(w, r, "dashboardID", ErrDashboardNotFound)
	if !ok {
		return
	}
	widgetID, ok := pathUUID(w, r, "widgetID", ErrWidgetNotFound)
	if !ok {
		return
	}

	var req UpdateWidgetRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	widget, err := h.service.UpdateWidget(r.Context(), user, orgID, dashboardID, widgetID, req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": widget})
}

func (h *Handler) DeleteWidget(w http.ResponseWriter, r *http.Request) {
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	orgID, ok := pathUUID(w, r, "orgID", ErrOrganizationNotFound)
	if !ok {
		return
	}
	dashboardID, ok := pathUUID(w, r, "dashboardID", ErrDashboardNotFound)
	if !ok {
		return
	}
	widgetID, ok := pathUUID(w, r, "widgetID", ErrWidgetNotFound)
	if !ok {
		return
	}

	if err := h.service.DeleteWidget(r.Context(), user, orgID, dashboardID, widgetID); err != nil {
		writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
