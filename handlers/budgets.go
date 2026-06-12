package handlers

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

type BudgetRequest struct {
	CategoryID string  `json:"categoryId"`
	Limit      float64 `json:"limit"`
}

// BudgetsHandler handles listing and upserting budgets
func BudgetsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		cacheKey := utils.BuildCacheKey("budgets", userID, "list")
		var cachedResponse []database.Budget
		if err := utils.CacheGet(cacheKey, &cachedResponse); err == nil {
			utils.JSONResponse(w, http.StatusOK, cachedResponse)
			return
		}

		var budgets []database.Budget
		if err := database.DB.Where("user_id = ?", userID).Find(&budgets).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve budgets")
			return
		}
		_ = utils.CacheSet(cacheKey, budgets, 30*time.Minute)
		utils.JSONResponse(w, http.StatusOK, budgets)

	case http.MethodPost:
		var req BudgetRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		if req.CategoryID == "" || req.Limit < 0 {
			utils.ErrorResponse(w, http.StatusBadRequest, "CategoryId and non-negative Limit are required")
			return
		}

		// Idempotent: Upsert (Prisma equivalent)
		var budget database.Budget
		err := database.DB.Where("user_id = ? AND category_id = ?", userID, req.CategoryID).First(&budget).Error

		if err == nil {
			// Update existing limit
			budget.Limit = req.Limit
			budget.UpdatedAt = time.Now()
			if err := database.DB.Save(&budget).Error; err != nil {
				utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to update budget limit")
				return
			}
			_ = utils.CacheInvalidateUser(userID)
			utils.JSONResponse(w, http.StatusOK, budget)
		} else {
			// Create new limit
			budget = database.Budget{
				ID:         uuid.New().String(),
				UserID:     userID,
				CategoryID: req.CategoryID,
				Limit:      req.Limit,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			}
			if err := database.DB.Create(&budget).Error; err != nil {
				utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to create budget limit")
				return
			}
			_ = utils.CacheInvalidateUser(userID)
			utils.JSONResponse(w, http.StatusCreated, budget)
		}

	default:
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// BudgetDetailHandler handles deleting a specific budget target
func BudgetDetailHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	budgetID := r.PathValue("id")
	if budgetID == "" {
		utils.ErrorResponse(w, http.StatusBadRequest, "Missing budget ID")
		return
	}

	var budget database.Budget
	if err := database.DB.Where("id = ? AND user_id = ?", budgetID, userID).First(&budget).Error; err != nil {
		utils.ErrorResponse(w, http.StatusNotFound, "Budget limit not found")
		return
	}

	if r.Method == http.MethodDelete {
		if err := database.DB.Delete(&budget).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to delete budget limit")
			return
		}
		_ = utils.CacheInvalidateUser(userID)
		utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Budget limit deleted successfully"})
	} else {
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
