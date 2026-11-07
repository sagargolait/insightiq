package dashboards

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/sagargolait/insightiq/services/api/internal/modules/auth"
	"github.com/sagargolait/insightiq/services/api/internal/modules/authorization"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrForbidden    = errors.New("not allowed")
	// ErrOrganizationNotFound is returned to non-members so an org's existence
	// cannot be probed through the dashboard routes.
	ErrOrganizationNotFound = errors.New("organization not found")
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// requirePermission resolves the actor's role in orgID and checks it against
// the permission matrix. Non-members get ErrOrganizationNotFound (404);
// members lacking the permission get ErrForbidden (403).
func (s *Service) requirePermission(
	ctx context.Context,
	orgID, userID uuid.UUID,
	permission authorization.Permission,
) error {
	role, ok, err := s.repo.MemberRole(ctx, orgID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrOrganizationNotFound
	}
	if !authorization.Can(role, permission) {
		return ErrForbidden
	}
	return nil
}

// Dashboards -----------------------------------------------------------------

func (s *Service) CreateDashboard(
	ctx context.Context,
	actor auth.User,
	orgID uuid.UUID,
	req CreateDashboardRequest,
) (Dashboard, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.CreateDashboards); err != nil {
		return Dashboard{}, err
	}

	name, ok := validateName(req.Name)
	if !ok {
		return Dashboard{}, ErrInvalidInput
	}
	desc, ok := validateDescription(req.Description)
	if !ok {
		return Dashboard{}, ErrInvalidInput
	}
	if !validateJSONObject(req.Layout) {
		return Dashboard{}, ErrInvalidInput
	}

	return s.repo.CreateDashboard(ctx, NewDashboard{
		OrganizationID: orgID,
		Name:           name,
		Description:    desc,
		Layout:         req.Layout,
		CreatedBy:      actor.ID,
	})
}

func (s *Service) ListDashboards(ctx context.Context, actor auth.User, orgID uuid.UUID) ([]Dashboard, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.ViewDashboards); err != nil {
		return nil, err
	}
	return s.repo.ListDashboards(ctx, orgID)
}

func (s *Service) GetDashboard(ctx context.Context, actor auth.User, orgID, dashboardID uuid.UUID) (Dashboard, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.ViewDashboards); err != nil {
		return Dashboard{}, err
	}
	return s.repo.GetDashboard(ctx, orgID, dashboardID)
}

func (s *Service) UpdateDashboard(
	ctx context.Context,
	actor auth.User,
	orgID, dashboardID uuid.UUID,
	req UpdateDashboardRequest,
) (Dashboard, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.EditDashboards); err != nil {
		return Dashboard{}, err
	}

	patch := DashboardPatch{UpdatedBy: actor.ID}

	if req.Name != nil {
		name, ok := validateName(*req.Name)
		if !ok {
			return Dashboard{}, ErrInvalidInput
		}
		patch.Name = &name
	}
	if req.Description != nil {
		desc, ok := validateDescription(req.Description)
		if !ok {
			return Dashboard{}, ErrInvalidInput
		}
		patch.Description = desc
	}
	if len(req.Layout) > 0 {
		if !validateJSONObject(req.Layout) {
			return Dashboard{}, ErrInvalidInput
		}
		patch.Layout = req.Layout
	}

	return s.repo.UpdateDashboard(ctx, orgID, dashboardID, patch)
}

func (s *Service) DeleteDashboard(ctx context.Context, actor auth.User, orgID, dashboardID uuid.UUID) error {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.DeleteDashboards); err != nil {
		return err
	}
	return s.repo.DeleteDashboard(ctx, orgID, dashboardID)
}

// Widgets --------------------------------------------------------------------
//
// Widgets inherit a dashboard's permissions: viewing a dashboard lets you view
// its widgets; editing a dashboard lets you create, change and remove them.

func (s *Service) CreateWidget(
	ctx context.Context,
	actor auth.User,
	orgID, dashboardID uuid.UUID,
	req CreateWidgetRequest,
) (Widget, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.EditDashboards); err != nil {
		return Widget{}, err
	}

	if !isValidWidgetType(req.Type) {
		return Widget{}, ErrInvalidInput
	}
	title, ok := validateTitle(req.Title)
	if !ok {
		return Widget{}, ErrInvalidInput
	}
	if !validateConfig(req.Type, req.Config) ||
		!validatePosition(req.Position) ||
		!validateSize(req.Size) {
		return Widget{}, ErrInvalidInput
	}

	return s.repo.CreateWidget(ctx, NewWidget{
		OrganizationID: orgID,
		DashboardID:    dashboardID,
		Type:           req.Type,
		Title:          title,
		Config:         req.Config,
		Position:       req.Position,
		Size:           req.Size,
	})
}

func (s *Service) ListWidgets(ctx context.Context, actor auth.User, orgID, dashboardID uuid.UUID) ([]Widget, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.ViewDashboards); err != nil {
		return nil, err
	}
	return s.repo.ListWidgets(ctx, orgID, dashboardID)
}

func (s *Service) GetWidget(ctx context.Context, actor auth.User, orgID, dashboardID, widgetID uuid.UUID) (Widget, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.ViewDashboards); err != nil {
		return Widget{}, err
	}
	return s.repo.GetWidget(ctx, orgID, dashboardID, widgetID)
}

func (s *Service) UpdateWidget(
	ctx context.Context,
	actor auth.User,
	orgID, dashboardID, widgetID uuid.UUID,
	req UpdateWidgetRequest,
) (Widget, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.EditDashboards); err != nil {
		return Widget{}, err
	}

	// A widget's type is fixed at creation; only title/config/geometry change.
	// Validate config against the stored type so a partial update cannot slip
	// in a config that is malformed for that widget.
	patch := WidgetPatch{}
	if req.Title != nil {
		title, ok := validateTitle(*req.Title)
		if !ok {
			return Widget{}, ErrInvalidInput
		}
		patch.Title = &title
	}
	if len(req.Config) > 0 {
		existing, err := s.repo.GetWidget(ctx, orgID, dashboardID, widgetID)
		if err != nil {
			return Widget{}, err
		}
		if !validateConfig(existing.Type, req.Config) {
			return Widget{}, ErrInvalidInput
		}
		patch.Config = req.Config
	}
	if len(req.Position) > 0 {
		if !validatePosition(req.Position) {
			return Widget{}, ErrInvalidInput
		}
		patch.Position = req.Position
	}
	if len(req.Size) > 0 {
		if !validateSize(req.Size) {
			return Widget{}, ErrInvalidInput
		}
		patch.Size = req.Size
	}

	return s.repo.UpdateWidget(ctx, orgID, dashboardID, widgetID, patch)
}

func (s *Service) DeleteWidget(ctx context.Context, actor auth.User, orgID, dashboardID, widgetID uuid.UUID) error {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.EditDashboards); err != nil {
		return err
	}
	return s.repo.DeleteWidget(ctx, orgID, dashboardID, widgetID)
}
