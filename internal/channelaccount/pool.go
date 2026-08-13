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
// The underlying store uses SELECT FOR UPDATE SKIP LOCKED so that concurrent
// callers never receive the same account. Returns an error if none available.
func (p *Pool) Acquire(ctx context.Context) (*state.ChannelAccount, error) {
	ca, err := p.store.AcquireChannelAccount(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring channel account: %w", err)
	}
	return ca, nil
}

// Release marks a channel account as available for reuse. It only clears the
// lock set by Acquire — current_seq is never rolled back here, since a
// released account may have just had a transaction submitted under its
// reserved sequence and Stellar sequence numbers only ever move forward.
func (p *Pool) Release(ctx context.Context, publicKey string) error {
	if err := p.store.ReleaseChannelAccount(ctx, publicKey); err != nil {
		return fmt.Errorf("releasing channel account %s: %w", publicKey, err)
	}
	return nil
}

// ResetLocks clears every lock in the pool. Call once at startup, after
// sequences have been re-synced from the network: a lock still held from
// before a restart belongs to a process that no longer exists, and the fresh
// sequence sync already makes the account safe to hand out again.
func (p *Pool) ResetLocks(ctx context.Context) error {
	return p.store.UnlockAllChannelAccounts(ctx)
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

// InitPool seeds the pool with the given channel account secret seeds. It is
// idempotent: the underlying store upserts on public_key, so re-seeding an
// already-present account preserves its current_seq and never creates a
// duplicate. Safe to call on every startup.
func (p *Pool) InitPool(ctx context.Context, secretKeys []string) error {
	for _, sk := range secretKeys {
		if _, err := p.AddAccount(ctx, sk); err != nil {
			return fmt.Errorf("seeding channel account: %w", err)
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
