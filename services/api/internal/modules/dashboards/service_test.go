package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/sagargolait/insightiq/services/api/internal/modules/auth"
	"github.com/sagargolait/insightiq/services/api/internal/modules/authorization"
)

// fakeRepository keeps dashboards and widgets in memory and enforces the same
// not-found semantics as the Postgres repository. Role lookups come from a
// per-org member map so tests can exercise the permission matrix.
type fakeRepository struct {
	members    map[uuid.UUID]map[uuid.UUID]string // orgID -> userID -> role
	dashboards map[uuid.UUID]Dashboard
	widgets    map[uuid.UUID]Widget
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		members:    map[uuid.UUID]map[uuid.UUID]string{},
		dashboards: map[uuid.UUID]Dashboard{},
		widgets:    map[uuid.UUID]Widget{},
	}
}

func (f *fakeRepository) addMember(orgID, userID uuid.UUID, role string) {
	if f.members[orgID] == nil {
		f.members[orgID] = map[uuid.UUID]string{}
	}
	f.members[orgID][userID] = role
}

func (f *fakeRepository) MemberRole(_ context.Context, orgID, userID uuid.UUID) (string, bool, error) {
	role, ok := f.members[orgID][userID]
	return role, ok, nil
}

func (f *fakeRepository) CreateDashboard(_ context.Context, d NewDashboard) (Dashboard, error) {
	dash := Dashboard{
		ID:             uuid.New(),
		OrganizationID: d.OrganizationID,
		Name:           d.Name,
		Description:    d.Description,
		Layout:         orDefault(d.Layout),
		CreatedBy:      &d.CreatedBy,
		UpdatedBy:      &d.CreatedBy,
	}
	f.dashboards[dash.ID] = dash
	return dash, nil
}

func (f *fakeRepository) ListDashboards(_ context.Context, orgID uuid.UUID) ([]Dashboard, error) {
	var out []Dashboard
	for _, d := range f.dashboards {
		if d.OrganizationID == orgID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeRepository) GetDashboard(_ context.Context, orgID, dashboardID uuid.UUID) (Dashboard, error) {
	d, ok := f.dashboards[dashboardID]
	if !ok || d.OrganizationID != orgID {
		return Dashboard{}, ErrDashboardNotFound
	}
	return d, nil
}

func (f *fakeRepository) UpdateDashboard(_ context.Context, orgID, dashboardID uuid.UUID, p DashboardPatch) (Dashboard, error) {
	d, ok := f.dashboards[dashboardID]
	if !ok || d.OrganizationID != orgID {
		return Dashboard{}, ErrDashboardNotFound
	}
	if p.Name != nil {
		d.Name = *p.Name
	}
	if p.Description != nil {
		d.Description = p.Description
	}
	if len(p.Layout) > 0 {
		d.Layout = p.Layout
	}
	d.UpdatedBy = &p.UpdatedBy
	f.dashboards[dashboardID] = d
	return d, nil
}

func (f *fakeRepository) DeleteDashboard(_ context.Context, orgID, dashboardID uuid.UUID) error {
	d, ok := f.dashboards[dashboardID]
	if !ok || d.OrganizationID != orgID {
		return ErrDashboardNotFound
	}
	delete(f.dashboards, dashboardID)
	return nil
}

func (f *fakeRepository) CreateWidget(_ context.Context, w NewWidget) (Widget, error) {
	d, ok := f.dashboards[w.DashboardID]
	if !ok || d.OrganizationID != w.OrganizationID {
		return Widget{}, ErrDashboardNotFound
	}
	widget := Widget{
		ID:             uuid.New(),
		OrganizationID: w.OrganizationID,
		DashboardID:    w.DashboardID,
		Type:           w.Type,
		Title:          w.Title,
		Config:         orDefault(w.Config),
		Position:       orDefault(w.Position),
		Size:           orDefault(w.Size),
	}
	f.widgets[widget.ID] = widget
	return widget, nil
}

func (f *fakeRepository) ListWidgets(_ context.Context, orgID, dashboardID uuid.UUID) ([]Widget, error) {
	d, ok := f.dashboards[dashboardID]
	if !ok || d.OrganizationID != orgID {
		return nil, ErrDashboardNotFound
	}
	var out []Widget
	for _, wdg := range f.widgets {
		if wdg.DashboardID == dashboardID {
			out = append(out, wdg)
		}
	}
	return out, nil
}

func (f *fakeRepository) GetWidget(_ context.Context, orgID, dashboardID, widgetID uuid.UUID) (Widget, error) {
	wdg, ok := f.widgets[widgetID]
	if !ok || wdg.OrganizationID != orgID || wdg.DashboardID != dashboardID {
		return Widget{}, ErrWidgetNotFound
	}
	return wdg, nil
}

func (f *fakeRepository) UpdateWidget(_ context.Context, orgID, dashboardID, widgetID uuid.UUID, p WidgetPatch) (Widget, error) {
	wdg, ok := f.widgets[widgetID]
	if !ok || wdg.OrganizationID != orgID || wdg.DashboardID != dashboardID {
		return Widget{}, ErrWidgetNotFound
	}
	if p.Title != nil {
		wdg.Title = *p.Title
	}
	if len(p.Config) > 0 {
		wdg.Config = p.Config
	}
	if len(p.Position) > 0 {
		wdg.Position = p.Position
	}
	if len(p.Size) > 0 {
		wdg.Size = p.Size
	}
	f.widgets[widgetID] = wdg
	return wdg, nil
}

func (f *fakeRepository) DeleteWidget(_ context.Context, orgID, dashboardID, widgetID uuid.UUID) error {
	wdg, ok := f.widgets[widgetID]
	if !ok || wdg.OrganizationID != orgID || wdg.DashboardID != dashboardID {
		return ErrWidgetNotFound
	}
	delete(f.widgets, widgetID)
	return nil
}

func orDefault(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

// fixture wires a service over a fake repo with one org and one owner.
type fixture struct {
	repo  *fakeRepository
	svc   *Service
	orgID uuid.UUID
	owner auth.User
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	repo := newFakeRepository()
	orgID := uuid.New()
	owner := auth.User{ID: uuid.New(), Email: "owner@example.test"}
	repo.addMember(orgID, owner.ID, authorization.RoleOwner)
	return &fixture{repo: repo, svc: NewService(repo), orgID: orgID, owner: owner}
}

func (fx *fixture) user(t *testing.T, role string) auth.User {
	t.Helper()
	u := auth.User{ID: uuid.New(), Email: role + "@example.test"}
	fx.repo.addMember(fx.orgID, u.ID, role)
	return u
}

func ctx() context.Context { return context.Background() }

// --- dashboard validation -------------------------------------------------

func TestCreateDashboardValidatesName(t *testing.T) {
	fx := newFixture(t)

	cases := map[string]string{
		"empty":      "",
		"whitespace": "   ",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := fx.svc.CreateDashboard(ctx(), fx.owner, fx.orgID, CreateDashboardRequest{Name: input})
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("want ErrInvalidInput, got %v", err)
			}
		})
	}

	if _, err := fx.svc.CreateDashboard(ctx(), fx.owner, fx.orgID,
		CreateDashboardRequest{Name: "Sales"}); err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
}

