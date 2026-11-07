package dashboards

import (
	"encoding/json"
	"strings"
)

const (
	maxNameLen        = 200
	maxDescriptionLen = 2000
	maxTitleLen       = 150
	maxGridCells      = 1000 // generous upper bound on a grid coordinate/span
)

// validWidgetTypes is the set the service accepts; it mirrors the database
// CHECK constraint in migration 000004.
var validWidgetTypes = map[string]struct{}{
	WidgetKPI:       {},
	WidgetBarChart:  {},
	WidgetLineChart: {},
	WidgetPieChart:  {},
	WidgetTable:     {},
}

func isValidWidgetType(t string) bool {
	_, ok := validWidgetTypes[t]
	return ok
}

// validateName trims and bounds a dashboard name.
func validateName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxNameLen {
		return "", false
	}
	return name, true
}

// validateDescription bounds an optional description. A nil pointer is valid
// (no description); a present value is trimmed and length-checked.
func validateDescription(desc *string) (*string, bool) {
	if desc == nil {
		return nil, true
	}
	trimmed := strings.TrimSpace(*desc)
	if len(trimmed) > maxDescriptionLen {
		return nil, false
	}
	return &trimmed, true
}

func validateTitle(title string) (string, bool) {
	title = strings.TrimSpace(title)
	if title == "" || len(title) > maxTitleLen {
		return "", false
	}
	return title, true
}

// validateJSONObject ensures raw, if present, is a JSON object. An empty
// RawMessage means "not supplied" and is left for the repository to default.
func validateJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var obj map[string]json.RawMessage
	return json.Unmarshal(raw, &obj) == nil
}

// gridRect is the shape both position and size decode to for bounds checking.
type gridRect struct {
	X *int `json:"x"`
	Y *int `json:"y"`
	W *int `json:"w"`
	H *int `json:"h"`
}

// validatePosition requires non-negative x/y when supplied.
func validatePosition(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var r gridRect
	if err := json.Unmarshal(raw, &r); err != nil {
		return false
	}
	return inRange(r.X, 0, maxGridCells) && inRange(r.Y, 0, maxGridCells)
}

// validateSize requires positive w/h when supplied (a widget cannot be zero
// cells wide or tall).
func validateSize(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var r gridRect
	if err := json.Unmarshal(raw, &r); err != nil {
		return false
	}
	return inRange(r.W, 1, maxGridCells) && inRange(r.H, 1, maxGridCells)
}

// inRange returns true when v is nil (field omitted) or within [min,max].
func inRange(v *int, min, max int) bool {
	if v == nil {
		return true
	}
	return *v >= min && *v <= max
}

// validateConfig enforces per-type config rules. Config must be a JSON object;
// chart and table widgets require a data-binding field.
func validateConfig(widgetType string, raw json.RawMessage) bool {
	if !validateJSONObject(raw) {
		return false
	}
	// An absent config is allowed only for KPI, which can render from a single
	// metric reference added later; every other type needs an explicit config.
	if len(raw) == 0 {
		return widgetType == WidgetKPI
	}

	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return false
	}

	switch widgetType {
	case WidgetKPI:
		// Needs a single metric to display.
		return has(cfg, "metric")
	case WidgetBarChart, WidgetLineChart:
		// Cartesian charts need both axes bound.
		return has(cfg, "x") && has(cfg, "y")
	case WidgetPieChart:
		// Proportional chart needs a category and a value.
		return has(cfg, "category") && has(cfg, "value")
	case WidgetTable:
		// Tabular widget needs a column list.
		return has(cfg, "columns")
	default:
		return false
	}
}

func has(m map[string]json.RawMessage, key string) bool {
	_, ok := m[key]
	return ok
}
