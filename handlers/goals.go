package handlers

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

type SavingsGoalRequest struct {
	Name          string  `json:"name" validate:"required"`
	TargetAmount  float64 `json:"targetAmount" validate:"required,min=0.01"`
	CurrentAmount float64 `json:"currentAmount"`
	TargetDate    string  `json:"targetDate"` // ISO string e.g. "2026-12-31" or RFC3339
	AccountID     *string `json:"accountId"`
	Note          string  `json:"note"`
}

// GoalsHandler handles listing and creating savings goals
func GoalsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	switch r.Method {
	case http.MethodGet:
		goals := make([]database.SavingsGoal, 0)
		if err := database.DB.Preload("Account").Where("user_id = ?", userID).Find(&goals).Error; err != nil {
			utils.HandleDBError(w, err, "retrieve goals")
			return
		}
		utils.JSONResponse(w, http.StatusOK, goals)

	case http.MethodPost:
		var req SavingsGoalRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.HandleBadRequest(w, "Invalid request body")
			return
		}

		if !middleware.ValidateAndRespond(w, req) {
			return
		}

		parsedDate := time.Now().AddDate(1, 0, 0) // default 1 year from now
		if req.TargetDate != "" {
			if parsed, err := time.Parse("2006-01-02", req.TargetDate); err == nil {
				parsedDate = parsed
			} else if parsed, err := time.Parse(time.RFC3339, req.TargetDate); err == nil {
				parsedDate = parsed
			}
		}

		goal := database.SavingsGoal{
			ID:            uuid.New().String(),
			UserID:        userID,
			Name:          req.Name,
			TargetAmount:  req.TargetAmount,
			CurrentAmount: req.CurrentAmount,
			TargetDate:    parsedDate,
			AccountID:     req.AccountID,
			Note:          req.Note,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}

		if err := database.DB.Create(&goal).Error; err != nil {
			utils.HandleDBError(w, err, "create savings goal")
			return
		}

		// Preload Account if set
		if goal.AccountID != nil && *goal.AccountID != "" {
			database.DB.Preload("Account").First(&goal, "id = ?", goal.ID)
		}

		utils.JSONResponse(w, http.StatusCreated, goal)

	default:
		utils.HandleMethodNotAllowed(w)
	}
}

// GoalDetailHandler handles updating and deleting a specific savings goal
func GoalDetailHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	goalID := r.PathValue("id")
	if goalID == "" {
		utils.HandleBadRequest(w, "Missing goal ID")
		return
	}

	var goal database.SavingsGoal
	if err := database.DB.Where("id = ? AND user_id = ?", goalID, userID).First(&goal).Error; err != nil {
		utils.HandleNotFound(w, "Savings goal")
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req SavingsGoalRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.HandleBadRequest(w, "Invalid request body")
			return
		}

		if !middleware.ValidateAndRespond(w, req) {
			return
		}

		if req.TargetDate != "" {
			if parsed, err := time.Parse("2006-01-02", req.TargetDate); err == nil {
				goal.TargetDate = parsed
			} else if parsed, err := time.Parse(time.RFC3339, req.TargetDate); err == nil {
				goal.TargetDate = parsed
			}
		}

		goal.Name = req.Name
		goal.TargetAmount = req.TargetAmount
		goal.CurrentAmount = req.CurrentAmount
		goal.AccountID = req.AccountID
		goal.Note = req.Note
		goal.UpdatedAt = time.Now()

		if err := database.DB.Save(&goal).Error; err != nil {
			utils.HandleDBError(w, err, "update savings goal")
			return
		}

		// Preload Account if set
		if goal.AccountID != nil && *goal.AccountID != "" {
			database.DB.Preload("Account").First(&goal, "id = ?", goal.ID)
		} else {
			goal.Account = nil
		}

		utils.JSONResponse(w, http.StatusOK, goal)

	case http.MethodDelete:
		if err := database.DB.Delete(&goal).Error; err != nil {
			utils.HandleDBError(w, err, "delete savings goal")
			return
		}
		utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Savings goal deleted successfully"})

	default:
		utils.HandleMethodNotAllowed(w)
	}
}
