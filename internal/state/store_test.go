package state

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
)

// mockStore implements Store for testing without a real DB.
type mockStore struct {
	transactions map[string]*Transaction
	byExternal   map[string]string // externalID -> txID
	transitions  map[string][]*Transition
	accounts     map[string]*ChannelAccount // publicKey -> account
	nextTxID     int
}

func newMockStore() *mockStore {
	return &mockStore{
		transactions: make(map[string]*Transaction),
		byExternal:   make(map[string]string),
		transitions:  make(map[string][]*Transition),
		accounts:     make(map[string]*ChannelAccount),
	}
}

func (m *mockStore) CreateTransaction(ctx context.Context, params NewTransactionParams) (*Transaction, error) {
	if _, exists := m.byExternal[params.ExternalID]; exists {
		return nil, ErrAlreadyExists
	}
	m.nextTxID++
	tx := &Transaction{
		ID:            string(rune(m.nextTxID)),
		ExternalID:    params.ExternalID,
		SourceAccount: params.SourceAccount,
		Destination:   params.Destination,
		Amount:        params.Amount,
		AssetCode:     params.AssetCode,
		AssetIssuer:   params.AssetIssuer,
		Status:        StatusPending,
	}
	m.transactions[tx.ID] = tx
	m.byExternal[params.ExternalID] = tx.ID
	return tx, nil
}

func (m *mockStore) GetTransaction(ctx context.Context, id string) (*Transaction, error) {
	tx, ok := m.transactions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return tx, nil
}

func (m *mockStore) GetByExternalID(ctx context.Context, externalID string) (*Transaction, error) {
	txID, ok := m.byExternal[externalID]
	if !ok {
		return nil, ErrNotFound
	}
	return m.transactions[txID], nil
}

func (m *mockStore) ListPending(ctx context.Context, limit int) ([]*Transaction, error) {
	var result []*Transaction
	for _, tx := range m.transactions {
		if tx.Status == StatusPending {
			result = append(result, tx)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *mockStore) UpdateStatus(ctx context.Context, txID string, newStatus Status, detail string, horizonTxID *string) error {
	tx, ok := m.transactions[txID]
	if !ok {
		return ErrNotFound
	}
	if newStatus.IsTerminal() && tx.Status.IsTerminal() {
		return ErrTerminalState
	}
	oldStatus := tx.Status
	tx.Status = newStatus
	if newStatus == StatusSubmitted && horizonTxID != nil {
		// horizonTxID is stored in the transition detail
	}
	m.transitions[txID] = append(m.transitions[txID], &Transition{
		TransactionID: txID,
		FromStatus:    &oldStatus,
		ToStatus:      newStatus,
		Detail:        &detail,
	})
	return nil
}

func (m *mockStore) TransitionHistory(ctx context.Context, txID string) ([]*Transition, error) {
	return m.transitions[txID], nil
}

func (m *mockStore) MarkChannelUsed(ctx context.Context, txID, channelPublicKey string) error {
	tx, ok := m.transactions[txID]
	if !ok {
		return ErrNotFound
	}
	tx.ChannelAccount = channelPublicKey
	return nil
}

func (m *mockStore) CreateChannelAccount(ctx context.Context, secretKey string) (*ChannelAccount, error) {
	ca := &ChannelAccount{
		ID:         "ca-" + string(rune(m.nextTxID)),
		SecretKey:  secretKey,
		PublicKey:  "GABC" + secretKey,
		CurrentSeq: 0,
		IsActive:   true,
	}
	m.accounts[ca.PublicKey] = ca
	return ca, nil
}

func (m *mockStore) ListChannelAccounts(ctx context.Context, activeOnly bool) ([]*ChannelAccount, error) {
	var result []*ChannelAccount
	for _, ca := range m.accounts {
		if !activeOnly || ca.IsActive {
			result = append(result, ca)
		}
	}
	return result, nil
}

func (m *mockStore) UpdateChannelAccountSeq(ctx context.Context, publicKey string, seq int64) error {
	ca, ok := m.accounts[publicKey]
	if !ok {
		return ErrNotFound
	}
	ca.CurrentSeq = seq
	return nil
}

func (m *mockStore) DeactivateChannelAccount(ctx context.Context, publicKey string) error {
	ca, ok := m.accounts[publicKey]
	if !ok {
		return ErrNotFound
	}
	ca.IsActive = false
	return nil
}

func TestStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status   Status
		terminal bool
	}{
		{StatusPending, false},
		{StatusSubmitted, false},
		{StatusConfirmed, true},
		{StatusFailed, true},
		{StatusDeadLetter, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			got := tt.status.IsTerminal()
			if got != tt.terminal {
				t.Errorf("IsTerminal(%q) = %v, want %v", tt.status, got, tt.terminal)
			}
		})
	}
}

