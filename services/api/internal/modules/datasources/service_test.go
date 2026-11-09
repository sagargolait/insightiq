package datasources

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/sagargolait/insightiq/services/api/internal/modules/auth"
	"github.com/sagargolait/insightiq/services/api/internal/modules/authorization"
)

// --- fakes -----------------------------------------------------------------

type fakeRepo struct {
	members  map[uuid.UUID]map[uuid.UUID]string
	sources  map[uuid.UUID]DataSource
	creds    map[uuid.UUID]string // sourceID -> envelope
	schema   map[uuid.UUID][]SchemaColumn
	enqueued []uuid.UUID // sourceIDs passed to EnqueueJob
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		members: map[uuid.UUID]map[uuid.UUID]string{},
		sources: map[uuid.UUID]DataSource{},
		creds:   map[uuid.UUID]string{},
		schema:  map[uuid.UUID][]SchemaColumn{},
	}
}

func (f *fakeRepo) addMember(orgID, userID uuid.UUID, role string) {
	if f.members[orgID] == nil {
		f.members[orgID] = map[uuid.UUID]string{}
	}
	f.members[orgID][userID] = role
}

func (f *fakeRepo) MemberRole(_ context.Context, orgID, userID uuid.UUID) (string, bool, error) {
	role, ok := f.members[orgID][userID]
	return role, ok, nil
}

func (f *fakeRepo) Create(_ context.Context, d NewDataSource, _ uuid.UUID) (DataSource, error) {
	for _, s := range f.sources {
		if s.OrganizationID == d.OrganizationID && s.Name == d.Name {
			return DataSource{}, ErrNameTaken
		}
	}
	id := uuid.New()
	src := DataSource{
		ID: id, OrganizationID: d.OrganizationID, Name: d.Name, Type: d.Type,
		Status: StatusPending, Host: d.Host, Port: d.Port, Database: d.Database,
		Username: d.Username, CreatedBy: &d.CreatedBy,
	}
	f.sources[id] = src
	f.creds[id] = d.EncryptedCredentials
	return src, nil
}

