// Package submitter handles building, signing, and submitting Stellar payments.
package submitter

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gamp/stellar-tx-submitter/internal/metrics"
	"github.com/gamp/stellar-tx-submitter/internal/retry"
	"github.com/gamp/stellar-tx-submitter/internal/state"
	"github.com/gamp/stellar-tx-submitter/internal/channelaccount"
	"github.com/stellar/go-stellar-sdk/clients/horizonclient"
	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/protocols/horizon"
	"github.com/stellar/go-stellar-sdk/txnbuild"
)

// ledgerBoundOffset is how many ledgers ahead of the current ledger a
// transaction remains valid. At ~5s per ledger this is roughly 20 minutes,
// comfortably longer than the 300s time bound plus retry window.
const ledgerBoundOffset = 240

// Engine builds, signs, submits, and polls for Stellar payment inclusion.
type Engine struct {
	horizon        *horizonclient.Client
	store          state.Store
	pool           *channelaccount.Pool
	builder        *Builder
	retryPolicy    *retry.Policy
	networkPass    string
	distribution   string
	distributionKP *keypair.Full
	maxBaseFee     int64
	pollInterval   time.Duration
	logger         *slog.Logger
}

// NewEngine creates a new SubmitterEngine.
func NewEngine(
	horizon *horizonclient.Client,
	store state.Store,
	pool *channelaccount.Pool,
	builder *Builder,
	retryPolicy *retry.Policy,
	networkPassphrase, distribution string,
	maxBaseFee int64,
	queuePollingInterval time.Duration,
	logger *slog.Logger,
) (*Engine, error) {
	kp, err := keypair.ParseFull(distribution)
	if err != nil {
		return nil, fmt.Errorf("parsing distribution key: %w", err)
	}

	return &Engine{
		horizon:        horizon,
		store:          store,
		pool:           pool,
		builder:        builder,
		retryPolicy:    retryPolicy,
		networkPass:    networkPassphrase,
		distribution:   distribution,
		distributionKP: kp,
		maxBaseFee:     maxBaseFee,
		pollInterval:   queuePollingInterval,
		logger:         logger,
	}, nil
}

// currentLedgerBound returns a MaxLedger ceiling for new transactions based on
// the network's latest ledger. On error it returns 0, which the builder treats
// as "no ledger bound" — submission still proceeds, just without replay
// protection for that one transaction.
func (e *Engine) currentLedgerBound() uint32 {
	root, err := e.horizon.Root()
	if err != nil {
		e.logger.Warn("fetching root for ledger bound failed", "error", err)
		return 0
	}
	if root.HorizonSequence <= 0 {
		return 0
	}
	return uint32(root.HorizonSequence) + ledgerBoundOffset
}

