package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=6"`
	Name     string `json:"name" validate:"required"`
}

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// RegisterHandler handles user signup
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req RegisterRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if !middleware.ValidateAndRespond(w, req) {
		return
	}

	// Check if user already exists
	var existing database.User
	if err := database.DB.Where("email = ?", req.Email).First(&existing).Error; err == nil {
		utils.ErrorResponse(w, http.StatusConflict, "Email already registered")
		return
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to process password")
		return
	}

	user := database.User{
		ID:        uuid.New().String(),
		Email:     req.Email,
		Password:  string(hashedPassword),
		Name:      req.Name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	tx := database.DB.Begin()
	if err := tx.Create(&user).Error; err != nil {
		tx.Rollback()
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	// Seed user with default categories (equivalent to user categories setup)
	// We clone the global default categories for this user.
	var defaultCategories []database.Category
	if err := tx.Where("is_default = ? AND user_id IS NULL", true).Find(&defaultCategories).Error; err == nil {
		for _, cat := range defaultCategories {
			userCat := database.Category{
				ID:        uuid.New().String(),
				UserID:    &user.ID,
				Name:      cat.Name,
				Type:      cat.Type,
				Icon:      cat.Icon,
				Color:     cat.Color,
				IsDefault: false,
				CreatedAt: time.Now(),
			}
			if err := tx.Create(&userCat).Error; err != nil {
				tx.Rollback()
				utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to setup user categories")
				return
			}
		}
	}

	if err := tx.Commit().Error; err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to save user setup")
		return
	}

	// Generate JWT
	token, err := middleware.GenerateToken(user.ID, user.Email)
	if err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to generate session")
		return
	}

	// Set Auth Cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Set to true in HTTPS production
		SameSite: http.SameSiteLaxMode,
	})

	utils.JSONResponse(w, http.StatusCreated, map[string]interface{}{
		"id":    user.ID,
		"name":  user.Name,
		"email": user.Email,
	})
}

// LoginHandler handles user signin
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req LoginRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if !middleware.ValidateAndRespond(w, req) {
		return
	}

	var user database.User
	if err := database.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}

	// Check password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Invalid email or password")
		return
	}

	// Generate JWT
	token, err := middleware.GenerateToken(user.ID, user.Email)
	if err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to generate session")
		return
	}

	// Set Auth Cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Set to true in HTTPS production
		SameSite: http.SameSiteLaxMode,
	})

	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"id":    user.ID,
		"name":  user.Name,
		"email": user.Email,
	})
}

// LogoutHandler signs out the user by clearing the auth token cookie
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		MaxAge:   -1,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	})

	utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Logged out successfully"})
}

// MeHandler retrieves or updates the profile of the logged-in user
func MeHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var user database.User
	if err := database.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		utils.ErrorResponse(w, http.StatusNotFound, "User not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
			"id":             user.ID,
			"name":           user.Name,
			"email":          user.Email,
			"telegramChatId": user.TelegramChatID,
		})

	case http.MethodPut:
		var req struct {
			TelegramChatID string `json:"telegramChatId"`
		}
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.ErrorResponse(w, http.StatusBadRequest, "Invalid request body")
			return
		}

		user.TelegramChatID = req.TelegramChatID
		if err := database.DB.Save(&user).Error; err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to update user profile")
			return
		}

		utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
			"id":             user.ID,
			"name":           user.Name,
			"email":          user.Email,
			"telegramChatId": user.TelegramChatID,
		})

	default:
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
type ApiKeyAuth struct {
	UserID string
	Name   string
}

// APIKeyAuthHelper authenticates an incoming request via Bearer API prefix key.
// Performs sha256 hash validation against database.
func APIKeyAuthHelper(r *http.Request) (*ApiKeyAuth, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || len(authHeader) < 8 || authHeader[:7] != "Bearer " {
		return nil, errors.New("missing or invalid authorization header")
	}

	plainKey := authHeader[7:]
	hash := sha256.Sum256([]byte(plainKey))
	hashHex := hex.EncodeToString(hash[:])

	var apiKey database.ApiKey
	if err := database.DB.Where("key_hash = ? AND revoked_at IS NULL", hashHex).First(&apiKey).Error; err != nil {
		return nil, errors.New("invalid or revoked API key")
	}

	now := time.Now()
	database.DB.Model(&apiKey).Update("last_used_at", &now)

	return &ApiKeyAuth{
		UserID: apiKey.UserID,
		Name:   apiKey.Name,
	}, nil
}
