package dashboards

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/sagargolait/insightiq/services/api/internal/modules/auth"
	"github.com/sagargolait/insightiq/services/api/internal/modules/authorization"
)

// mount wraps the handler the same way main.go does, so {orgID} is populated
// from the URL just like in production.
func mount(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Mount("/orgs/{orgID}/dashboards", h.Routes())
	return r
}

func serve(router chi.Router, user *auth.User, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != nil {
		req = req.WithContext(auth.WithUser(req.Context(), *user))
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestHandlerCreateAndGetDashboard(t *testing.T) {
	repo := newFakeRepository()
	orgID := uuid.New()
	owner := auth.User{ID: uuid.New(), Email: "owner@example.test"}
	repo.addMember(orgID, owner.ID, authorization.RoleOwner)

	router := mount(NewHandler(NewService(repo)))
	base := "/orgs/" + orgID.String() + "/dashboards"

	created := serve(router, &owner, http.MethodPost, base, `{"name":"Sales"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d: %s", created.Code, created.Body)
	}
	var body struct {
		Data Dashboard `json:"data"`
	}
	if err := json.NewDecoder(created.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Name != "Sales" {
		t.Fatalf("name not persisted: %+v", body.Data)
	}

	got := serve(router, &owner, http.MethodGet, base+"/"+body.Data.ID.String(), "")
	if got.Code != http.StatusOK {
		t.Fatalf("get: want 200, got %d: %s", got.Code, got.Body)
	}
}

func TestHandlerUnauthenticatedIs401(t *testing.T) {
	router := mount(NewHandler(NewService(newFakeRepository())))
	got := serve(router, nil, http.MethodGet, "/orgs/"+uuid.New().String()+"/dashboards", "")
	if got.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", got.Code)
	}
}

func TestHandlerRejectsUnknownFields(t *testing.T) {
	repo := newFakeRepository()
	orgID := uuid.New()
	owner := auth.User{ID: uuid.New(), Email: "owner@example.test"}
	repo.addMember(orgID, owner.ID, authorization.RoleOwner)

	router := mount(NewHandler(NewService(repo)))
	got := serve(router, &owner, http.MethodPost, "/orgs/"+orgID.String()+"/dashboards",
		`{"name":"Sales","unexpected":true}`)
	if got.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: want 400, got %d: %s", got.Code, got.Body)
	}
}

func TestHandlerMissingDashboardIs404(t *testing.T) {
	repo := newFakeRepository()
	orgID := uuid.New()
	owner := auth.User{ID: uuid.New(), Email: "owner@example.test"}
	repo.addMember(orgID, owner.ID, authorization.RoleOwner)

	router := mount(NewHandler(NewService(repo)))
	got := serve(router, &owner, http.MethodGet,
		"/orgs/"+orgID.String()+"/dashboards/"+uuid.New().String(), "")
	if got.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d: %s", got.Code, got.Body)
	}
}

func TestHandlerWidgetLifecycle(t *testing.T) {
	repo := newFakeRepository()
	orgID := uuid.New()
	owner := auth.User{ID: uuid.New(), Email: "owner@example.test"}
	repo.addMember(orgID, owner.ID, authorization.RoleOwner)

	router := mount(NewHandler(NewService(repo)))
	base := "/orgs/" + orgID.String() + "/dashboards"

	created := serve(router, &owner, http.MethodPost, base, `{"name":"Board"}`)
	var db struct {
		Data Dashboard `json:"data"`
	}
	_ = json.NewDecoder(created.Body).Decode(&db)
	wbase := base + "/" + db.Data.ID.String() + "/widgets"

	w := serve(router, &owner, http.MethodPost, wbase,
		`{"type":"kpi","title":"Revenue","config":{"metric":"revenue"}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create widget: want 201, got %d: %s", w.Code, w.Body)
	}
	var wb struct {
		Data Widget `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&wb)

	del := serve(router, &owner, http.MethodDelete, wbase+"/"+wb.Data.ID.String(), "")
	if del.Code != http.StatusNoContent {
		t.Fatalf("delete widget: want 204, got %d: %s", del.Code, del.Body)
	}
}
