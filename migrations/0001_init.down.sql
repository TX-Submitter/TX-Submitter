-- 0001_init.down.sql

DROP INDEX IF EXISTS idx_channel_accounts_active;
DROP INDEX IF EXISTS idx_transactions_transaction_id;
DROP INDEX IF EXISTS idx_transactions_external_id;
DROP INDEX IF EXISTS idx_transactions_status;

DROP TABLE IF EXISTS transitions;
DROP TABLE IF EXISTS channel_accounts;
DROP TABLE IF EXISTS transactions;

DROP TYPE IF EXISTS transaction_status;
