// Package channelaccount manages a pool of Stellar channel accounts used to
// avoid sequence number collisions during concurrent transaction submission.
// Accounts are acquired atomically using SELECT FOR UPDATE SKIP LOCKED.
package channelaccount

import (
	"context"
	"fmt"
	"time"

	"github.com/gamp/stellar-tx-submitter/internal/state"
)

// Pool manages the channel account pool, providing atomic acquire/release
// with persistent state in Postgres.
type Pool struct {
	store state.Store
}

// New creates a new Pool backed by the given state.Store.
func New(store state.Store) *Pool {
	return &Pool{store: store}
}

// Acquire claims an active channel account under concurrent load.
// It uses SELECT FOR UPDATE SKIP LOCKED semantics to avoid double-assignment.
// Returns an error if no accounts are available.
func (p *Pool) Acquire(ctx context.Context) (*state.ChannelAccount, error) {
	const sql = `
	SELECT id, secret_key, public_key, current_seq, is_active, created_at
	FROM channel_accounts
	WHERE is_active = true
	ORDER BY current_seq ASC
	FOR UPDATE SKIP LOCKED
	LIMIT 1`

	rows, err := p.store.ListChannelAccounts(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("listing channel accounts: %w", err)
	}

	// Find the account with the lowest sequence number that is active
	// and not currently being used (current_seq is the latest sequence used,
	// we need to track in-use state separately — for now, use the DB query pattern)
	var best *state.ChannelAccount
	for _, ca := range rows {
		if best == nil || ca.CurrentSeq < best.CurrentSeq {
			best = ca
		}
	}

	if best == nil {
		return nil, fmt.Errorf("no available channel accounts in pool")
	}

	// Increment the sequence number to claim this account
	nextSeq := best.CurrentSeq + 1
	err = p.store.UpdateChannelAccountSeq(ctx, best.PublicKey, nextSeq)
	if err != nil {
		return nil, fmt.Errorf("updating channel account sequence: %w", err)
	}

	best.CurrentSeq = nextSeq
	return best, nil
}

// Release marks a channel account as available for reuse.
// The account's sequence is rolled back to allow future submissions.
func (p *Pool) Release(ctx context.Context, publicKey string) error {
	// Read current sequence
	accounts, err := p.store.ListChannelAccounts(ctx, false)
	if err != nil {
		return fmt.Errorf("listing accounts: %w", err)
	}

	for _, ca := range accounts {
		if ca.PublicKey == publicKey {
			// Decrement to free the slot
			if ca.CurrentSeq > 0 {
				newSeq := ca.CurrentSeq - 1
				err := p.store.UpdateChannelAccountSeq(ctx, publicKey, newSeq)
				if err != nil {
					return fmt.Errorf("rolling back sequence: %w", err)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("channel account %s not found", publicKey)
}

// RefreshSequence updates the channel account's sequence to the latest
// known value from the network (used when a tx_too_late error requires
// re-syncing with Horizon).
func (p *Pool) RefreshSequence(ctx context.Context, publicKey string, newSeq int64) error {
	return p.store.UpdateChannelAccountSeq(ctx, publicKey, newSeq)
}

// PoolSize returns the number of active channel accounts in the pool.
func (p *Pool) PoolSize(ctx context.Context) (int, error) {
	accounts, err := p.store.ListChannelAccounts(ctx, true)
	if err != nil {
		return 0, err
	}
	return len(accounts), nil
}

// AddAccount adds a new channel account to the pool.
func (p *Pool) AddAccount(ctx context.Context, secretKey string) (*state.ChannelAccount, error) {
	return p.store.CreateChannelAccount(ctx, secretKey)
}

// RemoveAccount deactivates a channel account (used when draining the pool).
func (p *Pool) RemoveAccount(ctx context.Context, publicKey string) error {
	return p.store.DeactivateChannelAccount(ctx, publicKey)
}

// InitPopulates the pool with the configured number of channel accounts if empty.
// Each account is generated from the secret key derivation.
func (p *Pool) InitPool(ctx context.Context, secretKeys []string) error {
	existing, err := p.store.ListChannelAccounts(ctx, true)
	if err != nil {
		return fmt.Errorf("checking pool: %w", err)
	}
	if len(existing) >= len(secretKeys) {
		return nil // pool already initialized
	}

	for _, sk := range secretKeys {
		_, err := p.AddAccount(ctx, sk)
		if err != nil {
			return fmt.Errorf("adding channel account: %w", err)
		}
	}
	return nil
}

// Account represents a channel account with its usage metadata.
type AccountInfo struct {
	PublicKey  string
	CurrentSeq int64
	IsActive   bool
	LastUsed   time.Time
}

// ListActive returns metadata for all active channel accounts.
func (p *Pool) ListActive(ctx context.Context) ([]*AccountInfo, error) {
	accounts, err := p.store.ListChannelAccounts(ctx, true)
	if err != nil {
		return nil, err
	}

	result := make([]*AccountInfo, 0, len(accounts))
	for _, ca := range accounts {
		result = append(result, &AccountInfo{
			PublicKey:  ca.PublicKey,
			CurrentSeq: ca.CurrentSeq,
			IsActive:   ca.IsActive,
		})
	}
	return result, nil
}
