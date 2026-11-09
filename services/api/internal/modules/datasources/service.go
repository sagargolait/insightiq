package datasources

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"

	"github.com/google/uuid"

	"github.com/sagargolait/insightiq/services/api/internal/modules/auth"
	"github.com/sagargolait/insightiq/services/api/internal/modules/authorization"
	"github.com/sagargolait/insightiq/services/api/internal/platform/observability"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrForbidden    = errors.New("not allowed")
	// ErrOrganizationNotFound is returned to non-members so an org's existence
	// cannot be probed through the data-source routes.
	ErrOrganizationNotFound = errors.New("organization not found")
	// ErrNotActive means a query/schema op hit a source that has not passed a
	// connectivity test.
	ErrNotActive = errors.New("data source is not active")
	// ErrConnectionFailed wraps a sanitized connectivity failure.
	ErrConnectionFailed = errors.New("could not connect to the data source")
	// ErrNoCredentials means the stored source has no sealed credential.
	ErrNoCredentials = errors.New("data source has no stored credentials")
)

// cipher is the subset of crypto.Cipher the service needs (interface so tests
// can substitute a trivial implementation).
type cipher interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(envelope string) (string, error)
	ActiveVersion() string
}

// connector is the subset of Connector the service needs.
type connector interface {
	Ping(ctx context.Context, cfg ConnectionConfig) error
	Discover(ctx context.Context, cfg ConnectionConfig) ([]SchemaColumn, error)
}

// executor is the subset of Executor the service needs.
type executor interface {
	Execute(ctx context.Context, sourceID uuid.UUID, cfg ConnectionConfig, validatedSQL string, limit, offset int) (QueryResult, error)
	Invalidate(sourceID uuid.UUID)
}

type Service struct {
	repo   Repository
	cipher cipher
	conn   connector
	exec   executor
}

func NewService(repo Repository, c cipher, conn connector, exec executor) *Service {
	return &Service{repo: repo, cipher: c, conn: conn, exec: exec}
}

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

// Create validates and encrypts, then stores a pending source.
func (s *Service) Create(ctx context.Context, actor auth.User, orgID uuid.UUID, req CreateDataSourceRequest) (DataSource, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.ManageDataSources); err != nil {
		return DataSource{}, err
	}

	name, ok := validateName(req.Name)
	if !ok {
		return DataSource{}, ErrInvalidInput
	}
	if req.Type != TypePostgres {
		return DataSource{}, ErrInvalidInput
	}
	cfg, ok := validateConnection(req.Host, req.Port, req.Database, req.Username, req.Password)
	if !ok {
		return DataSource{}, ErrInvalidInput
	}

	envelope, err := s.cipher.Encrypt(cfg.Password)
	if err != nil {
		return DataSource{}, err
	}

	return s.repo.Create(ctx, NewDataSource{
		OrganizationID:       orgID,
		Name:                 name,
		Type:                 req.Type,
		Host:                 cfg.Host,
		Port:                 cfg.Port,
		Database:             cfg.Database,
		Username:             cfg.Username,
		EncryptedCredentials: envelope,
		KeyVersion:           s.cipher.ActiveVersion(),
		CreatedBy:            actor.ID,
	}, actor.ID)
}

func (s *Service) List(ctx context.Context, actor auth.User, orgID uuid.UUID) ([]DataSource, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.ViewDataSources); err != nil {
		return nil, err
	}
	return s.repo.List(ctx, orgID)
}

func (s *Service) Get(ctx context.Context, actor auth.User, orgID, sourceID uuid.UUID) (DataSource, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.ViewDataSources); err != nil {
		return DataSource{}, err
	}
	return s.repo.Get(ctx, orgID, sourceID)
}