func (f *fakeRepo) List(_ context.Context, orgID uuid.UUID) ([]DataSource, error) {
	var out []DataSource
	for _, s := range f.sources {
		if s.OrganizationID == orgID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeRepo) Get(_ context.Context, orgID, sourceID uuid.UUID) (DataSource, error) {
	s, ok := f.sources[sourceID]
	if !ok || s.OrganizationID != orgID {
		return DataSource{}, ErrDataSourceNotFound
	}
	return s, nil
}

func (f *fakeRepo) Update(_ context.Context, orgID, sourceID uuid.UUID, p DataSourcePatch, _ uuid.UUID) (DataSource, error) {
	s, ok := f.sources[sourceID]
	if !ok || s.OrganizationID != orgID {
		return DataSource{}, ErrDataSourceNotFound
	}
	if p.Name != nil {
		s.Name = *p.Name
	}
	if p.Host != nil {
		s.Host = *p.Host
	}
	if p.EncryptedCredentials != nil {
		f.creds[sourceID] = *p.EncryptedCredentials
		s.Status = StatusPending
	}
	f.sources[sourceID] = s
	return s, nil
}

func (f *fakeRepo) Delete(_ context.Context, orgID, sourceID uuid.UUID, _ uuid.UUID) error {
	s, ok := f.sources[sourceID]
	if !ok || s.OrganizationID != orgID {
		return ErrDataSourceNotFound
	}
	delete(f.sources, sourceID)
	return nil
}

func (f *fakeRepo) GetCredentials(_ context.Context, orgID, sourceID uuid.UUID) (string, string, error) {
	s, ok := f.sources[sourceID]
	if !ok || s.OrganizationID != orgID {
		return "", "", ErrDataSourceNotFound
	}
	return f.creds[sourceID], "v1", nil
}

func (f *fakeRepo) UpdateStatus(_ context.Context, orgID, sourceID uuid.UUID, status string, lastErr *string, _ uuid.UUID, _ string) error {
	s, ok := f.sources[sourceID]
	if !ok || s.OrganizationID != orgID {
		return ErrDataSourceNotFound
	}
	s.Status = status
	s.LastError = lastErr
	f.sources[sourceID] = s
	return nil
}

func (f *fakeRepo) ReplaceSchema(_ context.Context, orgID, sourceID uuid.UUID, cols []SchemaColumn, _ uuid.UUID) error {
	if s, ok := f.sources[sourceID]; !ok || s.OrganizationID != orgID {
		return ErrDataSourceNotFound
	}
	f.schema[sourceID] = cols
	return nil
}

func (f *fakeRepo) ListSchema(_ context.Context, orgID, sourceID uuid.UUID) ([]SchemaColumn, error) {
	if s, ok := f.sources[sourceID]; !ok || s.OrganizationID != orgID {
		return nil, ErrDataSourceNotFound
	}
	return f.schema[sourceID], nil
}

func (f *fakeRepo) RecordQuery(_ context.Context, _, _ uuid.UUID, _ uuid.UUID, _ int64, _ *string) error {
	return nil
}

func (f *fakeRepo) EnqueueJob(_ context.Context, _, sourceID uuid.UUID, _ string) error {
	f.enqueued = append(f.enqueued, sourceID)
	return nil
}

// fakeCipher is a trivial reversible transform; never used for real secrecy.
type fakeCipher struct{}

func (fakeCipher) Encrypt(p string) (string, error) { return "enc:" + p, nil }
func (fakeCipher) Decrypt(e string) (string, error) {
	if len(e) < 4 || e[:4] != "enc:" {
		return "", errors.New("bad envelope")
	}
	return e[4:], nil
}
func (fakeCipher) ActiveVersion() string { return "v1" }

type fakeConnector struct {
	pingErr error
	cols    []SchemaColumn
	gotPass string
}

func (c *fakeConnector) Ping(_ context.Context, cfg ConnectionConfig) error {
	c.gotPass = cfg.Password
	return c.pingErr
}
func (c *fakeConnector) Discover(_ context.Context, _ ConnectionConfig) ([]SchemaColumn, error) {
	return c.cols, c.pingErr
}

type fakeExecutor struct {
	result      QueryResult
	err         error
	invalidated []uuid.UUID
}

func (e *fakeExecutor) Execute(_ context.Context, _ uuid.UUID, _ ConnectionConfig, _ string, _, _ int) (QueryResult, error) {
	return e.result, e.err
}
func (e *fakeExecutor) Invalidate(id uuid.UUID) { e.invalidated = append(e.invalidated, id) }

// --- fixture ---------------------------------------------------------------

type fixture struct {
	repo  *fakeRepo
	conn  *fakeConnector
	exec  *fakeExecutor
	svc   *Service
	orgID uuid.UUID
}

func newFixture() *fixture {
	repo := newFakeRepo()
	conn := &fakeConnector{}
	exec := &fakeExecutor{}
	return &fixture{
		repo: repo, conn: conn, exec: exec,
		svc:   NewService(repo, fakeCipher{}, conn, exec),
		orgID: uuid.New(),
	}
}

func user() auth.User { return auth.User{ID: uuid.New()} }

func validCreate() CreateDataSourceRequest {
	return CreateDataSourceRequest{
		Name: "analytics-db", Type: TypePostgres, Host: "db.example.com",
		Port: 5432, Database: "analytics", Username: "readonly", Password: "s3cret",
	}
}

// --- tests -----------------------------------------------------------------

func TestCreateEncryptsAndStartsPending(t *testing.T) {
	fx := newFixture()
	u := user()
	fx.repo.addMember(fx.orgID, u.ID, authorization.RoleAdmin)

	src, err := fx.svc.Create(context.Background(), u, fx.orgID, validCreate())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if src.Status != StatusPending {
		t.Errorf("status = %q, want pending", src.Status)
	}
	// Credential stored must be the encrypted envelope, not the plaintext.
	if env := fx.repo.creds[src.ID]; env == "s3cret" || env != "enc:s3cret" {
		t.Errorf("stored credential = %q, want encrypted envelope", env)
	}
	// The DataSource must never carry the plaintext/envelope in its fields.
	if src.EncryptedCredentials != "" {
		t.Error("response DataSource leaked encrypted credentials")
	}
}

func TestCreateRequiresManagePermission(t *testing.T) {
	fx := newFixture()
	u := user()
	fx.repo.addMember(fx.orgID, u.ID, authorization.RoleViewer) // view only

	_, err := fx.svc.Create(context.Background(), u, fx.orgID, validCreate())
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer Create = %v, want ErrForbidden", err)
	}
}

func TestNonMemberSeesNotFound(t *testing.T) {
	fx := newFixture()
	u := user() // never added as member

	_, err := fx.svc.List(context.Background(), u, fx.orgID)
	if !errors.Is(err, ErrOrganizationNotFound) {
		t.Errorf("non-member List = %v, want ErrOrganizationNotFound", err)
	}
}

func TestCreateValidatesInput(t *testing.T) {
	fx := newFixture()
	u := user()
	fx.repo.addMember(fx.orgID, u.ID, authorization.RoleOwner)

	bad := []CreateDataSourceRequest{
		func() CreateDataSourceRequest { r := validCreate(); r.Name = ""; return r }(),
		func() CreateDataSourceRequest { r := validCreate(); r.Type = "mysql"; return r }(),
		func() CreateDataSourceRequest { r := validCreate(); r.Port = 0; return r }(),
		func() CreateDataSourceRequest { r := validCreate(); r.Port = 70000; return r }(),
		func() CreateDataSourceRequest { r := validCreate(); r.Host = "http://x/y"; return r }(),
		func() CreateDataSourceRequest { r := validCreate(); r.Password = ""; return r }(),
		func() CreateDataSourceRequest { r := validCreate(); r.Username = "a b"; return r }(),
	}
	for i, req := range bad {
		if _, err := fx.svc.Create(context.Background(), u, fx.orgID, req); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("case %d: Create = %v, want ErrInvalidInput", i, err)
		}
	}
}