func TestCreateDashboardRejectsNonObjectLayout(t *testing.T) {
	fx := newFixture(t)
	_, err := fx.svc.CreateDashboard(ctx(), fx.owner, fx.orgID, CreateDashboardRequest{
		Name:   "Sales",
		Layout: json.RawMessage(`[1,2,3]`),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("want ErrInvalidInput for array layout, got %v", err)
	}
}

// --- role-based permissions ------------------------------------------------

func TestViewerCannotCreateDashboard(t *testing.T) {
	fx := newFixture(t)
	viewer := fx.user(t, authorization.RoleViewer)
	_, err := fx.svc.CreateDashboard(ctx(), viewer, fx.orgID, CreateDashboardRequest{Name: "X"})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer create: want ErrForbidden, got %v", err)
	}
}

func TestAnalystCanCreateButNotDelete(t *testing.T) {
	fx := newFixture(t)
	analyst := fx.user(t, authorization.RoleAnalyst)

	d, err := fx.svc.CreateDashboard(ctx(), analyst, fx.orgID, CreateDashboardRequest{Name: "Metrics"})
	if err != nil {
		t.Fatalf("analyst create: %v", err)
	}
	if err := fx.svc.DeleteDashboard(ctx(), analyst, fx.orgID, d.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("analyst delete: want ErrForbidden, got %v", err)
	}
}

func TestAdminCanDeleteDashboard(t *testing.T) {
	fx := newFixture(t)
	admin := fx.user(t, authorization.RoleAdmin)

	d, err := fx.svc.CreateDashboard(ctx(), admin, fx.orgID, CreateDashboardRequest{Name: "Metrics"})
	if err != nil {
		t.Fatalf("admin create: %v", err)
	}
	if err := fx.svc.DeleteDashboard(ctx(), admin, fx.orgID, d.ID); err != nil {
		t.Fatalf("admin delete: %v", err)
	}
}

func TestNonMemberGetsOrganizationNotFound(t *testing.T) {
	fx := newFixture(t)
	stranger := auth.User{ID: uuid.New(), Email: "stranger@example.test"}
	_, err := fx.svc.ListDashboards(ctx(), stranger, fx.orgID)
	if !errors.Is(err, ErrOrganizationNotFound) {
		t.Fatalf("non-member: want ErrOrganizationNotFound, got %v", err)
	}
}

// --- widget validation -----------------------------------------------------

