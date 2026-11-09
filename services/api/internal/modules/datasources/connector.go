package datasources

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// connectTimeout bounds dialing and the connectivity probe. Kept short so a
// bad host fails fast instead of hanging an HTTP request.
const connectTimeout = 5 * time.Second

// Connection failure buckets. The service maps a raw driver error to one of
// these so the API never leaks a host, credential, or driver internal.
var (
	ErrConnAuth        = errors.New("authentication failed")
	ErrConnUnreachable = errors.New("database unreachable")
	ErrConnTimeout     = errors.New("connection timed out")
	ErrConnTLS         = errors.New("tls negotiation failed")
	ErrConnRejected    = errors.New("connection rejected")
)

// Connector dials external customer databases. It is the only place in the
// codebase that opens a connection to a database other than InsightIQ's own.
type Connector struct{}

func NewConnector() *Connector { return &Connector{} }

// buildConnConfig produces a pgx config for a read-only session against the
// external source. prefer-mode TLS is used (upgrade when the server supports
// it); the query executor additionally forces a read-only transaction.
func buildConnConfig(c ConnectionConfig) (*pgx.ConnConfig, error) {
	// Credentials are passed through the parsed config, never string-formatted
	// into a DSN that might be logged.
	cfg, err := pgx.ParseConfig("")
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.Host = c.Host
	cfg.Port = uint16(c.Port)
	cfg.Database = c.Database
	cfg.User = c.Username
	cfg.Password = c.Password
	cfg.ConnectTimeout = connectTimeout
	// Default to a read-only session at the connection level; the executor
	// also wraps each query in a read-only transaction (defense in depth).
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	cfg.RuntimeParams["application_name"] = "insightiq-analytics"
	return cfg, nil
}

// connect opens a single connection with the connect timeout applied.
func (Connector) connect(ctx context.Context, c ConnectionConfig) (*pgx.Conn, error) {
	cfg, err := buildConnConfig(c)
	if err != nil {
		return nil, err
	}
	dialCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	conn, err := pgx.ConnectConfig(dialCtx, cfg)
	if err != nil {
		return nil, classifyConnError(err)
	}
	return conn, nil
}

// Ping verifies connectivity by opening a connection and running a trivial
// read-only query. Returns a classified (sanitized) error on failure.
func (c Connector) Ping(ctx context.Context, cfg ConnectionConfig) error {
	conn, err := c.connect(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())

	queryCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	var one int
	if err := conn.QueryRow(queryCtx, "SELECT 1").Scan(&one); err != nil {
		return classifyConnError(err)
	}
	return nil
}

// Discover introspects the external database's non-system schemas and returns
// one SchemaColumn per column. System schemas are excluded.
func (c Connector) Discover(ctx context.Context, cfg ConnectionConfig) ([]SchemaColumn, error) {
	conn, err := c.connect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer conn.Close(context.Background())

	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	rows, err := conn.Query(queryCtx, `
        SELECT table_schema, table_name, column_name, data_type, ordinal_position
        FROM information_schema.columns
        WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
          AND table_schema NOT LIKE 'pg_%'
        ORDER BY table_schema, table_name, ordinal_position`)
	if err != nil {
		return nil, classifyConnError(err)
	}
	defer rows.Close()

	var cols []SchemaColumn
	for rows.Next() {
		var sc SchemaColumn
		if err := rows.Scan(&sc.SchemaName, &sc.TableName, &sc.ColumnName, &sc.DataType, &sc.Ordinal); err != nil {
			return nil, fmt.Errorf("scan schema row: %w", err)
		}
		cols = append(cols, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, classifyConnError(err)
	}
	return cols, nil
}

// classifyConnError maps a raw driver/network error to a sanitized bucket. The
// returned error intentionally carries no host, credential, or SQLSTATE detail
// that could aid probing; the original is deliberately discarded.
func classifyConnError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrConnTimeout
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "28P01" || pgErr.Code == "28000":
			return ErrConnAuth // invalid_password / invalid_authorization
		case pgErr.Code == "3D000":
			return ErrConnRejected // invalid_catalog_name (unknown database)
		case pgErr.Code == "08006" || pgErr.Code == "08001" || pgErr.Code == "08004":
			return ErrConnUnreachable // connection failures / rejected
		default:
			return ErrConnRejected
		}
	}

	// Network-level and TLS errors surface as plain errors; inspect the text
	// only to pick a bucket, never to forward it.
	msg := err.Error()
	switch {
	case containsAny(msg, "timeout", "timed out", "deadline exceeded"):
		return ErrConnTimeout
	case containsAny(msg, "tls", "certificate", "x509"):
		return ErrConnTLS
	case containsAny(msg, "refused", "no route", "no such host", "unreachable", "connect"):
		return ErrConnUnreachable
	default:
		return ErrConnRejected
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if indexFold(s, sub) {
			return true
		}
	}
	return false
}

// indexFold reports whether sub occurs in s, case-insensitively, without
// allocating a lowercased copy of the whole string.
func indexFold(s, sub string) bool {
	n, m := len(s), len(sub)
	if m == 0 {
		return true
	}
	for i := 0; i+m <= n; i++ {
		if equalFold(s[i:i+m], sub) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if lower(a[i]) != lower(b[i]) {
			return false
		}
	}
	return true
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
