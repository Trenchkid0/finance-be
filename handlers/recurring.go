package handlers

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"
	_ "time/tzdata"

	"github.com/google/uuid"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

type RecurringBillRequest struct {
	Name               string  `json:"name"`
	Amount             float64 `json:"amount"`
	CategoryID         *string `json:"categoryId"`
	Frequency          string  `json:"frequency"` // "weekly", "monthly", "yearly"
	DayOfMonth         int     `json:"dayOfMonth"`
	AutoPay            bool    `json:"autoPay"`
	AccountID          *string `json:"accountId"`
	ReminderDaysBefore *int    `json:"reminderDaysBefore"`
	ReminderTime       *string `json:"reminderTime"`
	Note               string  `json:"note"`
}

// RecurringHandler handles listing and creating recurring bills
func RecurringHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		bills := make([]database.RecurringBill, 0)
		if err := database.DB.Preload("Category").Preload("Account").Where("user_id = ?", userID).Find(&bills).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve recurring bills")
			return
		}
		utils.JSONResponse(w, http.StatusOK, bills)

	case http.MethodPost:
		var req RecurringBillRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		if req.Name == "" || req.Amount <= 0 {
			utils.ErrorResponse(w, http.StatusBadRequest, "Name and positive Amount are required")
			return
		}

		freq := req.Frequency
		if freq == "" {
			freq = "monthly"
		}

		day := req.DayOfMonth
		if day < 1 || day > 31 {
			day = 1
		}

		bill := database.RecurringBill{
			ID:                 uuid.New().String(),
			UserID:             userID,
			Name:               req.Name,
			Amount:             req.Amount,
			CategoryID:         req.CategoryID,
			Frequency:          freq,
			DayOfMonth:         day,
			AutoPay:            req.AutoPay,
			AccountID:          req.AccountID,
			ReminderDaysBefore: req.ReminderDaysBefore,
			ReminderTime:       req.ReminderTime,
			Note:               req.Note,
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		}

		if err := database.DB.Create(&bill).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to create recurring bill")
			return
		}

		// Preload Category & Account
		database.DB.Preload("Category").Preload("Account").First(&bill, "id = ?", bill.ID)

		utils.JSONResponse(w, http.StatusCreated, bill)

	default:
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// RecurringDetailHandler handles updating and deleting a specific recurring bill
func RecurringDetailHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	billID := r.PathValue("id")
	if billID == "" {
		utils.ErrorResponse(w, http.StatusBadRequest, "Missing bill ID")
		return
	}

	var bill database.RecurringBill
	if err := database.DB.Where("id = ? AND user_id = ?", billID, userID).First(&bill).Error; err != nil {
		utils.ErrorResponse(w, http.StatusNotFound, "Recurring bill not found")
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req RecurringBillRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		if req.Name == "" || req.Amount <= 0 {
			utils.ErrorResponse(w, http.StatusBadRequest, "Name and positive Amount are required")
			return
		}

		day := req.DayOfMonth
		if day < 1 || day > 31 {
			day = 1
		}

		bill.Name = req.Name
		bill.Amount = req.Amount
		bill.CategoryID = req.CategoryID
		bill.Frequency = req.Frequency
		bill.DayOfMonth = day
		bill.AutoPay = req.AutoPay
		bill.AccountID = req.AccountID
		// Reset LastRemindedAt if reminder settings are toggled or modified
		settingsChanged := false
		if (bill.ReminderDaysBefore == nil && req.ReminderDaysBefore != nil) ||
			(bill.ReminderDaysBefore != nil && req.ReminderDaysBefore == nil) ||
			(bill.ReminderDaysBefore != nil && req.ReminderDaysBefore != nil && *bill.ReminderDaysBefore != *req.ReminderDaysBefore) {
			settingsChanged = true
		}
		if (bill.ReminderTime == nil && req.ReminderTime != nil) ||
			(bill.ReminderTime != nil && req.ReminderTime == nil) ||
			(bill.ReminderTime != nil && req.ReminderTime != nil && *bill.ReminderTime != *req.ReminderTime) {
			settingsChanged = true
		}
		if settingsChanged {
			bill.LastRemindedAt = nil
		}

		bill.ReminderDaysBefore = req.ReminderDaysBefore
		bill.ReminderTime = req.ReminderTime
		bill.Note = req.Note
		bill.UpdatedAt = time.Now()

		if err := database.DB.Save(&bill).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to update recurring bill")
			return
		}

		// Preload relations
		database.DB.Preload("Category").Preload("Account").First(&bill, "id = ?", bill.ID)

		utils.JSONResponse(w, http.StatusOK, bill)

	case http.MethodDelete:
		if err := database.DB.Delete(&bill).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to delete recurring bill")
			return
		}
		utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Recurring bill deleted successfully"})

	default:
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// RecurringPayHandler records a transaction payment for a recurring bill
func RecurringPayHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if r.Method != http.MethodPost {
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	billID := r.PathValue("id")
	if billID == "" {
		utils.ErrorResponse(w, http.StatusBadRequest, "Missing bill ID")
		return
	}

	var bill database.RecurringBill
	if err := database.DB.Where("id = ? AND user_id = ?", billID, userID).First(&bill).Error; err != nil {
		utils.ErrorResponse(w, http.StatusNotFound, "Recurring bill not found")
		return
	}

	if bill.AccountID == nil || *bill.AccountID == "" {
		utils.ErrorResponse(w, http.StatusBadRequest, "Tagihan ini belum dikaitkan dengan akun pembayaran (rekening/dompet)")
		return
	}

	tx := database.DB.Begin()

	// 1. Deduct balance
	if err := adjustBalances(tx, userID, *bill.AccountID, nil, database.TransactionTypeExpense, bill.Amount, 1); err != nil {
		tx.Rollback()
		utils.ErrorResponse(w, http.StatusInternalServerError, "Gagal memotong saldo rekening")
		return
	}

	// 2. Create transaction
	transaction := database.Transaction{
		UserID:      userID,
		AccountID:   *bill.AccountID,
		CategoryID:  bill.CategoryID,
		Type:        database.TransactionTypeExpense,
		Amount:      bill.Amount,
		Description: fmt.Sprintf("Bayar Tagihan: %s", bill.Name),
		Note:        fmt.Sprintf("Pembayaran tagihan rutin '%s' secara manual.", bill.Name),
		Date:        time.Now(),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := tx.Create(&transaction).Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(w, http.StatusInternalServerError, "Gagal mencatat transaksi pembayaran")
		return
	}

	// 3. Update LastPaidAt on bill
	now := time.Now()
	bill.LastPaidAt = &now
	if err := tx.Save(&bill).Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(w, http.StatusInternalServerError, "Gagal memperbarui status tagihan")
		return
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(w, http.StatusInternalServerError, "Gagal menyimpan perubahan transaksi")
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

// CheckReminderBills checks all recurring bills and sends Telegram reminders if due
func CheckReminderBills() {
	db := database.DB
	if db == nil {
		return
	}

	// Fetch all bills with reminders set
	var bills []database.RecurringBill
	err := db.Where("reminder_days_before IS NOT NULL").Find(&bills).Error
	if err != nil {
		log.Printf("⚠️ [Reminder] Failed to fetch reminder bills: %v", err)
		return
	}

	// Load Asia/Jakarta timezone (GMT+7)
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		log.Printf("⚠️ [Reminder] Failed to load Asia/Jakarta timezone, falling back to Local: %v", err)
		loc = time.Local
	}
	now := time.Now().In(loc)

	log.Printf("⏰ [Reminder] Starting check for %d active bills. Current server time (in Asia/Jakarta): %v", len(bills), now.Format("02-01-2006 15:04:05 MST"))

	for _, bill := range bills {
		// Find user to get Telegram Chat ID
		var user database.User
		if err := db.Where("id = ?", bill.UserID).First(&user).Error; err != nil {
			log.Printf("🔍 [Reminder] User not found for bill '%s': %v", bill.Name, err)
			continue
		}

		if user.TelegramChatID == "" {
			log.Printf("🔍 [Reminder] Skip bill '%s': User %s has no Telegram Chat ID", bill.Name, user.Email)
			continue
		}

		// Calculate next due date
		dueDate := getNextDueDate(bill, now)

		// Calculate when the reminder should be sent
		daysBefore := 0
		if bill.ReminderDaysBefore != nil {
			daysBefore = *bill.ReminderDaysBefore
		}

		reminderDate := dueDate.AddDate(0, 0, -daysBefore)

		// Default reminder time is 09:00
		remHour, remMin := 9, 0
		if bill.ReminderTime != nil && *bill.ReminderTime != "" {
			fmt.Sscanf(*bill.ReminderTime, "%d:%d", &remHour, &remMin)
		}

		reminderDateTime := time.Date(
			reminderDate.Year(), reminderDate.Month(), reminderDate.Day(),
			remHour, remMin, 0, 0, loc,
		)

		log.Printf("🔍 [Reminder] Bill '%s': DueDate=%s, TargetReminderTime=%s", 
			bill.Name, dueDate.Format("02-01-2006"), reminderDateTime.Format("02-01-2006 15:04:05 MST"))

		// Check conditions
		timePassed := now.After(reminderDateTime)
		
		var alreadyReminded bool
		if bill.LastRemindedAt != nil {
			lastRemindedLocal := bill.LastRemindedAt.In(loc)
			alreadyReminded = !lastRemindedLocal.Before(reminderDateTime)
			log.Printf("🔍 [Reminder] Bill '%s': LastRemindedAt=%s, AlreadyReminded=%t (BeforeTarget=%t)", 
				bill.Name, lastRemindedLocal.Format("02-01-2006 15:04:05 MST"), alreadyReminded, lastRemindedLocal.Before(reminderDateTime))
		} else {
			log.Printf("🔍 [Reminder] Bill '%s': LastRemindedAt=nil", bill.Name)
		}

		if timePassed && !alreadyReminded {
			// Preload Category & Account for rich message
			var detailedBill database.RecurringBill
			db.Preload("Account").Preload("Category").Where("id = ?", bill.ID).First(&detailedBill)

			accountName := "Rekening/Dompet"
			if detailedBill.Account != nil {
				accountName = detailedBill.Account.Name
			}
			categoryName := "Lain-lain"
			if detailedBill.Category != nil {
				categoryName = detailedBill.Category.Name
			}

			// Format Rupiah
			amountVal := int64(bill.Amount)
			amountStr := strconv.FormatInt(amountVal, 10)
			var formattedAmount []rune
			for i, r := range amountStr {
				if i > 0 && (len(amountStr)-i)%3 == 0 {
					formattedAmount = append(formattedAmount, '.')
				}
				formattedAmount = append(formattedAmount, r)
			}

			// Format days remaining string
			daysRemaining := int(dueDate.Sub(now).Hours() / 24)
			var dueStr string
			if daysRemaining == 0 {
				dueStr = "HARI INI!"
			} else if daysRemaining < 0 {
				dueStr = fmt.Sprintf("telah lewat %d hari lalu!", -daysRemaining)
			} else {
				dueStr = fmt.Sprintf("dalam %d hari lagi (%s)", daysRemaining, dueDate.Format("02-01-2006"))
			}

			message := fmt.Sprintf("⚠️ <b>Pengingat Tagihan: %s</b>\n\n"+
				"📅 <b>Jatuh Tempo:</b> %s\n"+
				"💰 <b>Jumlah:</b> Rp %s\n"+
				"💳 <b>Sumber Dana:</b> %s\n"+
				"🏷️ <b>Kategori:</b> %s\n\n"+
				"<i>Silakan lakukan pembayaran melalui aplikasi Maybe Finance.</i>",
				bill.Name, dueStr, string(formattedAmount), accountName, categoryName)

			log.Printf("📣 [Reminder] Dispatching Telegram message to %s for bill '%s'", user.TelegramChatID, bill.Name)
			sendTelegramMessage(user.TelegramChatID, message)

			// Update LastRemindedAt
			bill.LastRemindedAt = &now
			db.Save(&bill)
			log.Printf("⏰ [Reminder] Successfully updated LastRemindedAt for bill '%s'", bill.Name)
		} else {
			log.Printf("🔍 [Reminder] Skip bill '%s': TimePassed=%t, AlreadyReminded=%t", bill.Name, timePassed, alreadyReminded)
		}
	}
}

func getNextDueDate(bill database.RecurringBill, from time.Time) time.Time {
	year, month, _ := from.Date()
	day := bill.DayOfMonth
	if day < 1 || day > 31 {
		day = 1
	}

	dueDate := time.Date(year, month, day, 0, 0, 0, 0, from.Location())
	// Adjust end of month
	if dueDate.Month() != month {
		dueDate = time.Date(year, month+1, 0, 0, 0, 0, 0, from.Location())
	}

	// If paid recently for this cycle
	if bill.LastPaidAt != nil {
		paidLocal := bill.LastPaidAt.In(from.Location())
		if paidLocal.Year() == year && paidLocal.Month() == month {
			nextMonth := month + 1
			nextYear := year
			if nextMonth > 12 {
				nextMonth = 1
				nextYear++
			}
			dueDate = time.Date(nextYear, nextMonth, day, 0, 0, 0, 0, from.Location())
			if dueDate.Month() != nextMonth {
				dueDate = time.Date(nextYear, nextMonth+1, 0, 0, 0, 0, 0, from.Location())
			}
		}
	}

	return dueDate
}
