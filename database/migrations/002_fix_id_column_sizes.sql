-- ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
-- Migration 002: Fix VARCHAR column sizes for id and FK fields
-- ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
-- Purpose: Fix "Data truncated for column 'id'" error on INSERT
-- Cause:   MySQL AUTO_MIGRATE may have created columns with smaller VARCHAR
--          sizes (e.g. VARCHAR(36)) that can't hold UUID values in STRICT mode
-- Impact:  SAFE — only WIDENs columns, never drops or truncates existing data
-- ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

-- transactions table
ALTER TABLE transactions MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE transactions MODIFY COLUMN user_id VARCHAR(191) NOT NULL;
ALTER TABLE transactions MODIFY COLUMN account_id VARCHAR(191) NOT NULL;
ALTER TABLE transactions MODIFY COLUMN category_id VARCHAR(191) NULL;
ALTER TABLE transactions MODIFY COLUMN transfer_to_id VARCHAR(191) NULL;

-- finance_accounts table
ALTER TABLE finance_accounts MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE finance_accounts MODIFY COLUMN user_id VARCHAR(191) NOT NULL;

-- categories table
ALTER TABLE categories MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE categories MODIFY COLUMN user_id VARCHAR(191) NULL;

-- users table
ALTER TABLE users MODIFY COLUMN id VARCHAR(191) NOT NULL;

-- budgets table
ALTER TABLE budgets MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE budgets MODIFY COLUMN user_id VARCHAR(191) NOT NULL;
ALTER TABLE budgets MODIFY COLUMN category_id VARCHAR(191) NOT NULL;

-- savings_goals table
ALTER TABLE savings_goals MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE savings_goals MODIFY COLUMN user_id VARCHAR(191) NOT NULL;
ALTER TABLE savings_goals MODIFY COLUMN account_id VARCHAR(191) NULL;

-- recurring_bills table
ALTER TABLE recurring_bills MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE recurring_bills MODIFY COLUMN user_id VARCHAR(191) NOT NULL;
ALTER TABLE recurring_bills MODIFY COLUMN category_id VARCHAR(191) NULL;
ALTER TABLE recurring_bills MODIFY COLUMN account_id VARCHAR(191) NULL;

-- asset_holdings table
ALTER TABLE asset_holdings MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE asset_holdings MODIFY COLUMN user_id VARCHAR(191) NOT NULL;
ALTER TABLE asset_holdings MODIFY COLUMN account_id VARCHAR(191) NOT NULL;

-- notifications table
ALTER TABLE notifications MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE notifications MODIFY COLUMN user_id VARCHAR(191) NOT NULL;

-- api_keys table
ALTER TABLE api_keys MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE api_keys MODIFY COLUMN user_id VARCHAR(191) NOT NULL;
