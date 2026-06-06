package handlers

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

type CategoryRequest struct {
	Name  string                `json:"name"`
	Type  database.CategoryType `json:"type"`
	Icon  string                `json:"icon"`
	Color string                `json:"color"`
}

// CategoriesHandler handles listing and creating categories
func CategoriesHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	switch r.Method {
	case http.MethodGet:
		var categories []database.Category
		// Retrieve global defaults (userId is null) OR custom user categories
		err := database.DB.Where("user_id IS NULL OR user_id = ?", userID).
			Order("is_default desc, name asc").
			Find(&categories).Error

		if err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to retrieve categories")
			return
		}
		utils.JSONResponse(w, http.StatusOK, categories)

	case http.MethodPost:
		var req CategoryRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		if req.Name == "" || req.Type == "" {
			utils.ErrorResponse(w, http.StatusBadRequest, "Name and Type are required")
			return
		}

		category := database.Category{
			ID:        uuid.New().String(),
			UserID:    &userID,
			Name:      req.Name,
			Type:      req.Type,
			Icon:      req.Icon,
			Color:     req.Color,
			IsDefault: false,
			CreatedAt: time.Now(),
		}

		if err := database.DB.Create(&category).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to create category")
			return
		}
		utils.JSONResponse(w, http.StatusCreated, category)

	default:
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
