// Package retry provides configurable backoff scheduling and fee-bump escalation
// for stuck Stellar transactions. It caps fees at a configurable maximum.
package retry

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Policy defines the retry schedule and fee escalation curve.
type Policy struct {
	maxAttempts    int
	baseFee        int64
	maxFee         int64
	multiplier     float64
	backoffBase    time.Duration
	horizonURL     string
}

// NewPolicy creates a new retry policy with the given parameters.
func NewPolicy(maxAttempts int, baseFee, maxFee int64, multiplier float64, backoffBase time.Duration, horizonURL string) *Policy {
	return &Policy{
		maxAttempts: maxAttempts,
		baseFee:     baseFee,
		maxFee:      maxFee,
		multiplier:  multiplier,
		backoffBase: backoffBase,
		horizonURL:  horizonURL,
	}
}

// MaxAttempts returns the configured maximum retry attempts.
func (p *Policy) MaxAttempts() int {
	return p.maxAttempts
}

// CurrentFee calculates the fee for the given attempt number using the
// multiplier schedule, capped at maxFee.
func (p *Policy) CurrentFee(attempt int) int64 {
	if attempt <= 0 {
		return p.baseFee
	}
	fee := float64(p.baseFee) * math.Pow(p.multiplier, float64(attempt-1))
	if fee > float64(p.maxFee) {
		return p.maxFee
	}
	return int64(fee)
}

// Backoff returns the duration to wait before the next retry attempt.
func (p *Policy) Backoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	d := float64(p.backoffBase) * math.Pow(2, float64(attempt-1))
	return time.Duration(d)
}

// ShouldRetry determines if an error is retryable and the attempt count
// hasn't been exceeded.
func (p *Policy) ShouldRetry(err error, attempt int) bool {
	if attempt >= p.maxAttempts {
		return false
	}
	return IsRetryable(err)
}

// IsRetryable classifies an error as retryable or terminal.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	terminalCodes := []string{
		"tx_bad_auth",
		"tx_bad_msg",
		"tx_too_old",
		"tx_fee_bump_within_max_fee",
	}

	for _, code := range terminalCodes {
		if strings.Contains(errStr, code) {
			return false
		}
	}

	return true
}

// EscalationCurve returns the fee schedule for all attempts.
func (p *Policy) EscalationCurve() []int64 {
	curve := make([]int64, p.maxAttempts)
	for i := 0; i < p.maxAttempts; i++ {
		curve[i] = p.CurrentFee(i)
	}
	return curve
}

// String returns a human-readable description of the policy.
func (p *Policy) String() string {
	curve := p.EscalationCurve()
	return fmt.Sprintf("Policy{maxAttempts:%d, baseFee:%d, maxFee:%d, curve:%v}",
		p.maxAttempts, p.baseFee, p.maxFee, curve)
}
