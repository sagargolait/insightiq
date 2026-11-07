package organizations

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/sagargolait/insightiq/services/api/internal/modules/auth"
)

func serve(h *Handler, user *auth.User, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != nil {
		request = request.WithContext(auth.WithUser(request.Context(), *user))
	}

	response := httptest.NewRecorder()
	h.Routes().ServeHTTP(response, request)
	return response
}

func TestHandlerCreateAndGetOrganization(t *testing.T) {
	handler := NewHandler(NewService(newFakeRepository()))
	user := auth.User{ID: uuid.New(), Email: "owner@example.com"}

	created := serve(handler, &user, http.MethodPost, "/", `{"name":"Acme","slug":"acme"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", created.Code, created.Body)
	}

	var body struct {
		Data OrganizationWithRole `json:"data"`
	}
	if err := json.NewDecoder(created.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Role != RoleOwner {
		t.Fatalf("expected owner role, got %q", body.Data.Role)
	}

	got := serve(handler, &user, http.MethodGet, "/"+body.Data.ID.String(), "")
	if got.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", got.Code)
	}
}

func TestHandlerStatusCodes(t *testing.T) {
	handler := NewHandler(NewService(newFakeRepository()))
	user := auth.User{ID: uuid.New(), Email: "owner@example.com"}

	cases := []struct {
		name   string
		user   *auth.User
		method string
		path   string
		body   string
		want   int
	}{
		{"no user", nil, http.MethodGet, "/", "", http.StatusUnauthorized},
		{"bad org id", &user, http.MethodGet, "/not-a-uuid", "", http.StatusNotFound},
		{"unknown org", &user, http.MethodGet, "/" + uuid.NewString(), "", http.StatusNotFound},
		{"unknown field", &user, http.MethodPost, "/", `{"name":"A","slug":"abc","x":1}`, http.StatusBadRequest},
		{"invalid slug", &user, http.MethodPost, "/", `{"name":"A","slug":"A B"}`, http.StatusUnprocessableEntity},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := serve(handler, tc.user, tc.method, tc.path, tc.body)
			if response.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, response.Code, response.Body)
			}
		})
	}
}
