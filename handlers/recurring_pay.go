package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

// RecurringPayHandler records a transaction payment for a recurring bill
func RecurringPayHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	billID := r.PathValue("id")
	if billID == "" {
		utils.HandleBadRequest(w, "Missing bill ID")
		return
	}

	var bill database.RecurringBill
	if err := database.DB.Where("id = ? AND user_id = ?", billID, userID).First(&bill).Error; err != nil {
		utils.HandleNotFound(w, "Recurring bill")
		return
	}

	if bill.AccountID == nil || *bill.AccountID == "" {
		utils.HandleBadRequest(w, "Tagihan ini belum dikaitkan dengan akun pembayaran (rekening/dompet)")
		return
	}

	tx := database.DB.Begin()

	// 1. Deduct balance
	if err := adjustBalances(tx, userID, *bill.AccountID, nil, database.TransactionTypeExpense, bill.Amount, bill.AdminFee, 1); err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "deduct balance for recurring payment")
		return
	}

	// 2. Create transaction
	transaction := database.Transaction{
		UserID:      userID,
		AccountID:   *bill.AccountID,
		CategoryID:  bill.CategoryID,
		Type:        database.TransactionTypeExpense,
		Amount:      bill.Amount,
		AdminFee:    bill.AdminFee,
		Description: fmt.Sprintf("Bayar Tagihan: %s", bill.Name),
		Note:        fmt.Sprintf("Pembayaran tagihan rutin '%s' secara manual.", bill.Name),
		Date:        time.Now(),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := tx.Create(&transaction).Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "record recurring payment transaction")
		return
	}

	// 3. Update LastPaidAt on bill
	now := time.Now()
	bill.LastPaidAt = &now
	if err := tx.Save(&bill).Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "update recurring bill paid status")
		return
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "commit recurring payment")
		return
	}

	// 4. Send Telegram message if user is connected
	var user database.User
	if err := database.DB.Where("id = ?", userID).First(&user).Error; err == nil && user.TelegramChatID != "" {
		// Preload Account & Category to display in message
		var detailedBill database.RecurringBill
		if err := database.DB.Preload("Account").Preload("Category").Where("id = ?", bill.ID).First(&detailedBill).Error; err == nil {
			accountName := "Rekening/Dompet"
			if detailedBill.Account != nil {
				accountName = detailedBill.Account.Name
			}
			categoryName := "Lain-lain"
			if detailedBill.Category != nil {
				categoryName = detailedBill.Category.Name
			}
			
			// Format Rupiah manually for pretty formatting (dot separator)
			amountVal := int64(bill.Amount)
			amountStr := strconv.FormatInt(amountVal, 10)
			var formattedAmount []rune
			for i, r := range amountStr {
				if i > 0 && (len(amountStr)-i)%3 == 0 {
					formattedAmount = append(formattedAmount, '.')
				}
				formattedAmount = append(formattedAmount, r)
			}
			
			msgText := fmt.Sprintf("💸 <b>Pembayaran Tagihan Rutin Berhasil!</b>\n\n"+
				"📝 <b>Nama Tagihan:</b> %s\n"+
				"💰 <b>Jumlah:</b> Rp %s\n"+
				"💳 <b>Sumber Dana:</b> %s\n"+
				"🏷️ <b>Kategori:</b> %s\n"+
				"📅 <b>Tanggal:</b> %s\n\n"+
				"<i>Transaksi telah otomatis tercatat di aplikasi Maybe Finance Anda.</i>",
				bill.Name, string(formattedAmount), accountName, categoryName, now.Format("02-01-2006 15:04:05"))
			
			sendTelegramMessage(user.TelegramChatID, msgText)
		}
	}

	// Invalidate cache
	_ = utils.CacheInvalidateUser(userID)

	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"message":     "Pembayaran tagihan berhasil dicatat",
		"transaction": transaction,
		"bill":        bill,
	})
}
