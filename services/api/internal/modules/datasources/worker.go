package datasources

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/sagargolait/insightiq/services/api/internal/platform/observability"
)

// discoverer is the worker's view of the service: just the out-of-band
// discovery entrypoint.
type discoverer interface {
	DiscoverForJob(ctx context.Context, orgID, sourceID uuid.UUID) (int, error)
}

// Worker drains the data_source_jobs queue out of band so long-running schema
// discovery never blocks an HTTP handler. It is a single in-process goroutine
// polling the DB queue; jobs are claimed with FOR UPDATE SKIP LOCKED, so running
// more than one instance is safe.
type Worker struct {
	jobs    JobRepository
	svc     discoverer
	logger  *slog.Logger
	poll    time.Duration
	baseOff time.Duration // base backoff for a failed attempt
}

// WorkerOption tunes a Worker.
type WorkerOption func(*Worker)

// WithPollInterval sets how often the worker scans for due jobs.
func WithPollInterval(d time.Duration) WorkerOption {
	return func(w *Worker) { w.poll = d }
}

// WithBaseBackoff sets the base retry backoff (doubled per attempt).
func WithBaseBackoff(d time.Duration) WorkerOption {
	return func(w *Worker) { w.baseOff = d }
}

func NewWorker(jobs JobRepository, svc discoverer, logger *slog.Logger, opts ...WorkerOption) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	w := &Worker{
		jobs:    jobs,
		svc:     svc,
		logger:  logger.With(slog.String("component", "datasource_worker")),
		poll:    15 * time.Second,
		baseOff: 30 * time.Second,
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

// Run blocks, draining the queue until ctx is cancelled. Intended to be started
// as `go worker.Run(ctx)`; it returns when ctx is done.
func (w *Worker) Run(ctx context.Context) {
	w.logger.Info("worker started", slog.Duration("poll_interval", w.poll))
	ticker := time.NewTicker(w.poll)
	defer ticker.Stop()

	// Drain once immediately so a job enqueued just before start isn't delayed a
	// full interval.
	w.drain(ctx)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("worker stopped")
			return
		case <-ticker.C:
			w.drain(ctx)
		}
	}
}

// DrainOnce runs a single drain pass synchronously and returns when the queue
// has no more due jobs. Useful for tests and for triggering an immediate drain.
func (w *Worker) DrainOnce(ctx context.Context) { w.drain(ctx) }

// drain claims and processes every currently-due job across tenants, one claim
// at a time per tenant per pass. A pass ends when no tenant yields a job.
func (w *Worker) drain(ctx context.Context) {
	tenants, err := w.jobs.ActiveTenants(ctx)
	if err != nil {
		w.logger.Warn("list tenants failed", slog.String("error", err.Error()))
		return
	}

	for _, orgID := range tenants {
		if ctx.Err() != nil {
			return
		}
		// Keep claiming for this tenant until it has nothing due, so a backlog is
		// cleared in one pass rather than one-per-interval.
		for {
			if ctx.Err() != nil {
				return
			}
			job, found, err := w.jobs.ClaimJob(ctx, orgID)
			if err != nil {
				w.logger.Warn("claim job failed",
					slog.String("org_id", orgID.String()),
					slog.String("error", err.Error()))
				break
			}
			if !found {
				break
			}
			w.process(ctx, job)
		}
	}
}

// process runs one claimed job and records its outcome. Failures are retried
// with exponential backoff up to the job's max_attempts.
func (w *Worker) process(ctx context.Context, job Job) {
	log := w.logger.With(
		slog.String("job_id", job.ID.String()),
		slog.String("org_id", job.OrganizationID.String()),
		slog.String("source_id", job.DataSourceID.String()),
		slog.String("kind", job.Kind),
		slog.Int("attempt", job.Attempts),
	)

	switch job.Kind {
	case JobSchemaDiscovery:
		cols, err := w.svc.DiscoverForJob(ctx, job.OrganizationID, job.DataSourceID)
		if err != nil {
			w.handleFailure(ctx, job, err, log)
			return
		}
		if err := w.jobs.CompleteJob(ctx, job.OrganizationID, job.ID); err != nil {
			log.Warn("complete job failed", slog.String("error", err.Error()))
			return
		}
		log.Info("job succeeded", slog.Int("columns", cols))
	default:
		// Unknown kind: fail terminally so it doesn't spin. reason is static.
		_ = w.jobs.FailJob(ctx, job.OrganizationID, job.ID, "unknown job kind", 0)
		log.Warn("unknown job kind; failed")
	}
}

func (w *Worker) handleFailure(ctx context.Context, job Job, cause error, log *slog.Logger) {
	// reason is a sanitized, non-sensitive string (service errors are already
	// bucketed; never the host or credentials).
	reason := cause.Error()
	backoff := w.baseOff * (1 << uint(minInt(job.Attempts, 6))) // cap the shift
	if err := w.jobs.FailJob(ctx, job.OrganizationID, job.ID, reason, backoff); err != nil {
		log.Warn("record failure failed", slog.String("error", err.Error()))
		return
	}
	observability.SchemaDiscoveries.WithLabelValues(observability.OutcomeFailure).Inc()
	terminal := job.Attempts >= job.MaxAttempts
	log.Warn("job attempt failed",
		slog.String("reason", reason),
		slog.Bool("terminal", terminal),
		slog.Duration("backoff", backoff))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