func seedDashboard(t *testing.T, fx *fixture) uuid.UUID {
	t.Helper()
	d, err := fx.svc.CreateDashboard(ctx(), fx.owner, fx.orgID, CreateDashboardRequest{Name: "Board"})
	if err != nil {
		t.Fatalf("seed dashboard: %v", err)
	}
	return d.ID
}

func TestCreateWidgetRejectsUnknownType(t *testing.T) {
	fx := newFixture(t)
	dash := seedDashboard(t, fx)
	_, err := fx.svc.CreateWidget(ctx(), fx.owner, fx.orgID, dash, CreateWidgetRequest{
		Type:   "gauge",
		Title:  "Bad",
		Config: json.RawMessage(`{"metric":"x"}`),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown type: want ErrInvalidInput, got %v", err)
	}
}

func TestCreateWidgetValidatesConfigPerType(t *testing.T) {
	fx := newFixture(t)
	dash := seedDashboard(t, fx)

	cases := []struct {
		name    string
		req     CreateWidgetRequest
		wantErr bool
	}{
		{"kpi ok", CreateWidgetRequest{Type: WidgetKPI, Title: "Rev", Config: json.RawMessage(`{"metric":"revenue"}`)}, false},
		{"kpi missing metric", CreateWidgetRequest{Type: WidgetKPI, Title: "Rev", Config: json.RawMessage(`{"foo":1}`)}, true},
		{"bar ok", CreateWidgetRequest{Type: WidgetBarChart, Title: "B", Config: json.RawMessage(`{"x":"month","y":"total"}`)}, false},
		{"bar missing axis", CreateWidgetRequest{Type: WidgetBarChart, Title: "B", Config: json.RawMessage(`{"x":"month"}`)}, true},
		{"pie ok", CreateWidgetRequest{Type: WidgetPieChart, Title: "P", Config: json.RawMessage(`{"category":"region","value":"sales"}`)}, false},
		{"table ok", CreateWidgetRequest{Type: WidgetTable, Title: "T", Config: json.RawMessage(`{"columns":["a","b"]}`)}, false},
		{"table missing columns", CreateWidgetRequest{Type: WidgetTable, Title: "T", Config: json.RawMessage(`{}`)}, true},
		{"config not object", CreateWidgetRequest{Type: WidgetTable, Title: "T", Config: json.RawMessage(`"oops"`)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fx.svc.CreateWidget(ctx(), fx.owner, fx.orgID, dash, tc.req)
			if tc.wantErr && !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("want ErrInvalidInput, got %v", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
		})
	}
}

func TestCreateWidgetValidatesGeometry(t *testing.T) {
	fx := newFixture(t)
	dash := seedDashboard(t, fx)

	// Negative position is rejected.
	_, err := fx.svc.CreateWidget(ctx(), fx.owner, fx.orgID, dash, CreateWidgetRequest{
		Type:     WidgetKPI,
		Title:    "K",
		Config:   json.RawMessage(`{"metric":"x"}`),
		Position: json.RawMessage(`{"x":-1,"y":0}`),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative position: want ErrInvalidInput, got %v", err)
	}

	// Zero-sized widget is rejected.
	_, err = fx.svc.CreateWidget(ctx(), fx.owner, fx.orgID, dash, CreateWidgetRequest{
		Type:   WidgetKPI,
		Title:  "K",
		Config: json.RawMessage(`{"metric":"x"}`),
		Size:   json.RawMessage(`{"w":0,"h":4}`),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero width: want ErrInvalidInput, got %v", err)
	}
}

func TestWidgetCreateNeedsEditPermission(t *testing.T) {
	fx := newFixture(t)
	dash := seedDashboard(t, fx)
	viewer := fx.user(t, authorization.RoleViewer)
	_, err := fx.svc.CreateWidget(ctx(), viewer, fx.orgID, dash, CreateWidgetRequest{
		Type:   WidgetKPI,
		Title:  "K",
		Config: json.RawMessage(`{"metric":"x"}`),
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("viewer create widget: want ErrForbidden, got %v", err)
	}
}

func TestUpdateWidgetValidatesConfigAgainstStoredType(t *testing.T) {
	fx := newFixture(t)
	dash := seedDashboard(t, fx)
	w, err := fx.svc.CreateWidget(ctx(), fx.owner, fx.orgID, dash, CreateWidgetRequest{
		Type:   WidgetTable,
		Title:  "T",
		Config: json.RawMessage(`{"columns":["a"]}`),
	})
	if err != nil {
		t.Fatalf("seed widget: %v", err)
	}

	// A config valid for a chart but missing columns is invalid for a table.
	_, err = fx.svc.UpdateWidget(ctx(), fx.owner, fx.orgID, dash, w.ID, UpdateWidgetRequest{
		Config: json.RawMessage(`{"x":"a","y":"b"}`),
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("mismatched config: want ErrInvalidInput, got %v", err)
	}
}