func (s *Service) Update(ctx context.Context, actor auth.User, orgID, sourceID uuid.UUID, req UpdateDataSourceRequest) (DataSource, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.ManageDataSources); err != nil {
		return DataSource{}, err
	}

	patch := DataSourcePatch{}
	changed := false

	if req.Name != nil {
		name, ok := validateName(*req.Name)
		if !ok {
			return DataSource{}, ErrInvalidInput
		}
		patch.Name = &name
		changed = true
	}
	if req.Host != nil {
		if !validateHost(*req.Host) {
			return DataSource{}, ErrInvalidInput
		}
		patch.Host = req.Host
		changed = true
	}
	if req.Port != nil {
		if !validatePort(*req.Port) {
			return DataSource{}, ErrInvalidInput
		}
		patch.Port = req.Port
		changed = true
	}
	if req.Database != nil {
		if !validateIdentifier(*req.Database) {
			return DataSource{}, ErrInvalidInput
		}
		patch.Database = req.Database
		changed = true
	}
	if req.Username != nil {
		if !validateIdentifier(*req.Username) {
			return DataSource{}, ErrInvalidInput
		}
		patch.Username = req.Username
		changed = true
	}
	if req.Password != nil {
		if strings.TrimSpace(*req.Password) == "" {
			return DataSource{}, ErrInvalidInput
		}
		envelope, err := s.cipher.Encrypt(*req.Password)
		if err != nil {
			return DataSource{}, err
		}
		version := s.cipher.ActiveVersion()
		patch.EncryptedCredentials = &envelope
		patch.KeyVersion = &version
		changed = true
	}

	if !changed {
		return DataSource{}, ErrInvalidInput
	}

	// Connection details changed: the cached pool is stale.
	s.exec.Invalidate(sourceID)

	return s.repo.Update(ctx, orgID, sourceID, patch, actor.ID)
}

func (s *Service) Delete(ctx context.Context, actor auth.User, orgID, sourceID uuid.UUID) error {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.ManageDataSources); err != nil {
		return err
	}
	if err := s.repo.Delete(ctx, orgID, sourceID, actor.ID); err != nil {
		return err
	}
	s.exec.Invalidate(sourceID)
	return nil
}

// TestConnection decrypts the stored credential, probes connectivity, and
// records the outcome as the source's status. The returned error is sanitized.
func (s *Service) TestConnection(ctx context.Context, actor auth.User, orgID, sourceID uuid.UUID) (DataSource, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.ManageDataSources); err != nil {
		return DataSource{}, err
	}

	cfg, err := s.connectionConfig(ctx, orgID, sourceID)
	if err != nil {
		return DataSource{}, err
	}

	log := observability.LoggerFrom(ctx)

	if pingErr := s.conn.Ping(ctx, cfg); pingErr != nil {
		reason := pingErr.Error() // already a sanitized bucket from classifyConnError
		_ = s.repo.UpdateStatus(ctx, orgID, sourceID, StatusFailed, &reason, actor.ID, auditFailed)
		observability.ConnectionTests.WithLabelValues(observability.OutcomeFailure).Inc()
		// reason is a sanitized bucket; safe to log. No host/credentials here.
		log.Warn("data source connection test failed",
			slog.String("source_id", sourceID.String()),
			slog.String("org_id", orgID.String()),
			slog.String("reason", reason))
		return DataSource{}, ErrConnectionFailed
	}

	if err := s.repo.UpdateStatus(ctx, orgID, sourceID, StatusActive, nil, actor.ID, auditActivated); err != nil {
		return DataSource{}, err
	}
	observability.ConnectionTests.WithLabelValues(observability.OutcomeSuccess).Inc()
	log.Info("data source activated",
		slog.String("source_id", sourceID.String()),
		slog.String("org_id", orgID.String()))
	// Fresh creds/connection: drop any stale cached pool.
	s.exec.Invalidate(sourceID)
	return s.repo.Get(ctx, orgID, sourceID)
}

// RefreshSchema discovers and stores the source's schema synchronously (so the
// caller gets the result immediately) and enqueues a discovery job so the
// background worker records and can retry the work out of band.
func (s *Service) RefreshSchema(ctx context.Context, actor auth.User, orgID, sourceID uuid.UUID) ([]SchemaColumn, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.ManageDataSources); err != nil {
		return nil, err
	}

	source, err := s.repo.Get(ctx, orgID, sourceID)
	if err != nil {
		return nil, err
	}
	if source.Status != StatusActive {
		return nil, ErrNotActive
	}

	// Enqueue is idempotent; a no-op if the worker already has one open.
	if err := s.repo.EnqueueJob(ctx, orgID, sourceID, JobSchemaDiscovery); err != nil {
		return nil, err
	}

	return s.discover(ctx, orgID, sourceID, actor.ID)
}

