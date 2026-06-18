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
		newBalance := utils.RoundToTwoDecimals(sourceAcc.Balance + ((amount - adminFee) * multiplier))
		return tx.Model(&sourceAcc).Update("balance", newBalance).Error
	case database.TransactionTypeExpense:
		newBalance := utils.RoundToTwoDecimals(sourceAcc.Balance - ((amount + adminFee) * multiplier))
		return tx.Model(&sourceAcc).Update("balance", newBalance).Error
	case database.TransactionTypeTransfer:
		if transferToID == nil || *transferToID == "" {
			return fmt.Errorf("Akun tujuan transfer belum dipilih.")
		}
		var destAcc database.FinanceAccount
		if err := tx.Where("id = ? AND user_id = ?", *transferToID, userID).First(&destAcc).Error; err != nil {
			return err
		}
		newSrcBal := utils.RoundToTwoDecimals(sourceAcc.Balance - ((amount + adminFee) * multiplier))
		newDstBal := utils.RoundToTwoDecimals(destAcc.Balance + (amount * multiplier))
		if err := tx.Model(&sourceAcc).Update("balance", newSrcBal).Error; err != nil {
			return err
		}
		return tx.Model(&destAcc).Update("balance", newDstBal).Error
	}

	return nil
}
