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

// descriptionPatch is the three-state result of parsing an update's raw
// description field: omitted (leave as-is), cleared (set NULL), or set.
type descriptionPatch struct {
	set   bool    // the field was present in the request
	value *string // nil means an explicit JSON null (clear); non-nil sets
}

// parseDescriptionPatch interprets the raw description from an update request.
// An empty RawMessage means the field was omitted (set=false). A JSON null
// clears the column; a JSON string is trimmed and length-checked.
func parseDescriptionPatch(raw json.RawMessage) (descriptionPatch, bool) {
	if len(raw) == 0 {
		return descriptionPatch{set: false}, true
	}
	if string(raw) == "null" {
		return descriptionPatch{set: true, value: nil}, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return descriptionPatch{}, false
	}
	trimmed := strings.TrimSpace(s)
	if len(trimmed) > maxDescriptionLen {
		return descriptionPatch{}, false
	}
	return descriptionPatch{set: true, value: &trimmed}, true
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
		return isNonEmptyString(cfg, "metric")
	case WidgetBarChart, WidgetLineChart:
		// Cartesian charts need both axes bound.
		return isNonEmptyString(cfg, "x") && isNonEmptyString(cfg, "y")
	case WidgetPieChart:
		// Proportional chart needs a category and a value.
		return isNonEmptyString(cfg, "category") && isNonEmptyString(cfg, "value")
	case WidgetTable:
		// Tabular widget needs a non-empty column list.
		return isNonEmptyStringArray(cfg, "columns")
	default:
		return false
	}
}

// isNonEmptyString reports whether key holds a JSON string that is not blank
// after trimming. A missing key, wrong type, or empty string all fail.
func isNonEmptyString(m map[string]json.RawMessage, key string) bool {
	raw, ok := m[key]
	if !ok {
		return false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return false
	}
	return strings.TrimSpace(s) != ""
}

// isNonEmptyStringArray reports whether key holds a non-empty JSON array of
// non-blank strings.
func isNonEmptyStringArray(m map[string]json.RawMessage, key string) bool {
	raw, ok := m[key]
	if !ok {
		return false
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err != nil {
		return false
	}
	if len(arr) == 0 {
		return false
	}
	for _, s := range arr {
		if strings.TrimSpace(s) == "" {
			return false
		}
	}
	return true
}
