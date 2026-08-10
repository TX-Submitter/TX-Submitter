// Package submitter handles building, signing, and submitting Stellar payments.
package submitter

import (
	"context"
	"fmt"

	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/txnbuild"
)

// Builder constructs Stellar payment transactions from payment parameters.
type Builder struct {
	baseFee       int64
	maxBaseFee    int64
	networkPass   string
}

// NewBuilder creates a new transaction builder.
func NewBuilder(networkPass string, baseFee, maxBaseFee int64) *Builder {
	return &Builder{
		networkPass: networkPass,
		baseFee:     baseFee,
		maxBaseFee:  maxBaseFee,
	}
}

// PaymentParams holds the parameters for building a payment transaction.
// The caller provides the channel account's current sequence so the
// built transaction uses the correct sequence number.
type PaymentParams struct {
	Destination    string
	Amount         string
	AssetCode      string // "XLM" for native, or issued asset code
	AssetIssuer    string // empty for native XLM
	ChannelAccount string // channel account providing the sequence
	ChannelSeq     int64  // current sequence of the channel account
	MaxLedger      uint32 // ledger bound ceiling; prevents replay after long delays
	BaseFee        int64  // fee for this build; caller may pass an escalated fee
}

// resolveAsset maps an asset code/issuer pair to a txnbuild.Asset. A code of
// "XLM" with no issuer is the native asset; anything else is a credit asset
// (the SDK selects the AlphaNum4/AlphaNum12 XDR variant by code length).
func resolveAsset(code, issuer string) txnbuild.Asset {
	if code == "XLM" && issuer == "" {
		return txnbuild.NativeAsset{}
	}
	return txnbuild.CreditAsset{Code: code, Issuer: issuer}
}

// Build creates a classic Stellar payment transaction. The returned
// transaction is unsigned; the caller signs it with the channel account
// key and, if the funding source differs, the source account key.
func (b *Builder) Build(ctx context.Context, params PaymentParams) (*txnbuild.Transaction, error) {
	if _, err := keypair.ParseAddress(params.ChannelAccount); err != nil {
		return nil, fmt.Errorf("parsing channel account: %w", err)
	}
	if _, err := keypair.ParseAddress(params.Destination); err != nil {
		return nil, fmt.Errorf("parsing destination: %w", err)
	}

	asset := resolveAsset(params.AssetCode, params.AssetIssuer)

	// Apply fee cap.
	fee := params.BaseFee
	if fee == 0 {
		fee = b.baseFee
	}
	if fee > b.maxBaseFee {
		fee = b.maxBaseFee
	}

	preconditions := txnbuild.Preconditions{
		TimeBounds: txnbuild.NewTimeout(300),
	}
	if params.MaxLedger > 0 {
		preconditions.LedgerBounds = &txnbuild.LedgerBounds{MaxLedger: params.MaxLedger}
	}

	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount: &txnbuild.SimpleAccount{
			AccountID: params.ChannelAccount,
			Sequence:  params.ChannelSeq,
		},
		IncrementSequenceNum: false,
		Operations: []txnbuild.Operation{&txnbuild.Payment{
			Destination: params.Destination,
			Amount:      params.Amount,
			Asset:       asset,
		}},
		BaseFee:       fee,
		Preconditions: preconditions,
	})
	if err != nil {
		return nil, fmt.Errorf("building transaction: %w", err)
	}

	return tx, nil
}
