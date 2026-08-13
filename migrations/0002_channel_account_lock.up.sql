-- 0002_channel_account_lock.up.sql — persistent in-use tracking for channel accounts
--
-- Acquire previously only held a row lock for the duration of the short
-- "bump current_seq" transaction, so nothing recorded that an account was
-- mid-flight in a submission. A process restart (or a second concurrent
-- Acquire) could then hand the same account out again while a transaction
-- built from it was still being signed/submitted/polled. locked_at closes
-- that gap: an account is only eligible for Acquire while locked_at IS NULL.

ALTER TABLE channel_accounts ADD COLUMN locked_at TIMESTAMPTZ;

CREATE INDEX idx_channel_accounts_available ON channel_accounts(current_seq)
    WHERE is_active = true AND locked_at IS NULL;
