package channelaccount

import (
	"context"
	"testing"

	"github.com/gamp/stellar-tx-submitter/internal/state"
)

// testMockStore is a minimal mock implementing state.Store for pool tests.
type testMockStore struct {
	accounts  map[string]*state.ChannelAccount
	seqCount  int
}

func newTestMockStore() *testMockStore {
	return &testMockStore{
		accounts: make(map[string]*state.ChannelAccount),
	}
}

func (m *testMockStore) CreateTransaction(ctx context.Context, params state.NewTransactionParams) (*state.Transaction, error) {
	return nil, state.ErrNotFound
}
func (m *testMockStore) GetTransaction(ctx context.Context, id string) (*state.Transaction, error) {
	return nil, state.ErrNotFound
}
func (m *testMockStore) GetByExternalID(ctx context.Context, externalID string) (*state.Transaction, error) {
	return nil, state.ErrNotFound
}
func (m *testMockStore) ListPending(ctx context.Context, limit int) ([]*state.Transaction, error) {
	return nil, nil
}
func (m *testMockStore) UpdateStatus(ctx context.Context, txID string, newStatus state.Status, detail string, horizonTxID *string) error {
	return nil
}
func (m *testMockStore) TransitionHistory(ctx context.Context, txID string) ([]*state.Transition, error) {
	return nil, nil
}
func (m *testMockStore) MarkChannelUsed(ctx context.Context, txID, channelPublicKey string) error {
	return nil
}
func (m *testMockStore) CreateChannelAccount(ctx context.Context, secretKey string) (*state.ChannelAccount, error) {
	ca := &state.ChannelAccount{
		PublicKey: "G" + secretKey,
		SecretKey: secretKey,
		IsActive:  true,
	}
	m.accounts[ca.PublicKey] = ca
	return ca, nil
}
func (m *testMockStore) ListChannelAccounts(ctx context.Context, activeOnly bool) ([]*state.ChannelAccount, error) {
	result := make([]*state.ChannelAccount, 0)
	for _, ca := range m.accounts {
		if !activeOnly || ca.IsActive {
			result = append(result, ca)
		}
	}
	return result, nil
}
func (m *testMockStore) UpdateChannelAccountSeq(ctx context.Context, publicKey string, seq int64) error {
	ca, ok := m.accounts[publicKey]
	if !ok {
		return state.ErrNotFound
	}
	ca.CurrentSeq = seq
	return nil
}
func (m *testMockStore) DeactivateChannelAccount(ctx context.Context, publicKey string) error {
	ca, ok := m.accounts[publicKey]
	if !ok {
		return state.ErrNotFound
	}
	ca.IsActive = false
	return nil
}
func (m *testMockStore) AcquireChannelAccount(ctx context.Context) (*state.ChannelAccount, error) {
	var best *state.ChannelAccount
	for _, ca := range m.accounts {
		if ca.IsActive && (best == nil || ca.CurrentSeq < best.CurrentSeq) {
			best = ca
		}
	}
	if best == nil {
		return nil, state.ErrNoChannelAccounts
	}
	best.CurrentSeq++
	return best, nil
}

func TestPool_Acquire_ReturnsAccount(t *testing.T) {
	ctx := context.Background()
	ms := newTestMockStore()
	ms.accounts["GACC1"] = &state.ChannelAccount{PublicKey: "GACC1", CurrentSeq: 10, IsActive: true}
	ms.accounts["GACC2"] = &state.ChannelAccount{PublicKey: "GACC2", CurrentSeq: 15, IsActive: true}

	pool := New(ms)
	ca, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}
	if ca.PublicKey != "GACC1" {
		t.Errorf("expected GACC1 (lowest seq), got %s", ca.PublicKey)
	}
	if ca.CurrentSeq != 11 {
		t.Errorf("expected seq 11 after acquire, got %d", ca.CurrentSeq)
	}
}

