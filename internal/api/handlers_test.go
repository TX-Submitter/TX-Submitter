package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gamp/stellar-tx-submitter/internal/state"
)

func TestHealth(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()

	h.Health(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("health status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestReady(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rr := httptest.NewRecorder()

	h.Ready(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("ready status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestHealth_MethodNotAllowed(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	rr := httptest.NewRecorder()

	h.Health(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("health post status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestSubmit_InvalidBody(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader([]byte("not json")))
	rr := httptest.NewRecorder()

	h.Submit(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("submit invalid body status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestSubmit_MethodNotAllowed(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodGet, "/submit", nil)
	rr := httptest.NewRecorder()

	h.Submit(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("submit get status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestSubmit_MissingFields(t *testing.T) {
	h := &Handlers{}
	body := map[string]string{"source": "GABC"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader(jsonBody))
	rr := httptest.NewRecorder()

	h.Submit(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("submit missing fields status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestTransaction_NotFound(t *testing.T) {
	h := &Handlers{store: newTestStore()}
	req := httptest.NewRequest(http.MethodGet, "/transactions/nonexistent", nil)
	rr := httptest.NewRecorder()

	h.Transaction(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("transaction not found status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestTransaction_MethodNotAllowed(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/transactions/abc123", nil)
	rr := httptest.NewRecorder()

	h.Transaction(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("transaction post status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestRouter_Routes(t *testing.T) {
	h := &Handlers{}
	router := Router(h)

	tests := []struct {
		method string
		path   string
		code   int
	}{
		{http.MethodGet, "/health", http.StatusOK},
		{http.MethodGet, "/ready", http.StatusOK},
		{http.MethodPost, "/submit", http.StatusBadRequest}, // missing fields
		{http.MethodGet, "/transactions/", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)
			if rr.Code != tt.code {
				t.Errorf("%s %s = %d, want %d", tt.method, tt.path, rr.Code, tt.code)
			}
		})
	}
}

type testStore struct{}

func newTestStore() *testStore { return &testStore{} }

func (m *testStore) CreateTransaction(ctx context.Context, params state.NewTransactionParams) (*state.Transaction, error) {
	return nil, state.ErrNotFound
}
func (m *testStore) GetTransaction(ctx context.Context, id string) (*state.Transaction, error) {
	return nil, state.ErrNotFound
}
func (m *testStore) GetByExternalID(ctx context.Context, externalID string) (*state.Transaction, error) {
	return nil, state.ErrNotFound
}
func (m *testStore) ListPending(ctx context.Context, limit int) ([]*state.Transaction, error) {
	return nil, nil
}
func (m *testStore) UpdateStatus(ctx context.Context, txID string, newStatus state.Status, detail string, horizonTxID *string) error {
	return nil
}
func (m *testStore) TransitionHistory(ctx context.Context, txID string) ([]*state.Transition, error) {
	return nil, nil
}
func (m *testStore) MarkChannelUsed(ctx context.Context, txID, channelPublicKey string) error {
	return nil
}
func (m *testStore) CreateChannelAccount(ctx context.Context, secretKey string) (*state.ChannelAccount, error) {
	return nil, nil
}
func (m *testStore) ListChannelAccounts(ctx context.Context, activeOnly bool) ([]*state.ChannelAccount, error) {
	return nil, nil
}
func (m *testStore) UpdateChannelAccountSeq(ctx context.Context, publicKey string, seq int64) error {
	return nil
}
func (m *testStore) DeactivateChannelAccount(ctx context.Context, publicKey string) error {
	return nil
}
func (m *testStore) AcquireChannelAccount(ctx context.Context) (*state.ChannelAccount, error) {
	return nil, state.ErrNoChannelAccounts
}
func (m *testStore) ReleaseChannelAccount(ctx context.Context, publicKey string) error {
	return nil
}
func (m *testStore) UnlockAllChannelAccounts(ctx context.Context) error {
	return nil
}
