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
	Name       string  `json:"name"`
	Amount     float64 `json:"amount"`
	CategoryID *string `json:"categoryId"`
	Frequency  string  `json:"frequency"` // "weekly", "monthly", "yearly"
	DayOfMonth int     `json:"dayOfMonth"`
	Note       string  `json:"note"`
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
		if err := database.DB.Preload("Category").Where("user_id = ?", userID).Find(&bills).Error; err != nil {
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
			ID:         uuid.New().String(),
			UserID:     userID,
			Name:       req.Name,
			Amount:     req.Amount,
			CategoryID: req.CategoryID,
			Frequency:  freq,
			DayOfMonth: day,
			Note:       req.Note,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		if err := database.DB.Create(&bill).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to create recurring bill")
			return
		}

		// Preload Category if set
		if bill.CategoryID != nil && *bill.CategoryID != "" {
			database.DB.Preload("Category").First(&bill, "id = ?", bill.ID)
		}

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
		bill.Note = req.Note
		bill.UpdatedAt = time.Now()

		if err := database.DB.Save(&bill).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to update recurring bill")
			return
		}

		// Preload Category if set
		if bill.CategoryID != nil && *bill.CategoryID != "" {
			database.DB.Preload("Category").First(&bill, "id = ?", bill.ID)
		} else {
			bill.Category = nil
		}

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
