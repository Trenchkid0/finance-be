-- ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
-- Migration 002: Convert INT AUTO_INCREMENT id columns to VARCHAR(191)
-- ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
-- Purpose:  Fix "Incorrect integer value" when inserting UUID strings.
--           The existing DB was created with INT AUTO_INCREMENT ids (1,2,3...)
--           but the Go application now generates UUID string ids.
-- Safety:   SAFE — MySQL automatically converts integer values to their
--           string equivalents: 1 → '1', 2 → '2', etc. NO data is lost.
-- Method:   1. Disable FK checks to allow altering PK/FK columns freely
--           2. Strip AUTO_INCREMENT by setting column to plain INT (step 1)
--           3. Then convert INT → VARCHAR(191) (step 2)
--           4. Re-enable FK checks
-- ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

-- Disable FK checks so we can alter PK/FK columns without constraint errors
SET FOREIGN_KEY_CHECKS = 0;

-- ──────────────────────────────────────────────────────────────────────
-- users table
-- ──────────────────────────────────────────────────────────────────────
ALTER TABLE users MODIFY COLUMN id INT NOT NULL;
ALTER TABLE users MODIFY COLUMN id VARCHAR(191) NOT NULL;

-- ──────────────────────────────────────────────────────────────────────
-- finance_accounts table
-- ──────────────────────────────────────────────────────────────────────
ALTER TABLE finance_accounts MODIFY COLUMN id INT NOT NULL;
ALTER TABLE finance_accounts MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE finance_accounts MODIFY COLUMN user_id VARCHAR(191) NOT NULL;

-- ──────────────────────────────────────────────────────────────────────
-- categories table
-- ──────────────────────────────────────────────────────────────────────
ALTER TABLE categories MODIFY COLUMN id INT NOT NULL;
ALTER TABLE categories MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE categories MODIFY COLUMN user_id VARCHAR(191) NULL;

-- ──────────────────────────────────────────────────────────────────────
-- transactions table
-- ──────────────────────────────────────────────────────────────────────
ALTER TABLE transactions MODIFY COLUMN id INT NOT NULL;
ALTER TABLE transactions MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE transactions MODIFY COLUMN user_id VARCHAR(191) NOT NULL;
ALTER TABLE transactions MODIFY COLUMN account_id VARCHAR(191) NOT NULL;
ALTER TABLE transactions MODIFY COLUMN category_id VARCHAR(191) NULL;
ALTER TABLE transactions MODIFY COLUMN transfer_to_id VARCHAR(191) NULL;

-- ──────────────────────────────────────────────────────────────────────
-- budgets table
-- ──────────────────────────────────────────────────────────────────────
ALTER TABLE budgets MODIFY COLUMN id INT NOT NULL;
ALTER TABLE budgets MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE budgets MODIFY COLUMN user_id VARCHAR(191) NOT NULL;
ALTER TABLE budgets MODIFY COLUMN category_id VARCHAR(191) NOT NULL;

-- ──────────────────────────────────────────────────────────────────────
-- savings_goals table
-- ──────────────────────────────────────────────────────────────────────
ALTER TABLE savings_goals MODIFY COLUMN id INT NOT NULL;
ALTER TABLE savings_goals MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE savings_goals MODIFY COLUMN user_id VARCHAR(191) NOT NULL;
ALTER TABLE savings_goals MODIFY COLUMN account_id VARCHAR(191) NULL;

-- ──────────────────────────────────────────────────────────────────────
-- recurring_bills table
-- ──────────────────────────────────────────────────────────────────────
ALTER TABLE recurring_bills MODIFY COLUMN id INT NOT NULL;
ALTER TABLE recurring_bills MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE recurring_bills MODIFY COLUMN user_id VARCHAR(191) NOT NULL;
ALTER TABLE recurring_bills MODIFY COLUMN category_id VARCHAR(191) NULL;
ALTER TABLE recurring_bills MODIFY COLUMN account_id VARCHAR(191) NULL;

-- ──────────────────────────────────────────────────────────────────────
-- asset_holdings table
-- ──────────────────────────────────────────────────────────────────────
ALTER TABLE asset_holdings MODIFY COLUMN id INT NOT NULL;
ALTER TABLE asset_holdings MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE asset_holdings MODIFY COLUMN user_id VARCHAR(191) NOT NULL;
ALTER TABLE asset_holdings MODIFY COLUMN account_id VARCHAR(191) NOT NULL;

-- ──────────────────────────────────────────────────────────────────────
-- notifications table
-- ──────────────────────────────────────────────────────────────────────
ALTER TABLE notifications MODIFY COLUMN id INT NOT NULL;
ALTER TABLE notifications MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE notifications MODIFY COLUMN user_id VARCHAR(191) NOT NULL;

-- ──────────────────────────────────────────────────────────────────────
-- api_keys table
-- ──────────────────────────────────────────────────────────────────────
ALTER TABLE api_keys MODIFY COLUMN id INT NOT NULL;
ALTER TABLE api_keys MODIFY COLUMN id VARCHAR(191) NOT NULL;
ALTER TABLE api_keys MODIFY COLUMN user_id VARCHAR(191) NOT NULL;

-- Re-enable FK checks
SET FOREIGN_KEY_CHECKS = 1;
