// Package channelaccount provides channel account provisioning for the pool.
package channelaccount

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/stellar/go-stellar-sdk/clients/horizonclient"
	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/txnbuild"
)

// Provisioner creates and funds channel accounts on the Stellar network.
type Provisioner struct {
	horizon         *horizonclient.Client
	networkPass     string
	distributionKP  *keypair.Full
}

// NewProvisioner creates a provisioner that sponsors channel accounts from
// the distribution account.
func NewProvisioner(horizon *horizonclient.Client, networkPass, distributionSecret string) (*Provisioner, error) {
	kp, err := keypair.ParseFull(distributionSecret)
	if err != nil {
		return nil, fmt.Errorf("parsing distribution key: %w", err)
	}
	return &Provisioner{
		horizon:        horizon,
		networkPass:    networkPass,
		distributionKP: kp,
	}, nil
}

// ProvisionAccounts generates count new channel accounts, funds them with the
// distribution account via CreateAccount operations, and returns their secret keys.
// The caller stores these secrets in the database pool.
func (p *Provisioner) ProvisionAccounts(ctx context.Context, count int, startingBalance string) ([]string, error) {
	if count <= 0 {
		return nil, nil
	}

	// Fetch distribution account details for sequence
	distAcct, err := p.horizon.AccountDetail(horizonclient.AccountRequest{
		AccountID: p.distributionKP.Address(),
	})
	if err != nil {
		return nil, fmt.Errorf("fetching distribution account: %w", err)
	}

	// Generate keypairs
	channelKeys := make([]*keypair.Full, count)
	secrets := make([]string, count)
	for i := 0; i < count; i++ {
		kp, err := keypair.Random()
		if err != nil {
			return nil, fmt.Errorf("generating keypair %d: %w", i, err)
		}
		channelKeys[i] = kp
		secrets[i] = kp.Seed()
	}

	// Build one transaction with multiple CreateAccount operations
	ops := make([]txnbuild.Operation, count)
	for i, kp := range channelKeys {
		ops[i] = &txnbuild.CreateAccount{
			Destination: kp.Address(),
			Amount:      startingBalance,
		}
	}

	sourceAccount := txnbuild.SimpleAccount{
		AccountID: p.distributionKP.Address(),
		Sequence:  distAcct.Sequence,
	}

	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        &sourceAccount,
		IncrementSequenceNum: true,
		Operations:           ops,
		BaseFee:              txnbuild.MinBaseFee * int64(count),
		Preconditions:        txnbuild.Preconditions{TimeBounds: txnbuild.NewTimeout(300)},
	})
	if err != nil {
		return nil, fmt.Errorf("building provision transaction: %w", err)
	}

	tx, err = tx.Sign(p.networkPass, p.distributionKP)
	if err != nil {
		return nil, fmt.Errorf("signing provision transaction: %w", err)
	}

	_, err = p.horizon.SubmitTransaction(tx)
	if err != nil {
		return nil, fmt.Errorf("submitting provision transaction: %w", err)
	}

	return secrets, nil
}

// EnsurePoolSize checks the pool size and provisions additional accounts if below target.
func (p *Provisioner) EnsurePoolSize(ctx context.Context, pool *Pool, targetSize int, startingBalance string) error {
	currentSize, err := pool.PoolSize(ctx)
	if err != nil {
		return fmt.Errorf("checking pool size: %w", err)
	}

	if currentSize >= targetSize {
		return nil
	}

	needed := targetSize - currentSize
	secrets, err := p.ProvisionAccounts(ctx, needed, startingBalance)
	if err != nil {
		return fmt.Errorf("provisioning %d accounts: %w", needed, err)
	}

	for _, secret := range secrets {
		_, err := pool.AddAccount(ctx, secret)
		if err != nil {
			return fmt.Errorf("adding provisioned account to pool: %w", err)
		}
	}

	return nil
}

// SyncSequences fetches each active channel account's real on-chain sequence
// from Horizon and updates the stored current_seq accordingly. Freshly seeded
// or provisioned accounts default to current_seq = 0, which would make the
// first submission fail with tx_bad_seq; syncing aligns the pool with the
// network before any payment is built.
//
// An account that cannot be fetched (e.g. not yet funded on-chain) is logged
// and skipped rather than treated as fatal, so one bad account does not block
// startup for the rest of the pool.
func (p *Provisioner) SyncSequences(ctx context.Context, pool *Pool, logger *slog.Logger) error {
	accounts, err := pool.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("listing active accounts for sequence sync: %w", err)
	}

	for _, acct := range accounts {
		detail, err := p.horizon.AccountDetail(horizonclient.AccountRequest{
			AccountID: acct.PublicKey,
		})
		if err != nil {
			logger.Warn("fetching on-chain sequence failed; leaving current_seq unchanged",
				"account", acct.PublicKey, "error", err)
			continue
		}
		if err := pool.RefreshSequence(ctx, acct.PublicKey, detail.Sequence); err != nil {
			return fmt.Errorf("updating sequence for %s: %w", acct.PublicKey, err)
		}
	}

	return nil
}
