-- 0002_channel_account_lock.down.sql

DROP INDEX IF EXISTS idx_channel_accounts_available;
ALTER TABLE channel_accounts DROP COLUMN IF EXISTS locked_at;
