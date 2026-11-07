package dashboards

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Widget types supported by the platform. The database CHECK constraint
// (migration 000004) pins the same set; keep them in sync.
const (
	WidgetKPI       = "kpi"
	WidgetBarChart  = "bar_chart"
	WidgetLineChart = "line_chart"
	WidgetPieChart  = "pie_chart"
	WidgetTable     = "table"
)

// Dashboard is a named canvas of widgets owned by one organization.
type Dashboard struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"organization_id"`
	Name           string          `json:"name"`
	Description    *string         `json:"description"`
	Layout         json.RawMessage `json:"layout"`
	CreatedBy      *uuid.UUID      `json:"created_by"`
	UpdatedBy      *uuid.UUID      `json:"updated_by"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// Widget is a single visualization placed on a dashboard. Config shape depends
// on Type; Position and Size are grid geometry validated in the service.
type Widget struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"organization_id"`
	DashboardID    uuid.UUID       `json:"dashboard_id"`
	Type           string          `json:"type"`
	Title          string          `json:"title"`
	Config         json.RawMessage `json:"config"`
	Position       json.RawMessage `json:"position"`
	Size           json.RawMessage `json:"size"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type CreateDashboardRequest struct {
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	Layout      json.RawMessage `json:"layout"`
}

// UpdateDashboardRequest fields are pointers so an absent field means
// "leave unchanged" and an explicit value (including empty) means "set".
type UpdateDashboardRequest struct {
	Name        *string         `json:"name"`
	Description *string         `json:"description"`
	Layout      json.RawMessage `json:"layout"`
}

type CreateWidgetRequest struct {
	Type     string          `json:"type"`
	Title    string          `json:"title"`
	Config   json.RawMessage `json:"config"`
	Position json.RawMessage `json:"position"`
	Size     json.RawMessage `json:"size"`
}

type UpdateWidgetRequest struct {
	Title    *string         `json:"title"`
	Config   json.RawMessage `json:"config"`
	Position json.RawMessage `json:"position"`
	Size     json.RawMessage `json:"size"`
}

// NewDashboard carries validated values from service to repository.
type NewDashboard struct {
	OrganizationID uuid.UUID
	Name           string
	Description    *string
	Layout         json.RawMessage
	CreatedBy      uuid.UUID
}

// DashboardPatch holds only the fields an update should change; nil means
// leave the column as-is.
type DashboardPatch struct {
	Name        *string
	Description *string
	Layout      json.RawMessage
	UpdatedBy   uuid.UUID
}

type NewWidget struct {
	OrganizationID uuid.UUID
	DashboardID    uuid.UUID
	Type           string
	Title          string
	Config         json.RawMessage
	Position       json.RawMessage
	Size           json.RawMessage
}

type WidgetPatch struct {
	Title    *string
	Config   json.RawMessage
	Position json.RawMessage
	Size     json.RawMessage
}