// SubmitProcess builds, signs, submits, and polls one pending transaction.
func (e *Engine) SubmitProcess(ctx context.Context, tx *state.Transaction) error {
	start := time.Now()
	defer func() {
		metrics.ObserveSubmitLatency(time.Since(start).Seconds())
	}()

	// Acquire a channel account from the pool
	ca, err := e.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring channel account: %w", err)
	}
	defer func() {
		_ = e.pool.Release(ctx, ca.PublicKey)
	}()

	// Mark the transaction as using this channel
	err = e.store.MarkChannelUsed(ctx, tx.ID, ca.PublicKey)
	if err != nil {
		return fmt.Errorf("marking channel used: %w", err)
	}

	// Build the transaction via the shared builder so LedgerBounds and fee
	// capping stay consistent between initial submission and fee-bumps.
	channelKP, err := keypair.ParseFull(ca.SecretKey)
	if err != nil {
		return fmt.Errorf("parsing channel key: %w", err)
	}

	starTx, err := e.builder.Build(ctx, PaymentParams{
		Destination:    tx.Destination,
		Amount:         tx.Amount.String(),
		AssetCode:      tx.AssetCode,
		AssetIssuer:    tx.AssetIssuer,
		ChannelAccount: ca.PublicKey,
		ChannelSeq:     ca.CurrentSeq,
		MaxLedger:      e.currentLedgerBound(),
		BaseFee:        e.retryPolicy.CurrentFee(0),
	})
	if err != nil {
		return fmt.Errorf("building transaction: %w", err)
	}

	// Sign with channel account key
	starTx, err = starTx.Sign(e.networkPass, channelKP)
	if err != nil {
		return fmt.Errorf("signing with channel key: %w", err)
	}

	// Sign with distribution (source) key if different from channel
	if tx.SourceAccount != ca.PublicKey {
		sourceKP, err := keypair.ParseFull(tx.SourceAccount)
		if err != nil {
			return fmt.Errorf("parsing source key for dual-sign: %w", err)
		}
		starTx, err = starTx.Sign(e.networkPass, sourceKP)
		if err != nil {
			return fmt.Errorf("signing with source key: %w", err)
		}
	}

	// Submit to Horizon
	horizonResp, err := e.horizon.SubmitTransaction(starTx)
	if err != nil {
		return e.handleSubmissionError(ctx, tx, ca, horizonResp, err)
	}

	// Record submitted state
	horizonID := horizonResp.Hash
	err = e.store.UpdateStatus(ctx, tx.ID, state.StatusSubmitted, "submitted to horizon", &horizonID)
	if err != nil {
		return fmt.Errorf("updating status to submitted: %w", err)
	}

	// Update channel account sequence from the response
	if ca.CurrentSeq > 0 {
		_ = e.pool.RefreshSequence(ctx, ca.PublicKey, ca.CurrentSeq)
	}

	// Poll for inclusion confirmation
	return e.pollForConfirmation(ctx, tx.ID, ca.PublicKey, horizonID)
}

// handleSubmissionError classifies submission errors and attempts fee-bump escalation.
func (e *Engine) handleSubmissionError(ctx context.Context, tx *state.Transaction, ca *state.ChannelAccount, resp horizon.Transaction, err error) error {
	maxAttempts := e.retryPolicy.MaxAttempts()

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if !e.retryPolicy.ShouldRetry(err, attempt-1) {
			// Not retryable — terminal failure
			e.markFailed(ctx, tx.ID, fmt.Sprintf("terminal error: %v", err))
			return fmt.Errorf("terminal error for tx %s: %w", tx.ID, err)
		}

		newFee := e.retryPolicy.CurrentFee(attempt)
		if newFee > e.maxBaseFee {
			newFee = e.maxBaseFee
		}

		// Wait backoff before retry
		backoff := e.retryPolicy.Backoff(attempt)
		if backoff > 0 {
			time.Sleep(backoff)
		}

		// Attempt fee-bump retry
		feeBumpTx, bumpErr := e.buildFeeBump(ctx, tx, ca, newFee)
		if bumpErr != nil {
			e.logger.Warn("fee-bump build failed", "tx_id", tx.ID, "attempt", attempt, "error", bumpErr)
			err = bumpErr
			continue
		}

		// Submit fee-bumped transaction
		bumpResp, bumpSubmitErr := e.horizon.SubmitFeeBumpTransaction(feeBumpTx)
		if bumpSubmitErr != nil {
			e.logger.Warn("fee-bump submission failed", "tx_id", tx.ID, "attempt", attempt, "error", bumpSubmitErr)
			err = bumpSubmitErr
			continue
		}

		// Fee-bump submitted successfully, record transition
		horizonID := bumpResp.Hash
		return e.store.UpdateStatus(ctx, tx.ID, state.StatusSubmitted, fmt.Sprintf("fee-bumped at attempt %d", attempt), &horizonID)
	}

	// Exhausted retries — dead letter
	e.markDeadLetter(ctx, tx.ID, fmt.Sprintf("max retries exceeded: %v", err))
	return fmt.Errorf("max retries exceeded for tx %s: %w", tx.ID, err)
}

