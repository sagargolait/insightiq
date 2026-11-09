package datasources

import (
	"time"

	"github.com/google/uuid"
)

// Supported data source types. PostgreSQL is the only one for Milestone 1; the
// column and validation keep room for more.
const (
	TypePostgres = "postgres"
)

// Connection states. The database CHECK constraint (migration 000005) pins the
// same set; keep them in sync.
const (
	StatusPending  = "pending"  // created, never successfully tested
	StatusActive   = "active"   // last connectivity test succeeded
	StatusFailed   = "failed"   // last connectivity test failed
	StatusDisabled = "disabled" // administratively turned off
)

// Job kinds and states for background work (migration 000005).
const (
	JobSchemaDiscovery = "schema_discovery"

	JobPending   = "pending"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
)

// DataSource is a connected external database owned by one organization. It
// never carries the decrypted credential; EncryptedCredentials and KeyVersion
// are the sealed form and are stripped from API responses via the json tags.
type DataSource struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	Name           string     `json:"name"`
	Type           string     `json:"type"`
	Status         string     `json:"status"`
	Host           string     `json:"host"`
	Port           int        `json:"port"`
	Database       string     `json:"database"`
	Username       string     `json:"username"`
	LastError      *string    `json:"last_error"`
	CreatedBy      *uuid.UUID `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`

	// EncryptedCredentials and KeyVersion never leave the repository layer in a
	// response; they are read only by the connector path. json:"-" guarantees
	// they are never serialized even if a DataSource is handed to writeJSON.
	EncryptedCredentials string `json:"-"`
	KeyVersion           string `json:"-"`
}

// CreatedByOrNil returns the creator's ID, or uuid.Nil when unknown. Used as the
// audit actor for background work the worker performs on the source's behalf.
func (d DataSource) CreatedByOrNil() uuid.UUID {
	if d.CreatedBy == nil {
		return uuid.Nil
	}
	return *d.CreatedBy
}

// ConnectionConfig is the decrypted set of values needed to dial the external
// database. It exists only transiently inside the connector path and is never
// persisted or logged.
type ConnectionConfig struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
}

// SchemaColumn is one discovered column of a table in a connected source.
type SchemaColumn struct {
	SchemaName   string    `json:"schema_name"`
	TableName    string    `json:"table_name"`
	ColumnName   string    `json:"column_name"`
	DataType     string    `json:"data_type"`
	Ordinal      int       `json:"ordinal"`
	DiscoveredAt time.Time `json:"discovered_at"`
}

// CreateDataSourceRequest is the create API body. Password is write-only: it is
// encrypted and never echoed back.
type CreateDataSourceRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// UpdateDataSourceRequest fields mean "leave unchanged" when nil. A non-nil
// Password triggers re-encryption. Type is immutable, so it is absent here.
type UpdateDataSourceRequest struct {
	Name     *string `json:"name"`
	Host     *string `json:"host"`
	Port     *int    `json:"port"`
	Database *string `json:"database"`
	Username *string `json:"username"`
	Password *string `json:"password"`
}

// QueryRequest is the analytics query body.
type QueryRequest struct {
	SQL      string `json:"sql"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

// NewDataSource carries validated, already-encrypted values from service to
// repository on create.
type NewDataSource struct {
	OrganizationID       uuid.UUID
	Name                 string
	Type                 string
	Host                 string
	Port                 int
	Database             string
	Username             string
	EncryptedCredentials string
	KeyVersion           string
	CreatedBy            uuid.UUID
}

// DataSourcePatch holds only the fields an update should change; nil means
// leave the column as-is. Encrypted credentials are set together with their
// key version when the caller supplied a new password.
type DataSourcePatch struct {
	Name                 *string
	Host                 *string
	Port                 *int
	Database             *string
	Username             *string
	EncryptedCredentials *string
	KeyVersion           *string
}
