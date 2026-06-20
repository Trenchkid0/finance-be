package handlers

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/services"
	"maybe-finance-backend/utils"
)

func handlePhotoUpload(msg *TelegramMessage, user *database.User) {
	chatID := strconv.FormatInt(msg.Chat.ID, 10)

	// Get the largest photo size (last in array)
	largestPhoto := msg.Photo[len(msg.Photo)-1]

	// Download and convert to WebP
	imageURL, err := downloadAndConvertTelegramPhoto(largestPhoto.FileID, user.ID)
	if err != nil {
		sendTelegramMessage(chatID, "❌ Gagal memproses gambar. Silakan coba lagi.")
		return
	}

	// Parse caption for transaction details
	amount, description, categoryID := parseCaption(msg.Caption, user.ID)

	// Find default account for user
	var account database.FinanceAccount
	if err := database.DB.Where("user_id = ?", user.ID).Order("created_at ASC").First(&account).Error; err != nil {
		sendTelegramMessage(chatID, "❌ Anda belum memiliki akun keuangan. Silakan buat akun terlebih dahulu di aplikasi.")
		return
	}

	// Begin DB transaction to ensure balance consistency
	tx := database.DB.Begin()

	// Reconcile Balance (Telegram bot is expense by default)
	if err := services.AdjustBalances(tx, user.ID, account.ID, nil, database.TransactionTypeExpense, amount, 0, 1); err != nil {
		tx.Rollback()
		sendTelegramMessage(chatID, "❌ Gagal memperbarui saldo akun.")
		return
	}

	// Create transaction
	transaction := database.Transaction{
		UserID:          user.ID,
		AccountID:       account.ID,
		CategoryID:      categoryID,
		Type:            database.TransactionTypeExpense,
		Amount:          amount,
		Description:     description,
		Note:            "Dicatat via Telegram Bot",
		Date:            time.Now(),
		ReceiptImageURL: &imageURL,
	}

	if err := tx.Create(&transaction).Error; err != nil {
		tx.Rollback()
		sendTelegramMessage(chatID, "❌ Gagal menyimpan transaksi.")
		return
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		sendTelegramMessage(chatID, "❌ Gagal menyimpan transaksi.")
		return
	}

	// Invalidate related caches
	_ = utils.CacheInvalidateUser(user.ID)

	// Send success message
	message := fmt.Sprintf("✅ Transaksi tercatat!\n\n💰 Jumlah: Rp %.0f\n📝 Deskripsi: %s\n📷 Struk: Tersimpan\n\nLihat detail di aplikasi Maybe Finance.",
		amount, description)
	sendTelegramMessage(chatID, message)
}

func handleTextTransaction(msg *TelegramMessage, user *database.User) {
	chatID := strconv.FormatInt(msg.Chat.ID, 10)
	text := strings.TrimSpace(msg.Text)

	// Try to parse amount from text (first number found)
	amount, description := parseAmountFromText(text)
	if amount <= 0 {
		sendTelegramMessage(chatID, "⚠️ Format tidak dikenali.\n\nKirim foto struk untuk mencatat transaksi, atau ketik jumlah dan deskripsi (contoh: \"50000 Makan siang\").")
		return
	}

	// Find default account
	var account database.FinanceAccount
	if err := database.DB.Where("user_id = ?", user.ID).Order("created_at ASC").First(&account).Error; err != nil {
		sendTelegramMessage(chatID, "❌ Anda belum memiliki akun keuangan.")
		return
	}

	// Begin DB transaction to ensure balance consistency
	tx := database.DB.Begin()

	// Reconcile Balance (Telegram bot is expense by default)
	if err := services.AdjustBalances(tx, user.ID, account.ID, nil, database.TransactionTypeExpense, amount, 0, 1); err != nil {
		tx.Rollback()
		sendTelegramMessage(chatID, "❌ Gagal memperbarui saldo akun.")
		return
	}

	// Create transaction
	transaction := database.Transaction{
		UserID:      user.ID,
		AccountID:   account.ID,
		Type:        database.TransactionTypeExpense,
		Amount:      amount,
		Description: description,
		Note:        "Dicatat via Telegram Bot",
		Date:        time.Now(),
	}

	if err := tx.Create(&transaction).Error; err != nil {
		tx.Rollback()
		sendTelegramMessage(chatID, "❌ Gagal menyimpan transaksi.")
		return
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		sendTelegramMessage(chatID, "❌ Gagal menyimpan transaksi.")
		return
	}

	// Invalidate related caches
	_ = utils.CacheInvalidateUser(user.ID)

	message := fmt.Sprintf("✅ Transaksi tercatat!\n\n💰 Jumlah: Rp %.0f\n📝 Deskripsi: %s\n\nLihat detail di aplikasi Maybe Finance.",
		amount, description)
	sendTelegramMessage(chatID, message)
}

func parseCaption(caption, userID string) (float64, string, *string) {
	if caption == "" {
		return 0, "Transaksi dari Telegram", nil
	}

	amount, description := parseAmountFromText(caption)

	// Try to find category from caption
	var category database.Category
	if err := database.DB.Where("user_id = ? AND name LIKE ?", userID, "%"+description+"%").First(&category).Error; err == nil {
		return amount, description, &category.ID
	}

	return amount, description, nil
}

func parseAmountFromText(text string) (float64, string) {
	// Remove common currency symbols and separators
	text = strings.ReplaceAll(text, ".", "")
	text = strings.ReplaceAll(text, ",", "")
	text = strings.TrimSpace(text)

	words := strings.Fields(text)
	if len(words) == 0 {
		return 0, ""
	}

	// Try to parse first word as amount
	amount, err := strconv.ParseFloat(words[0], 64)
	if err != nil {
		// Try finding number anywhere in text
		for _, word := range words {
			if amt, err := strconv.ParseFloat(word, 64); err == nil {
				amount = amt
				// Get remaining text as description
				idx := strings.Index(text, word) + len(word)
				description := strings.TrimSpace(text[idx:])
				return amount, description
			}
		}
		return 0, text
	}

	// Get remaining text as description
	description := strings.Join(words[1:], " ")
	if description == "" {
		description = "Transaksi dari Telegram"
	}

	return amount, description
}