// buildFeeBump creates a fee-bump transaction for a stuck payment.
// The inner transaction is a copy of the original payment with the
// higher fee, signed by the distribution account.
func (e *Engine) buildFeeBump(ctx context.Context, tx *state.Transaction, ca *state.ChannelAccount, newFee int64) (*txnbuild.FeeBumpTransaction, error) {
	channelKP, err := keypair.ParseFull(ca.SecretKey)
	if err != nil {
		return nil, fmt.Errorf("parsing channel key for fee-bump: %w", err)
	}

	// Build inner transaction matching the original payment
	innerTx, err := e.builder.Build(ctx, PaymentParams{
		Destination:    tx.Destination,
		Amount:         tx.Amount.String(),
		AssetCode:      tx.AssetCode,
		AssetIssuer:    tx.AssetIssuer,
		ChannelAccount: ca.PublicKey,
		ChannelSeq:     ca.CurrentSeq,
		MaxLedger:      e.currentLedgerBound(),
		BaseFee:        newFee,
	})
	if err != nil {
		return nil, fmt.Errorf("building inner tx for fee-bump: %w", err)
	}

	innerTx, err = innerTx.Sign(e.networkPass, channelKP)
	if err != nil {
		return nil, fmt.Errorf("signing inner tx: %w", err)
	}

	feeBump, err := txnbuild.NewFeeBumpTransaction(txnbuild.FeeBumpTransactionParams{
		Inner:      innerTx,
		FeeAccount: e.distribution,
		BaseFee:    newFee * 5,
	})
	if err != nil {
		return nil, fmt.Errorf("building fee-bump tx: %w", err)
	}

	feeBump, err = feeBump.Sign(e.networkPass, e.distributionKP)
	if err != nil {
		return nil, fmt.Errorf("signing fee-bump tx: %w", err)
	}

	return feeBump, nil
}

// pollForConfirmation polls Horizon until the transaction is confirmed or fails.
func (e *Engine) pollForConfirmation(ctx context.Context, txID, channelPublicKey, horizonID string) error {
	pollInterval := 2 * time.Second
	maxPolls := 30 // ~1 minute max polling

	for i := 0; i < maxPolls; i++ {
		time.Sleep(pollInterval)

		resp, err := e.horizon.TransactionDetail(horizonID)
		if err != nil {
			// Transaction not yet found — keep polling
			continue
		}

		if resp.Successful {
			err = e.store.UpdateStatus(ctx, txID, state.StatusConfirmed, "confirmed on chain", &horizonID)
			if err != nil {
				return fmt.Errorf("updating to confirmed: %w", err)
			}
			return nil
		}
		// resp.Successful == false means failed
		errMsg := "transaction failed"
		e.markFailed(ctx, txID, errMsg)
		return fmt.Errorf(errMsg)
	}

	// Timed out polling
	errMsg := "polling timed out — transaction not confirmed"
	e.markFailed(ctx, txID, errMsg)
	return fmt.Errorf(errMsg)
}

// markFailed records a terminal failure state.
func (e *Engine) markFailed(ctx context.Context, txID, errMsg string) {
	_ = e.store.UpdateStatus(ctx, txID, state.StatusFailed, errMsg, nil)
}

// markDeadLetter records a dead-letter state after retry exhaustion.
func (e *Engine) markDeadLetter(ctx context.Context, txID, reason string) {
	_ = e.store.UpdateStatus(ctx, txID, state.StatusDeadLetter, reason, nil)
	metrics.IncrementDeadLetters()
}

// Start begins the main submission loop, polling for pending transactions.
func (e *Engine) Start(ctx context.Context) {
	ticker := time.NewTicker(e.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pending, err := e.store.ListPending(ctx, 10)
			if err != nil {
				e.logger.Error("failed to list pending transactions", "error", err)
				continue
			}

			metrics.SetPending(float64(len(pending)))

			for _, tx := range pending {
				if err := e.SubmitProcess(ctx, tx); err != nil {
					e.logger.Error("submit process failed", "tx_id", tx.ID, "error", err)
				}
			}
		}
	}
}
