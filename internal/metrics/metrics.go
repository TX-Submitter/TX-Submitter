// Package metrics provides Prometheus collectors for the Stellar TX Submitter.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// SubmissionCount tracks the total number of submission attempts per status.
	SubmissionCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "stellar",
			Subsystem: "submitter",
			Name:      "transactions_total",
			Help:      "Total number of transaction submissions by status",
		},
		[]string{"status"},
	)

	// SubmitLatency tracks the time from submission to confirmation.
	SubmitLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "stellar",
			Subsystem: "submitter",
			Name:      "submit_latency_seconds",
			Help:      "Time from submission to confirmation in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 10),
		},
		[]string{"status"},
	)

	// DeadLetterCount tracks the total number of transactions sent to dead letter.
	DeadLetterCount = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: "stellar",
			Subsystem: "submitter",
			Name:      "dead_letters_total",
			Help:      "Total number of transactions sent to dead letter after retry exhaustion",
		},
	)

	// ActiveChannelAccounts tracks the number of active channel accounts in the pool.
	ActiveChannelAccounts = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "stellar",
			Subsystem: "pool",
			Name:      "active_accounts",
			Help:      "Number of active channel accounts in the pool",
		},
	)

	// PendingTransactions tracks the number of pending payments awaiting submission.
	PendingTransactions = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "stellar",
			Subsystem: "submitter",
			Name:      "pending_transactions",
			Help:      "Number of pending transactions awaiting submission",
		},
	)
)

// IncrementSubmission increments the submission counter for the given status.
func IncrementSubmission(status string) {
	SubmissionCount.WithLabelValues(status).Inc()
}

// ObserveSubmitLatency records a submission latency observation.
func ObserveSubmitLatency(seconds float64) {
	SubmitLatency.WithLabelValues("success").Observe(seconds)
}

// IncrementDeadLetters increments the dead letter counter.
func IncrementDeadLetters() {
	DeadLetterCount.Inc()
}

// SetActiveChannels sets the active channel account count.
func SetActiveChannels(count float64) {
	ActiveChannelAccounts.Set(count)
}

// SetPending sets the pending transaction count.
func SetPending(count float64) {
	PendingTransactions.Set(count)
}