func TestTestConnectionActivatesOnSuccess(t *testing.T) {
	fx := newFixture()
	u := user()
	fx.repo.addMember(fx.orgID, u.ID, authorization.RoleAdmin)
	src, _ := fx.svc.Create(context.Background(), u, fx.orgID, validCreate())

	got, err := fx.svc.TestConnection(context.Background(), u, fx.orgID, src.ID)
	if err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if got.Status != StatusActive {
		t.Errorf("status = %q, want active", got.Status)
	}
	// The connector must receive the DECRYPTED password.
	if fx.conn.gotPass != "s3cret" {
		t.Errorf("connector got password %q, want decrypted s3cret", fx.conn.gotPass)
	}
}

func TestTestConnectionFailsClosed(t *testing.T) {
	fx := newFixture()
	u := user()
	fx.repo.addMember(fx.orgID, u.ID, authorization.RoleAdmin)
	src, _ := fx.svc.Create(context.Background(), u, fx.orgID, validCreate())
	fx.conn.pingErr = ErrConnAuth

	_, err := fx.svc.TestConnection(context.Background(), u, fx.orgID, src.ID)
	if !errors.Is(err, ErrConnectionFailed) {
		t.Errorf("TestConnection = %v, want ErrConnectionFailed", err)
	}
	// Status recorded as failed with a sanitized reason.
	stored, _ := fx.repo.Get(context.Background(), fx.orgID, src.ID)
	if stored.Status != StatusFailed {
		t.Errorf("status = %q, want failed", stored.Status)
	}
	if stored.LastError == nil || *stored.LastError == "" {
		t.Error("expected a sanitized last_error")
	}
}

func TestRunQueryRequiresActiveAndRunPermission(t *testing.T) {
	fx := newFixture()
	u := user()
	fx.repo.addMember(fx.orgID, u.ID, authorization.RoleAnalyst) // has run + view, not manage

	// An admin owns the source; the analyst runs queries against it.
	admin := user()
	fx.repo.addMember(fx.orgID, admin.ID, authorization.RoleAdmin)
	src, err0 := fx.svc.Create(context.Background(), admin, fx.orgID, validCreate())
	if err0 != nil {
		t.Fatalf("Create: %v", err0)
	}

	// Pending source → not active.
	_, err := fx.svc.RunQuery(context.Background(), u, fx.orgID, src.ID, QueryRequest{SQL: "SELECT 1"})
	if !errors.Is(err, ErrNotActive) {
		t.Errorf("query on pending = %v, want ErrNotActive", err)
	}

	// Activate, then a valid query flows through the executor.
	_ = fx.repo.UpdateStatus(context.Background(), fx.orgID, src.ID, StatusActive, nil, u.ID, "")
	fx.exec.result = QueryResult{RowCount: 1, Rows: [][]any{{1}}}
	res, err := fx.svc.RunQuery(context.Background(), u, fx.orgID, src.ID, QueryRequest{SQL: "SELECT 1"})
	if err != nil {
		t.Fatalf("RunQuery: %v", err)
	}
	if res.RowCount != 1 {
		t.Errorf("rowCount = %d, want 1", res.RowCount)
	}

	// Invalid SQL is rejected before reaching the executor.
	if _, err := fx.svc.RunQuery(context.Background(), u, fx.orgID, src.ID, QueryRequest{SQL: "DROP TABLE t"}); !errors.Is(err, ErrNotReadOnly) {
		t.Errorf("DROP = %v, want ErrNotReadOnly", err)
	}
}

func TestRunQueryViewerDenied(t *testing.T) {
	fx := newFixture()
	u := user()
	fx.repo.addMember(fx.orgID, u.ID, authorization.RoleViewer) // view only, no run
	src, _ := fx.svc.Create(context.Background(), user(), fx.orgID, validCreate())
	_ = src

	_, err := fx.svc.RunQuery(context.Background(), u, fx.orgID, uuid.New(), QueryRequest{SQL: "SELECT 1"})
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer RunQuery = %v, want ErrForbidden", err)
	}
}

func TestUpdatePasswordReEncryptsAndInvalidates(t *testing.T) {
	fx := newFixture()
	u := user()
	fx.repo.addMember(fx.orgID, u.ID, authorization.RoleAdmin)
	src, _ := fx.svc.Create(context.Background(), u, fx.orgID, validCreate())

	newPass := "rotated"
	_, err := fx.svc.Update(context.Background(), u, fx.orgID, src.ID, UpdateDataSourceRequest{Password: &newPass})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if fx.repo.creds[src.ID] != "enc:rotated" {
		t.Errorf("credential = %q, want re-encrypted enc:rotated", fx.repo.creds[src.ID])
	}
	if len(fx.exec.invalidated) == 0 {
		t.Error("expected the cached pool to be invalidated after a connection change")
	}
}