func TestPool_Acquire_NoAccounts(t *testing.T) {
	ctx := context.Background()
	ms := newTestMockStore()
	pool := New(ms)

	_, err := pool.Acquire(ctx)
	if err == nil {
		t.Fatal("expected error when no accounts available")
	}
}

func TestPool_Acquire_InactiveSkipped(t *testing.T) {
	ctx := context.Background()
	ms := newTestMockStore()
	ms.accounts["GACC1"] = &state.ChannelAccount{PublicKey: "GACC1", CurrentSeq: 10, IsActive: false}

	pool := New(ms)
	_, err := pool.Acquire(ctx)
	if err == nil {
		t.Fatal("expected error when only inactive accounts")
	}
}

func TestPool_Release(t *testing.T) {
	ctx := context.Background()
	ms := newTestMockStore()
	ms.accounts["GACC1"] = &state.ChannelAccount{PublicKey: "GACC1", CurrentSeq: 11, IsActive: true}

	pool := New(ms)
	err := pool.Release(ctx, "GACC1")
	if err != nil {
		t.Fatalf("release failed: %v", err)
	}
	if ms.accounts["GACC1"].CurrentSeq != 10 {
		t.Errorf("expected seq 10 after release, got %d", ms.accounts["GACC1"].CurrentSeq)
	}
}

func TestPool_Release_NotFound(t *testing.T) {
	ctx := context.Background()
	ms := newTestMockStore()
	pool := New(ms)

	err := pool.Release(ctx, "GUNKNOWN")
	if err == nil {
		t.Fatal("expected error for unknown account")
	}
}

func TestPool_PoolSize(t *testing.T) {
	ctx := context.Background()
	ms := newTestMockStore()
	ms.accounts["GACC1"] = &state.ChannelAccount{PublicKey: "GACC1", IsActive: true}
	ms.accounts["GACC2"] = &state.ChannelAccount{PublicKey: "GACC2", IsActive: true}
	ms.accounts["GACC3"] = &state.ChannelAccount{PublicKey: "GACC3", IsActive: false}

	pool := New(ms)
	size, err := pool.PoolSize(ctx)
	if err != nil {
		t.Fatalf("pool size failed: %v", err)
	}
	if size != 2 {
		t.Errorf("expected pool size 2, got %d", size)
	}
}

func TestPool_RemoveAccount(t *testing.T) {
	ctx := context.Background()
	ms := newTestMockStore()
	ms.accounts["GACC1"] = &state.ChannelAccount{PublicKey: "GACC1", IsActive: true}

	pool := New(ms)
	err := pool.RemoveAccount(ctx, "GACC1")
	if err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if ms.accounts["GACC1"].IsActive {
		t.Error("expected account to be inactive after removal")
	}
}

func TestPool_AddAccount(t *testing.T) {
	ctx := context.Background()
	ms := newTestMockStore()

	pool := New(ms)
	ca, err := pool.AddAccount(ctx, "s3cr3t")
	if err != nil {
		t.Fatalf("add account failed: %v", err)
	}
	if ca.SecretKey != "s3cr3t" {
		t.Errorf("expected secret key s3cr3t, got %s", ca.SecretKey)
	}
}

func TestPool_ConcurrentAcquire(t *testing.T) {
	ctx := context.Background()
	ms := newTestMockStore()
	ms.accounts["GACC1"] = &state.ChannelAccount{PublicKey: "GACC1", CurrentSeq: 10, IsActive: true}
	ms.accounts["GACC2"] = &state.ChannelAccount{PublicKey: "GACC2", CurrentSeq: 10, IsActive: true}

	pool := New(ms)

	// Acquire 2 accounts — should both succeed
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := pool.Acquire(ctx)
			results <- err
		}()
	}

	successes := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successes++
		}
	}

	if successes != 2 {
		t.Errorf("expected 2 successes, got %d", successes)
	}
}
