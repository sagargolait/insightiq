package datasources

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// queryTimeout bounds a single analytics query against an external source.
	queryTimeout = 10 * time.Second
	// maxRows caps how many rows a query may return. The query is wrapped so
	// the database stops producing rows past this limit.
	maxRows = 1000
	// maxResultBytes caps the serialized result size as a second guard against
	// a few very wide rows blowing up memory.
	maxResultBytes = 8 << 20 // 8 MiB
	// poolIdleTTL is how long an idle cached source pool is kept before close.
	poolIdleTTL = 10 * time.Minute
)

// Query validation errors.
var (
	ErrEmptyQuery        = errors.New("query is empty")
	ErrMultipleStatement = errors.New("only a single statement is allowed")
	ErrNotReadOnly       = errors.New("only read-only SELECT queries are allowed")
	ErrForbiddenTable    = errors.New("query references a restricted schema")
	ErrResultTooLarge    = errors.New("result exceeds the maximum size")
)

// QueryResult is the shape returned to analytics callers.
type QueryResult struct {
	Columns    []ColumnMeta `json:"columns"`
	Rows       [][]any      `json:"rows"`
	RowCount   int          `json:"row_count"`
	DurationMs int64        `json:"duration_ms"`
	Truncated  bool         `json:"truncated"`
}

type ColumnMeta struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// disallowedLeading are statement kinds that must never run. Only SELECT and
// WITH (a CTE that resolves to a SELECT) are permitted; everything else —
// writes, DDL, grants, session changes, procedure calls — is rejected.
var disallowedKeywords = map[string]struct{}{
	"insert": {}, "update": {}, "delete": {}, "drop": {}, "alter": {},
	"create": {}, "truncate": {}, "grant": {}, "revoke": {}, "copy": {},
	"call": {}, "do": {}, "set": {}, "reset": {}, "vacuum": {}, "analyze": {},
	"comment": {}, "merge": {}, "lock": {}, "refresh": {}, "reindex": {},
	"begin": {}, "commit": {}, "rollback": {}, "savepoint": {}, "explain": {},
	"prepare": {}, "execute": {}, "listen": {}, "notify": {}, "import": {},
}

// restrictedSchemas must never be referenced. These are Postgres internals; a
// customer query has no business reading them, and blocking them is defense in
// depth even though the executor only ever connects to the customer database.
var restrictedSchemas = []string{"pg_catalog", "information_schema", "pg_toast"}

// ValidateQuery enforces the read-only, single-statement policy. It returns the
// cleaned SQL (comments stripped) ready to be wrapped and executed.
//
// Parameters are always passed to the driver separately; this function never
// interpolates user values into SQL.
func ValidateQuery(sql string) (string, error) {
	cleaned := stripComments(sql)
	trimmed := strings.TrimSpace(cleaned)
	if trimmed == "" {
		return "", ErrEmptyQuery
	}

	// Reject multiple statements. A single trailing semicolon is tolerated and
	// stripped; any semicolon with non-whitespace after it means two statements
	// (classic stacked-query injection).
	withoutTrailing := strings.TrimRight(trimmed, "; \t\r\n")
	if strings.Contains(withoutTrailing, ";") {
		return "", ErrMultipleStatement
	}
	trimmed = withoutTrailing

	lower := strings.ToLower(trimmed)

	// Must start with SELECT or WITH.
	first := firstToken(lower)
	if first != "select" && first != "with" {
		return "", ErrNotReadOnly
	}

	// No forbidden statement keyword may appear as a token anywhere (catches a
	// WITH ... that smuggles a data-modifying CTE like WITH x AS (DELETE ...)).
	for _, tok := range tokenize(lower) {
		if _, bad := disallowedKeywords[tok]; bad {
			return "", ErrNotReadOnly
		}
	}

	// No reference to a restricted schema.
	for _, schema := range restrictedSchemas {
		if containsToken(lower, schema) {
			return "", ErrForbiddenTable
		}
	}

	return trimmed, nil
}

// Executor runs validated read-only queries against external sources, reusing a
// per-source connection pool. It is safe for concurrent use.
type Executor struct {
	mu    sync.Mutex
	pools map[uuid.UUID]*cachedPool
}

type cachedPool struct {
	pool     *pgxpool.Pool
	lastUsed time.Time
}

func NewExecutor() *Executor {
	return &Executor{pools: make(map[uuid.UUID]*cachedPool)}
}

// poolFor returns a cached pool for sourceID, creating one from cfg if absent.
// Decryption happens once per source; subsequent queries reuse the pool.
func (e *Executor) poolFor(ctx context.Context, sourceID uuid.UUID, cfg ConnectionConfig) (*pgxpool.Pool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if cp, ok := e.pools[sourceID]; ok {
		cp.lastUsed = time.Now()
		return cp.pool, nil
	}

	connCfg, err := buildConnConfig(cfg)
	if err != nil {
		return nil, err
	}
	poolCfg, err := pgxpool.ParseConfig("")
	if err != nil {
		return nil, fmt.Errorf("pool config: %w", err)
	}
	poolCfg.ConnConfig = connCfg
	poolCfg.MaxConns = 4
	poolCfg.MaxConnIdleTime = poolIdleTTL

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, classifyConnError(err)
	}
	e.pools[sourceID] = &cachedPool{pool: pool, lastUsed: time.Now()}
	return pool, nil
}

