package services

import (
	"fmt"
	"gorm.io/gorm"
	"maybe-finance-backend/database"
	"maybe-finance-backend/utils"
)

// AdjustBalances shifts the balance of accounts when transactions are deleted, created or changed.
// multiplier: +1 to apply transaction, -1 to roll back transaction
func AdjustBalances(tx *gorm.DB, userID string, accountID string, transferToID *string, txType database.TransactionType, amount float64, adminFee float64, multiplier float64) error {
	var sourceAcc database.FinanceAccount
	if err := tx.Where("id = ? AND user_id = ?", accountID, userID).First(&sourceAcc).Error; err != nil {
		return err
	}

	switch txType {
	case database.TransactionTypeIncome:
		sourceAcc.Balance = utils.RoundToTwoDecimals(sourceAcc.Balance + ((amount - adminFee) * multiplier))
		return tx.Save(&sourceAcc).Error
	case database.TransactionTypeExpense:
		sourceAcc.Balance = utils.RoundToTwoDecimals(sourceAcc.Balance - ((amount + adminFee) * multiplier))
		return tx.Save(&sourceAcc).Error
	case database.TransactionTypeTransfer:
		if transferToID == nil || *transferToID == "" {
			return fmt.Errorf("Akun tujuan transfer belum dipilih.")
		}
		var destAcc database.FinanceAccount
		if err := tx.Where("id = ? AND user_id = ?", *transferToID, userID).First(&destAcc).Error; err != nil {
			return err
		}
		sourceAcc.Balance = utils.RoundToTwoDecimals(sourceAcc.Balance - ((amount + adminFee) * multiplier))
		destAcc.Balance = utils.RoundToTwoDecimals(destAcc.Balance + (amount * multiplier))
		if err := tx.Save(&sourceAcc).Error; err != nil {
			return err
		}
		return tx.Save(&destAcc).Error
	}

	return nil
}
