package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Data-source and analytics metrics. Registered against the default Prometheus
// registry via promauto, so the /metrics handler exposes them automatically.
//
// Labels are deliberately low-cardinality: no org IDs, source IDs, or SQL text
// (which would explode series count and could leak identifiers). "outcome" is a
// small fixed set of sanitized buckets.
var (
	ConnectionTests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "insightiq_datasource_connection_tests_total",
		Help: "Data source connectivity tests by outcome.",
	}, []string{"outcome"}) // success | failure

	SchemaDiscoveries = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "insightiq_datasource_schema_discoveries_total",
		Help: "Schema discovery/refresh runs by outcome.",
	}, []string{"outcome"}) // success | failure

	QueriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "insightiq_datasource_queries_total",
		Help: "Analytics queries by outcome.",
	}, []string{"outcome"}) // success | rejected | failure

	QueryDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "insightiq_datasource_query_duration_seconds",
		Help:    "Wall-clock duration of executed analytics queries.",
		Buckets: prometheus.DefBuckets,
	})
)

// Outcome label values.
const (
	OutcomeSuccess  = "success"
	OutcomeFailure  = "failure"
	OutcomeRejected = "rejected"
)
