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
	Name          string                `json:"name" validate:"required"`
	Type          database.CategoryType `json:"type" validate:"required,oneof=income|expense"`
	Icon          string                `json:"icon"`
	Color         string                `json:"color"`
	TaxDeductible bool                  `json:"taxDeductible"`
}

// CategoriesHandler handles listing and creating categories
func CategoriesHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	switch r.Method {
	case http.MethodGet:
		cacheKey := utils.BuildCacheKey("categories", userID, "list")
		var cachedResponse []database.Category
		if err := utils.CacheGet(cacheKey, &cachedResponse); err == nil {
			utils.JSONResponse(w, http.StatusOK, cachedResponse)
			return
		}

		var categories []database.Category
		// Retrieve global defaults (userId is null) OR custom user categories
		err := database.DB.Where("user_id IS NULL OR user_id = ?", userID).
			Order("is_default desc, name asc").
			Find(&categories).Error

		if err != nil {
			utils.HandleDBError(w, err, "retrieve categories")
			return
		}
		_ = utils.CacheSet(cacheKey, categories, 30*time.Minute)
		utils.JSONResponse(w, http.StatusOK, categories)

	case http.MethodPost:
		var req CategoryRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.HandleBadRequest(w, "Invalid request body")
			return
		}

		if !middleware.ValidateAndRespond(w, req) {
			return
		}

		category := database.Category{
			ID:            uuid.New().String(),
			UserID:        &userID,
			Name:          req.Name,
			Type:          req.Type,
			Icon:          req.Icon,
			Color:         req.Color,
			IsDefault:     false,
			TaxDeductible: req.TaxDeductible,
			CreatedAt:     time.Now(),
		}

		if err := database.DB.Create(&category).Error; err != nil {
			utils.HandleDBError(w, err, "create category")
			return
		}
		_ = utils.CacheInvalidateUser(userID)
		utils.JSONResponse(w, http.StatusCreated, category)

	default:
		utils.HandleMethodNotAllowed(w)
	}
}

// CategoryDetailHandler handles updating or deleting custom categories
func CategoryDetailHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	catID := r.PathValue("id")
	if catID == "" {
		utils.HandleBadRequest(w, "ID kategori tidak ditemukan.")
		return
	}

	var category database.Category
	// Only allow managing custom categories belonging to this user
	if err := database.DB.Where("id = ? AND user_id = ?", catID, userID).First(&category).Error; err != nil {
		utils.HandleNotFound(w, "Kategori")
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req CategoryRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.HandleBadRequest(w, "Format data tidak valid, silakan periksa input Anda.")
			return
		}

		if !middleware.ValidateAndRespond(w, req) {
			return
		}

		category.Name = req.Name
		category.Type = req.Type
		category.Icon = req.Icon
		category.Color = req.Color
		category.TaxDeductible = req.TaxDeductible

		if err := database.DB.Save(&category).Error; err != nil {
			utils.HandleDBError(w, err, "update category")
			return
		}

		_ = utils.CacheInvalidateUser(userID)
		utils.JSONResponse(w, http.StatusOK, category)

	case http.MethodDelete:
		// Delete custom category
		if err := database.DB.Delete(&category).Error; err != nil {
			utils.HandleDBError(w, err, "delete category")
			return
		}

		_ = utils.CacheInvalidateUser(userID)
		utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Kategori berhasil dihapus"})

	default:
		utils.HandleMethodNotAllowed(w)
	}
}
