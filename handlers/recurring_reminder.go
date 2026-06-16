package handlers

import (
	"fmt"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/utils"
)

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
		utils.Log.Error().Err(err).Msg("[Reminder] Failed to fetch reminder bills")
		return
	}

	// Load Asia/Jakarta timezone (GMT+7)
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		utils.Log.Warn().Err(err).Msg("[Reminder] Failed to load Asia/Jakarta timezone, falling back to Local")
		loc = time.Local
	}
	now := time.Now().In(loc)

	utils.Log.Info().Int("bill_count", len(bills)).Str("server_time", now.Format("02-01-2006 15:04:05 MST")).Msg("[Reminder] Starting check for active bills")

	for _, bill := range bills {
		// Find user to get Telegram Chat ID
		var user database.User
		if err := db.Where("id = ?", bill.UserID).First(&user).Error; err != nil {
			utils.Log.Warn().Err(err).Str("bill", bill.Name).Msg("[Reminder] User not found")
			continue
		}

		if user.TelegramChatID == "" {
			utils.Log.Debug().Str("bill", bill.Name).Str("email", user.Email).Msg("[Reminder] Skip bill: user has no Telegram Chat ID")
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

		utils.Log.Debug().Str("bill", bill.Name).Str("due_date", dueDate.Format("02-01-2006")).Str("reminder_time", reminderDateTime.Format("02-01-2006 15:04:05 MST")).Msg("[Reminder] Bill dates computed")

		// Check conditions
		timePassed := now.After(reminderDateTime)
		
		var alreadyReminded bool
		if bill.LastRemindedAt != nil {
			lastRemindedLocal := bill.LastRemindedAt.In(loc)
			alreadyReminded = !lastRemindedLocal.Before(reminderDateTime)
			utils.Log.Debug().Str("bill", bill.Name).Str("last_reminded", lastRemindedLocal.Format("02-01-2006 15:04:05 MST")).Bool("already_reminded", alreadyReminded).Bool("before_target", lastRemindedLocal.Before(reminderDateTime)).Msg("[Reminder] Reminder status check")
		} else {
			utils.Log.Debug().Str("bill", bill.Name).Msg("[Reminder] LastRemindedAt is nil")
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
				bill.Name, dueStr, utils.FormatRupiah(bill.Amount), accountName, categoryName)

			utils.Log.Info().Str("chat_id", user.TelegramChatID).Str("bill", bill.Name).Msg("[Reminder] Dispatching Telegram message")
			sendTelegramMessage(user.TelegramChatID, message)

			// Create in-app notification
			_ = CreateNotificationHelper(bill.UserID, "Reminder: "+bill.Name, fmt.Sprintf("Your recurring bill '%s' is due %s. Amount: %s.", bill.Name, dueStr, utils.FormatRupiah(bill.Amount)), "bill")

			// Update LastRemindedAt
			bill.LastRemindedAt = &now
			db.Save(&bill)
			utils.Log.Info().Str("bill", bill.Name).Msg("[Reminder] Successfully updated LastRemindedAt")
		} else {
			utils.Log.Debug().Str("bill", bill.Name).Bool("time_passed", timePassed).Bool("already_reminded", alreadyReminded).Msg("[Reminder] Skip bill: conditions not met")
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
