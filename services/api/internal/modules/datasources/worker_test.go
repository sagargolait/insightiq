package datasources

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeJobs implements JobRepository with an in-memory queue of pending jobs for
// a single tenant, recording completions and failures.
type fakeJobs struct {
	mu        sync.Mutex
	tenant    uuid.UUID
	pending   []Job
	completed []uuid.UUID
	failed    []failRecord
}

type failRecord struct {
	jobID   uuid.UUID
	reason  string
	backoff time.Duration
}

func (f *fakeJobs) ActiveTenants(_ context.Context) ([]uuid.UUID, error) {
	return []uuid.UUID{f.tenant}, nil
}

func (f *fakeJobs) ClaimJob(_ context.Context, orgID uuid.UUID) (Job, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if orgID != f.tenant || len(f.pending) == 0 {
		return Job{}, false, nil
	}
	job := f.pending[0]
	f.pending = f.pending[1:]
	job.Attempts++ // ClaimJob increments attempts in the real repo
	return job, true, nil
}

func (f *fakeJobs) CompleteJob(_ context.Context, _, jobID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed = append(f.completed, jobID)
	return nil
}

func (f *fakeJobs) FailJob(_ context.Context, _, jobID uuid.UUID, reason string, backoff time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failed = append(f.failed, failRecord{jobID, reason, backoff})
	return nil
}

// fakeDiscoverer records calls and returns a scripted outcome.
type fakeDiscoverer struct {
	mu    sync.Mutex
	calls []uuid.UUID
	cols  int
	err   error
}

func (d *fakeDiscoverer) DiscoverForJob(_ context.Context, _, sourceID uuid.UUID) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, sourceID)
	return d.cols, d.err
}

func newWorkerFixture(jobs *fakeJobs, disc *fakeDiscoverer) *Worker {
	// Fast poll; the tests drive drain() directly so the interval barely matters.
	return NewWorker(jobs, disc, nil, WithPollInterval(time.Millisecond), WithBaseBackoff(time.Second))
}

func TestWorkerProcessesJobToCompletion(t *testing.T) {
	org := uuid.New()
	job := Job{ID: uuid.New(), OrganizationID: org, DataSourceID: uuid.New(), Kind: JobSchemaDiscovery, MaxAttempts: 3}
	jobs := &fakeJobs{tenant: org, pending: []Job{job}}
	disc := &fakeDiscoverer{cols: 5}
	w := newWorkerFixture(jobs, disc)

	w.drain(context.Background())

	if len(disc.calls) != 1 || disc.calls[0] != job.DataSourceID {
		t.Fatalf("discovery not run for the job: %v", disc.calls)
	}
	if len(jobs.completed) != 1 || jobs.completed[0] != job.ID {
		t.Fatalf("job not completed: %v", jobs.completed)
	}
	if len(jobs.failed) != 0 {
		t.Fatalf("unexpected failures: %v", jobs.failed)
	}
}

func TestWorkerRetriesWithBackoff(t *testing.T) {
	org := uuid.New()
	// First attempt (Attempts becomes 1 on claim), below MaxAttempts → retry.
	job := Job{ID: uuid.New(), OrganizationID: org, DataSourceID: uuid.New(), Kind: JobSchemaDiscovery, Attempts: 0, MaxAttempts: 3}
	jobs := &fakeJobs{tenant: org, pending: []Job{job}}
	disc := &fakeDiscoverer{err: ErrConnectionFailed}
	w := newWorkerFixture(jobs, disc)

	w.drain(context.Background())

	if len(jobs.completed) != 0 {
		t.Fatalf("failed job should not complete: %v", jobs.completed)
	}
	if len(jobs.failed) != 1 {
		t.Fatalf("expected one failure record, got %v", jobs.failed)
	}
	rec := jobs.failed[0]
	if rec.jobID != job.ID {
		t.Errorf("failed wrong job: %v", rec.jobID)
	}
	// Attempt 1 → backoff = base << 1 = 2s.
	if rec.backoff != 2*time.Second {
		t.Errorf("backoff = %v, want 2s", rec.backoff)
	}
	// The sanitized sentinel text, never host/credentials.
	if rec.reason == "" {
		t.Error("expected a failure reason")
	}
}

func TestWorkerUnknownKindFailsTerminally(t *testing.T) {
	org := uuid.New()
	job := Job{ID: uuid.New(), OrganizationID: org, DataSourceID: uuid.New(), Kind: "bogus", MaxAttempts: 3}
	jobs := &fakeJobs{tenant: org, pending: []Job{job}}
	disc := &fakeDiscoverer{}
	w := newWorkerFixture(jobs, disc)

	w.drain(context.Background())

	if len(disc.calls) != 0 {
		t.Errorf("discovery should not run for an unknown kind: %v", disc.calls)
	}
	if len(jobs.failed) != 1 || jobs.failed[0].backoff != 0 {
		t.Errorf("unknown kind should fail with zero backoff: %v", jobs.failed)
	}
}

func TestWorkerDrainsBacklogInOnePass(t *testing.T) {
	org := uuid.New()
	var pending []Job
	for i := 0; i < 5; i++ {
		pending = append(pending, Job{ID: uuid.New(), OrganizationID: org, DataSourceID: uuid.New(), Kind: JobSchemaDiscovery, MaxAttempts: 3})
	}
	jobs := &fakeJobs{tenant: org, pending: pending}
	disc := &fakeDiscoverer{cols: 1}
	w := newWorkerFixture(jobs, disc)

	w.drain(context.Background())

	if len(jobs.completed) != 5 {
		t.Fatalf("expected all 5 jobs completed in one pass, got %d", len(jobs.completed))
	}
}

func TestWorkerRunStopsOnContextCancel(t *testing.T) {
	org := uuid.New()
	jobs := &fakeJobs{tenant: org} // nothing pending
	disc := &fakeDiscoverer{}
	w := newWorkerFixture(jobs, disc)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop after context cancel")
	}
}

// Guard: ensure the real repository satisfies JobRepository.
var _ JobRepository = (*PostgresRepository)(nil)

// Guard: ensure the service satisfies the worker's discoverer view.
var _ discoverer = (*Service)(nil)

func TestBackoffCapDoesNotOverflow(t *testing.T) {
	// A high attempt count must not shift past the cap (minInt(attempts,6)).
	org := uuid.New()
	job := Job{ID: uuid.New(), OrganizationID: org, DataSourceID: uuid.New(), Kind: JobSchemaDiscovery, Attempts: 99, MaxAttempts: 100}
	jobs := &fakeJobs{tenant: org, pending: []Job{job}}
	disc := &fakeDiscoverer{err: errors.New("boom")}
	w := newWorkerFixture(jobs, disc)

	w.drain(context.Background())

	if len(jobs.failed) != 1 {
		t.Fatalf("expected one failure, got %v", jobs.failed)
	}
	// base(1s) << 6 = 64s, the capped maximum.
	if jobs.failed[0].backoff != 64*time.Second {
		t.Errorf("backoff = %v, want capped 64s", jobs.failed[0].backoff)
	}
}
