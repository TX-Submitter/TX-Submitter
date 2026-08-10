package retry

import (
	"errors"
	"testing"
	"time"
)

func TestNewPolicy(t *testing.T) {
	p := NewPolicy(5, 100, 10000, 2.0, 5*time.Second, "testnet")
	if p.maxAttempts != 5 {
		t.Errorf("expected maxAttempts 5, got %d", p.maxAttempts)
	}
	if p.baseFee != 100 {
		t.Errorf("expected baseFee 100, got %d", p.baseFee)
	}
	if p.maxFee != 10000 {
		t.Errorf("expected maxFee 10000, got %d", p.maxFee)
	}
}

func TestCurrentFee(t *testing.T) {
	p := NewPolicy(5, 100, 10000, 2.0, 5*time.Second, "testnet")

	tests := []struct {
		attempt int
		want    int64
	}{
		{0, 100},
		{1, 100},
		{2, 200},
		{3, 400},
		{4, 800},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.attempt)), func(t *testing.T) {
			got := p.CurrentFee(tt.attempt)
			if got != tt.want {
				t.Errorf("CurrentFee(%d) = %d, want %d", tt.attempt, got, tt.want)
			}
		})
	}
}

func TestCurrentFee_Capped(t *testing.T) {
	p := NewPolicy(10, 100, 500, 2.0, 5*time.Second, "testnet")

	// Without cap: 100, 100, 200, 400, 800...
	// With cap at 500: 100, 100, 200, 400, 500, 500...
	tests := []struct {
		attempt int
		want    int64
	}{
		{0, 100},
		{1, 100},
		{2, 200},
		{3, 400},
		{4, 500}, // capped
		{5, 500}, // capped
		{9, 500}, // still capped
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.attempt)), func(t *testing.T) {
			got := p.CurrentFee(tt.attempt)
			if got != tt.want {
				t.Errorf("CurrentFee(%d) = %d, want %d", tt.attempt, got, tt.want)
			}
		})
	}
}

func TestBackoff(t *testing.T) {
	p := NewPolicy(5, 100, 10000, 2.0, 5*time.Second, "testnet")

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 0},
		{1, 5 * time.Second},
		{2, 10 * time.Second},
		{3, 20 * time.Second},
		{4, 40 * time.Second},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.attempt)), func(t *testing.T) {
			got := p.Backoff(tt.attempt)
			if got != tt.want {
				t.Errorf("Backoff(%d) = %v, want %v", tt.attempt, got, tt.want)
			}
		})
	}
}

func TestEscalationCurve(t *testing.T) {
	p := NewPolicy(5, 100, 10000, 2.0, 5*time.Second, "testnet")
	curve := p.EscalationCurve()

	expected := []int64{100, 100, 200, 400, 800}
	for i, want := range expected {
		if curve[i] != want {
			t.Errorf("curve[%d] = %d, want %d", i, curve[i], want)
		}
	}
}

func TestString(t *testing.T) {
	p := NewPolicy(3, 100, 1000, 2.0, 5*time.Second, "testnet")
	s := p.String()
	if s == "" {
		t.Error("expected non-empty string")
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "nil error",
			err:       nil,
			retryable: false,
		},
		{
			name:      "terminal tx_bad_auth",
			err:       errors.New("HTTP 400: {\"error\":\"tx_bad_auth\"}"),
			retryable: false,
		},
		{
			name:      "terminal tx_bad_msg",
			err:       errors.New("HTTP 400: {\"error\":\"tx_bad_msg\"}"),
			retryable: false,
		},
		{
			name:      "retryable network timeout",
			err:       errors.New("request timeout"),
			retryable: true,
		},
		{
			name:      "retryable connection refused",
			err:       errors.New("connection refused"),
			retryable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsRetryable(tt.err)
			if got != tt.retryable {
				t.Errorf("IsRetryable(%v) = %v, want %v", tt.err, got, tt.retryable)
			}
		})
	}
}
