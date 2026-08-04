-- 0001_init.up.sql — Initial schema for Stellar TX Submitter

-- Transaction statuses: pending, submitted, confirmed, failed, dead_letter
CREATE TYPE transaction_status AS ENUM (
    'pending',
    'submitted',
    'confirmed',
    'failed',
    'dead_letter'
);

-- Transactions table: one row per payment request
CREATE TABLE transactions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id     TEXT NOT NULL UNIQUE,        -- caller-provided idempotency key
    source_account  TEXT NOT NULL,               -- Stellar account that owns the funds
    destination     TEXT NOT NULL,               -- recipient Stellar address
    amount          NUMERIC(78, 0) NOT NULL,     -- amount in stroops (int64 precision)
    asset_code      TEXT NOT NULL DEFAULT 'XLM', -- asset code (XLM or issued asset)
    asset_issuer    TEXT NOT NULL DEFAULT '',    -- issuer address (empty for XLM)
    channel_account TEXT NOT NULL,               -- channel account used for submission
    status          transaction_status NOT NULL DEFAULT 'pending',
    horizon_tx_id   TEXT,                        -- Horizon transaction hash once submitted
    error_message   TEXT,                        -- last error if failed
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Channel accounts table: the pool of accounts used to avoid sequence collisions
CREATE TABLE channel_accounts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    secret_key      TEXT NOT NULL,               -- master keypair secret (stored in plaintext for now)
    public_key      TEXT NOT NULL UNIQUE,        -- derived public key
    current_seq     BIGINT NOT NULL DEFAULT 0,   -- latest sequence used by this account
    is_active       BOOLEAN NOT NULL DEFAULT true, -- false = draining/merged back
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Transition history: every state change is appended as a row
CREATE TABLE transitions (
    id              BIGSERIAL PRIMARY KEY,
    transaction_id  UUID NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    from_status     transaction_status,          -- NULL for initial transition from "nothing"
    to_status       transaction_status NOT NULL,
    detail          TEXT,                        -- Horizon response, error message, etc.
    attempted_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Indexes for common queries
CREATE INDEX idx_transactions_status ON transactions(status);
CREATE INDEX idx_transactions_external_id ON transactions(external_id);
CREATE INDEX idx_transactions_transaction_id ON transitions(transaction_id);
CREATE INDEX idx_channel_accounts_active ON channel_accounts(is_active) WHERE is_active = true;
