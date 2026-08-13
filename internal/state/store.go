// Package state provides a Postgres-backed persistent state store for tracking
// transaction lifecycle and channel account pooling.
package state

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stellar/go-stellar-sdk/keypair"
)

var (
	ErrNotFound          = errors.New("transaction not found")
	ErrAlreadyExists     = errors.New("transaction external id already exists")
	ErrTerminalState     = errors.New("transaction is in a terminal state")
	ErrNoChannelAccounts = errors.New("no available channel accounts in pool")
)

// Status represents the lifecycle state of a transaction.
type Status string

const (
	StatusPending    Status = "pending"
	StatusSubmitted  Status = "submitted"
	StatusConfirmed  Status = "confirmed"
	StatusFailed     Status = "failed"
	StatusDeadLetter Status = "dead_letter"
)

// IsTerminal returns true if the status represents a terminal state.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusConfirmed, StatusFailed, StatusDeadLetter:
		return true
	default:
		return false
	}
}

// Transaction represents a recorded payment submission.
type Transaction struct {
	ID            string
	ExternalID    string
	SourceAccount string
	Destination   string
	Amount        decimal.Decimal
	AssetCode     string
	AssetIssuer   string
	ChannelAccount string
	Status        Status
	HorizonTxID   *string
	ErrorMessage  *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Transition records a state change for a transaction.
type Transition struct {
	ID            int64
	TransactionID string
	FromStatus    *Status
	ToStatus      Status
	Detail        *string
	AttemptedAt   time.Time
}

// ChannelAccount represents a channel account row in the DB.
type ChannelAccount struct {
	ID         string
	SecretKey  string
	PublicKey  string
	CurrentSeq int64
	IsActive   bool
	LockedAt   *time.Time
	CreatedAt  time.Time
}

// NewTransactionParams holds the parameters for creating a new pending transaction.
type NewTransactionParams struct {
	ExternalID      string
	SourceAccount   string
	Destination     string
	Amount          decimal.Decimal
	AssetCode       string
	AssetIssuer     string
}

// Store provides the interface for persistent transaction and channel account state.
type Store interface {
	// Transaction operations
	CreateTransaction(ctx context.Context, params NewTransactionParams) (*Transaction, error)
	GetTransaction(ctx context.Context, id string) (*Transaction, error)
	GetByExternalID(ctx context.Context, externalID string) (*Transaction, error)
	ListPending(ctx context.Context, limit int) ([]*Transaction, error)
	UpdateStatus(ctx context.Context, txID string, newStatus Status, detail string, horizonTxID *string) error
	TransitionHistory(ctx context.Context, txID string) ([]*Transition, error)
	MarkChannelUsed(ctx context.Context, txID, channelPublicKey string) error

	// Channel account pool
	AcquireChannelAccount(ctx context.Context) (*ChannelAccount, error)
	ReleaseChannelAccount(ctx context.Context, publicKey string) error
	UnlockAllChannelAccounts(ctx context.Context) error
	CreateChannelAccount(ctx context.Context, secretKey string) (*ChannelAccount, error)
	ListChannelAccounts(ctx context.Context, activeOnly bool) ([]*ChannelAccount, error)
	UpdateChannelAccountSeq(ctx context.Context, publicKey string, seq int64) error
	DeactivateChannelAccount(ctx context.Context, publicKey string) error
}

// txStore wraps pgxpool.Pool and provides transactional query execution.
type txStore struct {
	pool *pgxpool.Pool
}

// New creates a new Store backed by the given pgxpool.Pool.
func New(pool *pgxpool.Pool) Store {
	return &txStore{pool: pool}
}

// --- Transaction Operations ---

const createTransactionSQL = `
INSERT INTO transactions (external_id, source_account, destination, amount, asset_code, asset_issuer, status)
VALUES ($1, $2, $3, $4, $5, $6, 'pending')
RETURNING id, external_id, source_account, destination, amount, asset_code, asset_issuer,
          channel_account, status, created_at, updated_at`

func (s *txStore) CreateTransaction(ctx context.Context, params NewTransactionParams) (*Transaction, error) {
	var tx Transaction
	err := s.pool.QueryRow(ctx, createTransactionSQL,
		params.ExternalID,
		params.SourceAccount,
		params.Destination,
		params.Amount,
		params.AssetCode,
		params.AssetIssuer,
	).Scan(
		&tx.ID, &tx.ExternalID, &tx.SourceAccount, &tx.Destination,
		&tx.Amount, &tx.AssetCode, &tx.AssetIssuer,
		&tx.ChannelAccount, &tx.Status,
		&tx.CreatedAt, &tx.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &tx, nil
}

const getTransactionSQL = `
SELECT id, external_id, source_account, destination, amount, asset_code, asset_issuer,
       channel_account, status, horizon_tx_id, error_message, created_at, updated_at
FROM transactions
WHERE id = $1`

func (s *txStore) GetTransaction(ctx context.Context, id string) (*Transaction, error) {
	var tx Transaction
	var horizonTxID, errMsg *string

	err := s.pool.QueryRow(ctx, getTransactionSQL, id).Scan(
		&tx.ID, &tx.ExternalID, &tx.SourceAccount, &tx.Destination,
		&tx.Amount, &tx.AssetCode, &tx.AssetIssuer,
		&tx.ChannelAccount, &tx.Status,
		&horizonTxID, &errMsg, &tx.CreatedAt, &tx.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	tx.HorizonTxID = horizonTxID
	tx.ErrorMessage = errMsg
	return &tx, nil
}

const getByExternalIDSQL = `
SELECT id, external_id, source_account, destination, amount, asset_code, asset_issuer,
       channel_account, status, horizon_tx_id, error_message, created_at, updated_at
FROM transactions
WHERE external_id = $1`

func (s *txStore) GetByExternalID(ctx context.Context, externalID string) (*Transaction, error) {
	var tx Transaction
	var horizonTxID, errMsg *string

	err := s.pool.QueryRow(ctx, getByExternalIDSQL, externalID).Scan(
		&tx.ID, &tx.ExternalID, &tx.SourceAccount, &tx.Destination,
		&tx.Amount, &tx.AssetCode, &tx.AssetIssuer,
		&tx.ChannelAccount, &tx.Status,
		&horizonTxID, &errMsg, &tx.CreatedAt, &tx.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	tx.HorizonTxID = horizonTxID
	tx.ErrorMessage = errMsg
	return &tx, nil
}

const listPendingSQL = `
SELECT id, external_id, source_account, destination, amount, asset_code, asset_issuer,
       channel_account, status, created_at, updated_at
FROM transactions
WHERE status = 'pending'
ORDER BY created_at ASC
LIMIT $1`

func (s *txStore) ListPending(ctx context.Context, limit int) ([]*Transaction, error) {
	rows, err := s.pool.Query(ctx, listPendingSQL, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txs []*Transaction
	for rows.Next() {
		var tx Transaction
		var amount decimal.Decimal
		err := rows.Scan(
			&tx.ID, &tx.ExternalID, &tx.SourceAccount, &tx.Destination,
			&amount, &tx.AssetCode, &tx.AssetIssuer,
			&tx.ChannelAccount, &tx.Status, &tx.CreatedAt, &tx.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		tx.Amount = amount
		txs = append(txs, &tx)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return txs, nil
}

const updateStatusSQL = `
UPDATE transactions
SET status = $2, horizon_tx_id = $3, error_message = $4, updated_at = now()
WHERE id = $1`

const insertTransitionSQL = `
INSERT INTO transitions (transaction_id, from_status, to_status, detail)
VALUES ($1, $2, $3, $4)`

func (s *txStore) UpdateStatus(ctx context.Context, txID string, newStatus Status, detail string, horizonTxID *string) error {
	var currentStatus Status
	err := s.pool.QueryRow(ctx, "SELECT status FROM transactions WHERE id = $1", txID).Scan(&currentStatus)
	if err == pgx.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("getting current status: %w", err)
	}

	if newStatus.IsTerminal() && currentStatus.IsTerminal() {
		return ErrTerminalState
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	fromStatus := currentStatus
	_, err = tx.Exec(ctx, updateStatusSQL, txID, newStatus, horizonTxID, &detail)
	if err != nil {
		return fmt.Errorf("updating status: %w", err)
	}

	_, err = tx.Exec(ctx, insertTransitionSQL, txID, fromStatus, newStatus, &detail)
	if err != nil {
		return fmt.Errorf("inserting transition: %w", err)
	}

	return tx.Commit(ctx)
}

const transitionHistorySQL = `
SELECT id, transaction_id, from_status, to_status, detail, attempted_at
FROM transitions
WHERE transaction_id = $1
ORDER BY attempted_at ASC`

func (s *txStore) TransitionHistory(ctx context.Context, txID string) ([]*Transition, error) {
	rows, err := s.pool.Query(ctx, transitionHistorySQL, txID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transitions []*Transition
	for rows.Next() {
		var t Transition
		var fromStatus *string
		err := rows.Scan(&t.ID, &t.TransactionID, &fromStatus, &t.ToStatus, &t.Detail, &t.AttemptedAt)
		if err != nil {
			return nil, err
		}
		if fromStatus != nil {
			s := Status(*fromStatus)
			t.FromStatus = &s
		}
		transitions = append(transitions, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return transitions, nil
}

const markChannelUsedSQL = `
UPDATE transactions
SET channel_account = $2, updated_at = now()
WHERE id = $1 AND status = 'pending'`

func (s *txStore) MarkChannelUsed(ctx context.Context, txID, channelPublicKey string) error {
	_, err := s.pool.Exec(ctx, markChannelUsedSQL, txID, channelPublicKey)
	return err
}

// --- Channel Account Operations ---

const acquireChannelAccountSQL = `
SELECT id, secret_key, public_key, current_seq, is_active, locked_at, created_at
FROM channel_accounts
WHERE is_active = true AND locked_at IS NULL
ORDER BY current_seq ASC
FOR UPDATE SKIP LOCKED
LIMIT 1`

const lockChannelAccountSQL = `
UPDATE channel_accounts SET current_seq = $2, locked_at = now() WHERE public_key = $1`

// AcquireChannelAccount atomically claims the least-recently-used active,
// unlocked channel account. FOR UPDATE SKIP LOCKED lets concurrent callers
// skip rows already being claimed instead of blocking on them, and locked_at
// is set (surviving past this transaction, unlike the row lock) so the
// account stays claimed for the entire submission — not just the sequence
// bump — until ReleaseChannelAccount clears it. This is what stops a process
// restart or a second caller from double-assigning an account still mid-flight.
func (s *txStore) AcquireChannelAccount(ctx context.Context) (*ChannelAccount, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("beginning acquire transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var ca ChannelAccount
	err = tx.QueryRow(ctx, acquireChannelAccountSQL).Scan(
		&ca.ID, &ca.SecretKey, &ca.PublicKey,
		&ca.CurrentSeq, &ca.IsActive, &ca.LockedAt, &ca.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, ErrNoChannelAccounts
	}
	if err != nil {
		return nil, fmt.Errorf("selecting channel account: %w", err)
	}

	nextSeq := ca.CurrentSeq + 1
	_, err = tx.Exec(ctx, lockChannelAccountSQL, ca.PublicKey, nextSeq)
	if err != nil {
		return nil, fmt.Errorf("locking channel account: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing acquire: %w", err)
	}

	ca.CurrentSeq = nextSeq
	return &ca, nil
}

const releaseChannelAccountSQL = `
UPDATE channel_accounts SET locked_at = NULL WHERE public_key = $1`

// ReleaseChannelAccount frees a channel account for reuse. It deliberately
// does not touch current_seq: a released account may have just had a
// transaction submitted under its reserved sequence, and Stellar sequence
// numbers only ever move forward — rolling the counter back would hand the
// same sequence to the next caller and guarantee a tx_bad_seq collision.
func (s *txStore) ReleaseChannelAccount(ctx context.Context, publicKey string) error {
	_, err := s.pool.Exec(ctx, releaseChannelAccountSQL, publicKey)
	return err
}

const unlockAllChannelAccountsSQL = `
UPDATE channel_accounts SET locked_at = NULL WHERE locked_at IS NOT NULL`

// UnlockAllChannelAccounts clears every lock in the pool. Call once at
// startup, after sequences have been re-synced from the network: any lock
// still held from before a restart is stale (its owning process is gone),
// and the fresh on-chain sequence sync already makes it safe to reuse the
// account regardless of whether the in-flight transaction landed.
func (s *txStore) UnlockAllChannelAccounts(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, unlockAllChannelAccountsSQL)
	return err
}

const createChannelAccountSQL = `
INSERT INTO channel_accounts (secret_key, public_key, current_seq)
VALUES ($1, $2, $3)
ON CONFLICT (public_key) DO UPDATE SET secret_key = EXCLUDED.secret_key
RETURNING id, secret_key, public_key, current_seq, is_active, locked_at, created_at`

func (s *txStore) CreateChannelAccount(ctx context.Context, secretKey string) (*ChannelAccount, error) {
	kp, err := keypair.ParseFull(secretKey)
	if err != nil {
		return nil, fmt.Errorf("invalid channel account secret: %w", err)
	}
	var ca ChannelAccount
	err = s.pool.QueryRow(ctx, createChannelAccountSQL,
		secretKey, kp.Address(), int64(0),
	).Scan(
		&ca.ID, &ca.SecretKey, &ca.PublicKey,
		&ca.CurrentSeq, &ca.IsActive, &ca.LockedAt, &ca.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &ca, nil
}

const listChannelAccountsSQL = `
SELECT id, secret_key, public_key, current_seq, is_active, locked_at, created_at
FROM channel_accounts
WHERE $1 = false OR is_active = true
ORDER BY created_at ASC`

func (s *txStore) ListChannelAccounts(ctx context.Context, activeOnly bool) ([]*ChannelAccount, error) {
	rows, err := s.pool.Query(ctx, listChannelAccountsSQL, activeOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []*ChannelAccount
	for rows.Next() {
		var ca ChannelAccount
		err := rows.Scan(&ca.ID, &ca.SecretKey, &ca.PublicKey,
			&ca.CurrentSeq, &ca.IsActive, &ca.LockedAt, &ca.CreatedAt)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, &ca)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return accounts, nil
}

const updateChannelAccountSeqSQL = `
UPDATE channel_accounts SET current_seq = $2 WHERE public_key = $1`

func (s *txStore) UpdateChannelAccountSeq(ctx context.Context, publicKey string, seq int64) error {
	_, err := s.pool.Exec(ctx, updateChannelAccountSeqSQL, publicKey, seq)
	return err
}

const deactivateChannelAccountSQL = `
UPDATE channel_accounts SET is_active = false WHERE public_key = $1`

func (s *txStore) DeactivateChannelAccount(ctx context.Context, publicKey string) error {
	_, err := s.pool.Exec(ctx, deactivateChannelAccountSQL, publicKey)
	return err
}