func TestCreateTransaction(t *testing.T) {
	ctx := context.Background()
	s := newMockStore()

	amount, _ := decimal.NewFromString("10000000")
	tx, err := s.CreateTransaction(ctx, NewTransactionParams{
		ExternalID:      "tx-001",
		SourceAccount:   "GSRC",
		Destination:     "GDEST",
		Amount:          amount,
		AssetCode:       "USD",
		AssetIssuer:     "GISS",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.Status != StatusPending {
		t.Errorf("expected status pending, got %s", tx.Status)
	}
	if tx.ExternalID != "tx-001" {
		t.Errorf("expected external ID tx-001, got %s", tx.ExternalID)
	}
}

func TestCreateTransaction_DuplicateExternalID(t *testing.T) {
	ctx := context.Background()
	s := newMockStore()

	amount, _ := decimal.NewFromString("1000")
	_, err := s.CreateTransaction(ctx, NewTransactionParams{ExternalID: "dup", SourceAccount: "A", Destination: "B", Amount: amount})
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	_, err = s.CreateTransaction(ctx, NewTransactionParams{ExternalID: "dup", SourceAccount: "A", Destination: "B", Amount: amount})
	if err != ErrAlreadyExists {
		t.Errorf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestGetTransaction_NotFound(t *testing.T) {
	ctx := context.Background()
	s := newMockStore()

	_, err := s.GetTransaction(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestGetByExternalID(t *testing.T) {
	ctx := context.Background()
	s := newMockStore()

	amount, _ := decimal.NewFromString("5000")
	_, err := s.CreateTransaction(ctx, NewTransactionParams{ExternalID: "ext-1", SourceAccount: "A", Destination: "B", Amount: amount})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	tx, err := s.GetByExternalID(ctx, "ext-1")
	if err != nil {
		t.Fatalf("get by external ID failed: %v", err)
	}
	if tx.Status != StatusPending {
		t.Errorf("expected pending, got %s", tx.Status)
	}
}

func TestUpdateStatus_Transitions(t *testing.T) {
	ctx := context.Background()
	s := newMockStore()

	amount, _ := decimal.NewFromString("1000")
	tx, _ := s.CreateTransaction(ctx, NewTransactionParams{ExternalID: "tx-1", SourceAccount: "A", Destination: "B", Amount: amount})

	// pending -> submitted
	err := s.UpdateStatus(ctx, tx.ID, StatusSubmitted, "submitted to horizon", nil)
	if err != nil {
		t.Fatalf("update to submitted failed: %v", err)
	}
	if tx.Status != StatusSubmitted {
		t.Errorf("expected submitted, got %s", tx.Status)
	}

	// submitted -> confirmed
	var horizonID string = "txhash123"
	err = s.UpdateStatus(ctx, tx.ID, StatusConfirmed, "confirmed on chain", &horizonID)
	if err != nil {
		t.Fatalf("update to confirmed failed: %v", err)
	}
	if tx.Status != StatusConfirmed {
		t.Errorf("expected confirmed, got %s", tx.Status)
	}
}

func TestUpdateStatus_TerminalToTerminal(t *testing.T) {
	ctx := context.Background()
	s := newMockStore()

	amount, _ := decimal.NewFromString("1000")
	tx, _ := s.CreateTransaction(ctx, NewTransactionParams{ExternalID: "tx-1", SourceAccount: "A", Destination: "B", Amount: amount})

	// Go to terminal state
	_ = s.UpdateStatus(ctx, tx.ID, StatusConfirmed, "confirmed", nil)

	// Try to transition from confirmed to another terminal state
	err := s.UpdateStatus(ctx, tx.ID, StatusFailed, "should fail", nil)
	if err != ErrTerminalState {
		t.Errorf("expected ErrTerminalState, got %v", err)
	}
}

func TestTransitionHistory(t *testing.T) {
	ctx := context.Background()
	s := newMockStore()

	amount, _ := decimal.NewFromString("1000")
	tx, _ := s.CreateTransaction(ctx, NewTransactionParams{ExternalID: "tx-1", SourceAccount: "A", Destination: "B", Amount: amount})

	_ = s.UpdateStatus(ctx, tx.ID, StatusSubmitted, "submitted", nil)
	_ = s.UpdateStatus(ctx, tx.ID, StatusConfirmed, "confirmed", nil)

	history, err := s.TransitionHistory(ctx, tx.ID)
	if err != nil {
		t.Fatalf("history query failed: %v", err)
	}
	if len(history) != 2 {
		t.Errorf("expected 2 transitions, got %d", len(history))
	}
	if history[0].ToStatus != StatusSubmitted {
		t.Errorf("expected first transition to submitted, got %s", history[0].ToStatus)
	}
	if history[1].ToStatus != StatusConfirmed {
		t.Errorf("expected second transition to confirmed, got %s", history[1].ToStatus)
	}
}

func TestListPending(t *testing.T) {
	ctx := context.Background()
	s := newMockStore()

	amount, _ := decimal.NewFromString("1000")
	_, _ = s.CreateTransaction(ctx, NewTransactionParams{ExternalID: "p1", SourceAccount: "A", Destination: "B", Amount: amount})
	_, _ = s.CreateTransaction(ctx, NewTransactionParams{ExternalID: "p2", SourceAccount: "A", Destination: "B", Amount: amount})
	_, _ = s.CreateTransaction(ctx, NewTransactionParams{ExternalID: "p3", SourceAccount: "A", Destination: "B", Amount: amount})

	pending, err := s.ListPending(ctx, 2)
	if err != nil {
		t.Fatalf("list pending failed: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending, got %d", len(pending))
	}
}

func TestMarkChannelUsed(t *testing.T) {
	ctx := context.Background()
	s := newMockStore()

	amount, _ := decimal.NewFromString("1000")
	tx, _ := s.CreateTransaction(ctx, NewTransactionParams{ExternalID: "tx-1", SourceAccount: "A", Destination: "B", Amount: amount})

	err := s.MarkChannelUsed(ctx, tx.ID, "GCHANNEL123")
	if err != nil {
		t.Fatalf("mark channel used failed: %v", err)
	}
	if tx.ChannelAccount != "GCHANNEL123" {
		t.Errorf("expected channel account GCHANNEL123, got %s", tx.ChannelAccount)
	}
}

func TestChannelAccountOps(t *testing.T) {
	ctx := context.Background()
	s := newMockStore()

	ca, err := s.CreateChannelAccount(ctx, "s3cr3t")
	if err != nil {
		t.Fatalf("create channel account failed: %v", err)
	}
	if !ca.IsActive {
		t.Error("expected channel account to be active")
	}

	err = s.UpdateChannelAccountSeq(ctx, ca.PublicKey, 42)
	if err != nil {
		t.Fatalf("update seq failed: %v", err)
	}
	if ca.CurrentSeq != 42 {
		t.Errorf("expected seq 42, got %d", ca.CurrentSeq)
	}

	err = s.DeactivateChannelAccount(ctx, ca.PublicKey)
	if err != nil {
		t.Fatalf("deactivate failed: %v", err)
	}
	if ca.IsActive {
		t.Error("expected channel account to be inactive")
	}

	all, _ := s.ListChannelAccounts(ctx, false)
	if len(all) != 1 {
		t.Errorf("expected 1 account (all), got %d", len(all))
	}

	active, _ := s.ListChannelAccounts(ctx, true)
	if len(active) != 0 {
		t.Errorf("expected 0 active accounts, got %d", len(active))
	}
}
