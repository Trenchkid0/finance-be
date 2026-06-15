package handlers

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

type RecurringBillRequest struct {
	Name               string   `json:"name" validate:"required"`
	Amount             float64  `json:"amount" validate:"required,min=0.01"`
	AdminFee           *float64 `json:"adminFee"`
	CategoryID         *string  `json:"categoryId"`
	Frequency          string   `json:"frequency"` // "weekly", "monthly", "yearly"
	DayOfMonth         int      `json:"dayOfMonth"`
	AutoPay            bool     `json:"autoPay"`
	AccountID          *string  `json:"accountId"`
	ReminderDaysBefore *int     `json:"reminderDaysBefore"`
	ReminderTime       *string  `json:"reminderTime"`
	Note               string   `json:"note"`
}

// RecurringHandler handles listing and creating recurring bills
func RecurringHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	switch r.Method {
	case http.MethodGet:
		bills := make([]database.RecurringBill, 0)
		if err := database.DB.Preload("Category").Preload("Account").Where("user_id = ?", userID).Find(&bills).Error; err != nil {
			utils.HandleDBError(w, err, "retrieve recurring bills")
			return
		}
		utils.JSONResponse(w, http.StatusOK, bills)

	case http.MethodPost:
		var req RecurringBillRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.HandleBadRequest(w, "Invalid request body")
			return
		}

		if !middleware.ValidateAndRespond(w, req) {
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

		adminFee := 0.0
		if req.AdminFee != nil {
			adminFee = *req.AdminFee
		}

		bill := database.RecurringBill{
			ID:                 uuid.New().String(),
			UserID:             userID,
			Name:               req.Name,
			Amount:             req.Amount,
			AdminFee:           adminFee,
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
			utils.HandleDBError(w, err, "create recurring bill")
			return
		}

		// Preload Category & Account
		database.DB.Preload("Category").Preload("Account").First(&bill, "id = ?", bill.ID)

		utils.JSONResponse(w, http.StatusCreated, bill)

	default:
		utils.HandleMethodNotAllowed(w)
	}
}

// RecurringDetailHandler handles updating and deleting a specific recurring bill
func RecurringDetailHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
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

	switch r.Method {
	case http.MethodPut:
		var req RecurringBillRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.HandleBadRequest(w, "Invalid request body")
			return
		}

		if !middleware.ValidateAndRespond(w, req) {
			return
		}

		day := req.DayOfMonth
		if day < 1 || day > 31 {
			day = 1
		}

		bill.Name = req.Name
		bill.Amount = req.Amount
		if req.AdminFee != nil {
			bill.AdminFee = *req.AdminFee
		}
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
			utils.HandleDBError(w, err, "update recurring bill")
			return
		}

		// Preload relations
		database.DB.Preload("Category").Preload("Account").First(&bill, "id = ?", bill.ID)

		utils.JSONResponse(w, http.StatusOK, bill)

	case http.MethodDelete:
		if err := database.DB.Delete(&bill).Error; err != nil {
			utils.HandleDBError(w, err, "delete recurring bill")
			return
		}
		utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Recurring bill deleted successfully"})

	default:
		utils.HandleMethodNotAllowed(w)
	}
}