// DiscoverForJob runs schema discovery outside any HTTP request, for the
// background worker. It performs no permission check (the worker is trusted) and
// does not depend on request context. Returns the number of columns discovered.
func (s *Service) DiscoverForJob(ctx context.Context, orgID, sourceID uuid.UUID) (int, error) {
	source, err := s.repo.Get(ctx, orgID, sourceID)
	if err != nil {
		return 0, err
	}
	if source.Status != StatusActive {
		// Nothing to do for a source that never passed a connectivity test.
		return 0, ErrNotActive
	}
	cols, err := s.discover(ctx, orgID, sourceID, source.CreatedByOrNil())
	if err != nil {
		return 0, err
	}
	return len(cols), nil
}

// discover is the shared, idempotent unit of work: connect, introspect, replace
// stored schema. Used by both the synchronous HTTP path and the worker.
func (s *Service) discover(ctx context.Context, orgID, sourceID, actorID uuid.UUID) ([]SchemaColumn, error) {
	cfg, err := s.connectionConfig(ctx, orgID, sourceID)
	if err != nil {
		return nil, err
	}

	log := observability.LoggerFrom(ctx)

	cols, err := s.conn.Discover(ctx, cfg)
	if err != nil {
		observability.SchemaDiscoveries.WithLabelValues(observability.OutcomeFailure).Inc()
		log.Warn("schema discovery failed",
			slog.String("source_id", sourceID.String()),
			slog.String("org_id", orgID.String()))
		return nil, ErrConnectionFailed
	}
	if err := s.repo.ReplaceSchema(ctx, orgID, sourceID, cols, actorID); err != nil {
		observability.SchemaDiscoveries.WithLabelValues(observability.OutcomeFailure).Inc()
		return nil, err
	}
	observability.SchemaDiscoveries.WithLabelValues(observability.OutcomeSuccess).Inc()
	log.Info("schema discovered",
		slog.String("source_id", sourceID.String()),
		slog.String("org_id", orgID.String()),
		slog.Int("columns", len(cols)))
	return cols, nil
}

func (s *Service) GetSchema(ctx context.Context, actor auth.User, orgID, sourceID uuid.UUID) ([]SchemaColumn, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.ViewDataSources); err != nil {
		return nil, err
	}
	return s.repo.ListSchema(ctx, orgID, sourceID)
}

// RunQuery validates, executes, and records an analytics query.
func (s *Service) RunQuery(ctx context.Context, actor auth.User, orgID, sourceID uuid.UUID, req QueryRequest) (QueryResult, error) {
	if err := s.requirePermission(ctx, orgID, actor.ID, authorization.RunQueries); err != nil {
		return QueryResult{}, err
	}

	source, err := s.repo.Get(ctx, orgID, sourceID)
	if err != nil {
		return QueryResult{}, err
	}
	if source.Status != StatusActive {
		return QueryResult{}, ErrNotActive
	}

	log := observability.LoggerFrom(ctx)

	validated, err := ValidateQuery(req.SQL)
	if err != nil {
		// Rejected by the read-only policy before touching the database.
		observability.QueriesTotal.WithLabelValues(observability.OutcomeRejected).Inc()
		log.Warn("analytics query rejected",
			slog.String("source_id", sourceID.String()),
			slog.String("org_id", orgID.String()),
			slog.String("reason", err.Error())) // sentinel text, never the SQL
		return QueryResult{}, err // validation sentinels map to 422
	}

	cfg, err := s.connectionConfig(ctx, orgID, sourceID)
	if err != nil {
		return QueryResult{}, err
	}

	limit := req.PageSize
	offset := 0
	if req.Page > 1 && limit > 0 {
		offset = (req.Page - 1) * limit
	}

	result, err := s.exec.Execute(ctx, sourceID, cfg, validated, limit, offset)
	if err != nil {
		reason := err.Error()
		_ = s.repo.RecordQuery(ctx, orgID, sourceID, actor.ID, result.DurationMs, &reason)
		observability.QueriesTotal.WithLabelValues(observability.OutcomeFailure).Inc()
		log.Warn("analytics query failed",
			slog.String("source_id", sourceID.String()),
			slog.String("org_id", orgID.String()),
			slog.String("reason", reason))
		if errors.Is(err, ErrResultTooLarge) {
			return QueryResult{}, ErrResultTooLarge
		}
		return QueryResult{}, ErrConnectionFailed
	}
	_ = s.repo.RecordQuery(ctx, orgID, sourceID, actor.ID, result.DurationMs, nil)
	observability.QueriesTotal.WithLabelValues(observability.OutcomeSuccess).Inc()
	observability.QueryDuration.Observe(float64(result.DurationMs) / 1000.0)
	log.Info("analytics query executed",
		slog.String("source_id", sourceID.String()),
		slog.String("org_id", orgID.String()),
		slog.Int("row_count", result.RowCount),
		slog.Int64("duration_ms", result.DurationMs),
		slog.Bool("truncated", result.Truncated))
	return result, nil
}