// Invalidate drops and closes the cached pool for a source (e.g. after its
// credentials or connection details change, or on delete).
func (e *Executor) Invalidate(sourceID uuid.UUID) {
	e.mu.Lock()
	cp, ok := e.pools[sourceID]
	if ok {
		delete(e.pools, sourceID)
	}
	e.mu.Unlock()
	if ok {
		cp.pool.Close()
	}
}

// Close shuts down every cached pool.
func (e *Executor) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, cp := range e.pools {
		cp.pool.Close()
		delete(e.pools, id)
	}
}

// Execute runs a pre-validated query against sourceID's pool inside a read-only
// transaction with a statement timeout and a hard row cap.
func (e *Executor) Execute(ctx context.Context, sourceID uuid.UUID, cfg ConnectionConfig, validatedSQL string, limit, offset int) (QueryResult, error) {
	if limit <= 0 || limit > maxRows {
		limit = maxRows
	}
	if offset < 0 {
		offset = 0
	}

	pool, err := e.poolFor(ctx, sourceID, cfg)
	if err != nil {
		return QueryResult{}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	start := time.Now()
	result, err := runInReadOnlyTx(runCtx, pool, validatedSQL, limit, offset)
	result.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		return QueryResult{}, err
	}
	return result, nil
}

func runInReadOnlyTx(ctx context.Context, pool *pgxpool.Pool, validatedSQL string, limit, offset int) (QueryResult, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return QueryResult{}, classifyConnError(err)
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck // read-only, no-op after rollback

	// Belt-and-braces timeout at the database so a slow query is killed server
	// side even if the context cancel races.
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", queryTimeout.Milliseconds())); err != nil {
		return QueryResult{}, classifyConnError(err)
	}

	// Wrap the validated query so the row cap and pagination are enforced by the
	// database regardless of what the inner query returns. The inner SQL is a
	// validated SELECT/WITH; limit/offset are integers, not user strings.
	wrapped := fmt.Sprintf("SELECT * FROM (%s) AS insightiq_q LIMIT %d OFFSET %d", validatedSQL, limit+1, offset)

	rows, err := tx.Query(ctx, wrapped)
	if err != nil {
		return QueryResult{}, classifyConnError(err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	cols := make([]ColumnMeta, len(fields))
	for i, f := range fields {
		cols[i] = ColumnMeta{Name: f.Name, Type: oidName(f.DataTypeOID)}
	}

	result := QueryResult{Columns: cols, Rows: [][]any{}}
	bytes := 0
	for rows.Next() {
		if len(result.Rows) >= limit {
			// We selected limit+1; seeing the extra row means there were more.
			result.Truncated = true
			break
		}
		vals, err := rows.Values()
		if err != nil {
			return QueryResult{}, fmt.Errorf("read row: %w", err)
		}
		bytes += approxRowBytes(vals)
		if bytes > maxResultBytes {
			return QueryResult{}, ErrResultTooLarge
		}
		result.Rows = append(result.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return QueryResult{}, classifyConnError(err)
	}
	result.RowCount = len(result.Rows)
	return result, nil
}

// --- small helpers ---------------------------------------------------------

// stripComments removes -- line comments and /* */ block comments so keyword
// checks cannot be fooled by hiding a statement inside a comment.
func stripComments(sql string) string {
	var b strings.Builder
	runes := []rune(sql)
	for i := 0; i < len(runes); i++ {
		// line comment
		if runes[i] == '-' && i+1 < len(runes) && runes[i+1] == '-' {
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
			continue
		}
		// block comment
		if runes[i] == '/' && i+1 < len(runes) && runes[i+1] == '*' {
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			i++ // land on '/', loop's i++ moves past it
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

func firstToken(s string) string {
	toks := tokenize(s)
	if len(toks) == 0 {
		return ""
	}
	return toks[0]
}

// tokenize splits on any non-alphanumeric/underscore character, lowercasing is
// assumed to have happened already by the caller.
func tokenize(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	})
}

func containsToken(s, token string) bool {
	for _, t := range tokenize(s) {
		if t == token {
			return true
		}
	}
	return false
}

func approxRowBytes(vals []any) int {
	n := 0
	for _, v := range vals {
		switch x := v.(type) {
		case nil:
			n += 1
		case string:
			n += len(x)
		case []byte:
			n += len(x)
		default:
			n += 16
		}
	}
	return n
}

// oidName maps a few common Postgres type OIDs to readable names; unknown OIDs
// fall back to the numeric OID. This keeps column metadata useful without a
// full type registry.
func oidName(oid uint32) string {
	switch oid {
	case 16:
		return "bool"
	case 20, 21, 23:
		return "integer"
	case 700, 701, 1700:
		return "numeric"
	case 25, 1042, 1043:
		return "text"
	case 1082:
		return "date"
	case 1114, 1184:
		return "timestamp"
	case 2950:
		return "uuid"
	case 114, 3802:
		return "json"
	default:
		return fmt.Sprintf("oid:%d", oid)
	}
}