// connectionConfig loads and decrypts a source's credential into a transient
// ConnectionConfig. The decrypted password lives only for the call.
func (s *Service) connectionConfig(ctx context.Context, orgID, sourceID uuid.UUID) (ConnectionConfig, error) {
	source, err := s.repo.Get(ctx, orgID, sourceID)
	if err != nil {
		return ConnectionConfig{}, err
	}
	envelope, _, err := s.repo.GetCredentials(ctx, orgID, sourceID)
	if err != nil {
		return ConnectionConfig{}, err
	}
	if envelope == "" {
		return ConnectionConfig{}, ErrNoCredentials
	}
	password, err := s.cipher.Decrypt(envelope)
	if err != nil {
		return ConnectionConfig{}, err
	}
	return ConnectionConfig{
		Host:     source.Host,
		Port:     source.Port,
		Database: source.Database,
		Username: source.Username,
		Password: password,
	}, nil
}

// --- validation ------------------------------------------------------------

func validateName(name string) (string, bool) {
	trimmed := strings.TrimSpace(name)
	if l := len(trimmed); l < 1 || l > 150 {
		return "", false
	}
	return trimmed, true
}

// validateConnection checks all connection fields and returns a config with the
// trimmed values (password is kept verbatim).
func validateConnection(host string, port int, database, username, password string) (ConnectionConfig, bool) {
	host = strings.TrimSpace(host)
	database = strings.TrimSpace(database)
	username = strings.TrimSpace(username)
	if !validateHost(host) || !validatePort(port) ||
		!validateIdentifier(database) || !validateIdentifier(username) ||
		strings.TrimSpace(password) == "" {
		return ConnectionConfig{}, false
	}
	return ConnectionConfig{
		Host:     host,
		Port:     port,
		Database: database,
		Username: username,
		Password: password,
	}, true
}

func validateHost(host string) bool {
	host = strings.TrimSpace(host)
	if l := len(host); l < 1 || l > 255 {
		return false
	}
	// Accept a hostname or an IP literal; reject anything with a scheme, path,
	// port, or whitespace so it cannot smuggle extra DSN parameters.
	if strings.ContainsAny(host, " \t\r\n/@?#") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return true
	}
	// Hostname: labels of letters/digits/hyphen/dot.
	for _, r := range host {
		if !(r == '.' || r == '-' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func validatePort(port int) bool {
	return port > 0 && port <= 65535
}

// validateIdentifier accepts a database/username value that is non-empty,
// bounded, and free of control characters and whitespace that could break the
// connection config.
func validateIdentifier(v string) bool {
	v = strings.TrimSpace(v)
	if l := len(v); l < 1 || l > 128 {
		return false
	}
	if strings.ContainsAny(v, " \t\r\n") {
		return false
	}
	return true
}
