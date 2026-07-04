package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
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
	Remember bool   `json:"remember"`
}

// RegisterHandler handles user signup
func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	var req RegisterRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.HandleBadRequest(w, "Format data tidak valid, silakan periksa input Anda.")
		return
	}

	if !middleware.ValidateAndRespond(w, req) {
		return
	}

	// Block registration if setup is already complete (at least one user exists)
	var count int64
	if err := database.DB.Model(&database.User{}).Count(&count).Error; err != nil {
		utils.HandleDBError(w, err, "check user setup status")
		return
	}
	if count > 0 {
		utils.ErrorResponse(w, http.StatusForbidden, "Setup has already been completed. Registration is disabled.")
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
		utils.HandleDBError(w, err, "process password")
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
		utils.HandleDBError(w, err, "create user")
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
				utils.HandleDBError(w, err, "setup user categories")
				return
			}
		}
	}

	if err := tx.Commit().Error; err != nil {
		utils.HandleDBError(w, err, "save user setup")
		return
	}

	// Generate JWT
	token, err := middleware.GenerateToken(user.ID, user.Email)
	if err != nil {
		utils.HandleDBError(w, err, "generate session")
		return
	}

	// Set Auth Cookie
	appEnv := strings.ToLower(os.Getenv("APP_ENV"))
	isSecure := appEnv == "production" || appEnv == "prod"

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Expires:  time.Now().Add(7 * 24 * time.Hour),
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
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
		utils.HandleMethodNotAllowed(w)
		return
	}

	var req LoginRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.HandleBadRequest(w, "Format data tidak valid, silakan periksa input Anda.")
		return
	}

	if !middleware.ValidateAndRespond(w, req) {
		return
	}

	var user database.User
	if err := database.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
		utils.HandleUnauthorized(w, "Invalid email or password")
		return
	}

	// Check password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		utils.HandleUnauthorized(w, "Invalid email or password")
		return
	}

	// Generate JWT
	token, err := middleware.GenerateToken(user.ID, user.Email)
	if err != nil {
		utils.HandleDBError(w, err, "generate session")
		return
	}

	// Set Auth Cookie
	appEnv := strings.ToLower(os.Getenv("APP_ENV"))
	isSecure := appEnv == "production" || appEnv == "prod"

	cookie := &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
	}

	if req.Remember {
		cookie.Expires = time.Now().Add(30 * 24 * time.Hour)
	}

	http.SetCookie(w, cookie)

	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"id":    user.ID,
		"name":  user.Name,
		"email": user.Email,
	})
}

// LogoutHandler signs out the user by clearing the auth token cookie
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	// Match the same Secure flag logic used in Login/Register
	appEnv := strings.ToLower(os.Getenv("APP_ENV"))
	isSecure := appEnv == "production" || appEnv == "prod"

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		MaxAge:   -1,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: http.SameSiteLaxMode,
	})

	utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Logged out successfully"})
}

// MeHandler retrieves or updates the profile of the logged-in user
func MeHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	var user database.User
	if err := database.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		utils.HandleNotFound(w, "Pengguna")
		return
	}

	switch r.Method {
	case http.MethodGet:
		utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
			"id":             user.ID,
			"name":           user.Name,
			"email":          user.Email,
			"image":          user.Image,
			"telegramChatId": user.TelegramChatID,
			"createdAt":      user.CreatedAt.Format(time.RFC3339),
		})

	case http.MethodPut:
		var req struct {
			Name           string `json:"name"`
			TelegramChatID string `json:"telegramChatId"`
			Image          string `json:"image"`
		}
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.HandleBadRequest(w, "Format data tidak valid, silakan periksa input Anda.")
			return
		}

		if req.Name != "" {
			user.Name = req.Name
		}
		user.TelegramChatID = req.TelegramChatID
		user.Image = req.Image
		user.UpdatedAt = time.Now()

		if err := database.DB.Save(&user).Error; err != nil {
			utils.HandleDBError(w, err, "update user profile")
			return
		}

		// Invalidate cache
		_ = utils.CacheInvalidateUser(user.ID)

		utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
			"id":             user.ID,
			"name":           user.Name,
			"email":          user.Email,
			"image":          user.Image,
			"telegramChatId": user.TelegramChatID,
		})

	default:
		utils.HandleMethodNotAllowed(w)
	}
}


type ForgotPasswordRequest struct {
	Email string `json:"email" validate:"required,email"`
}

func ForgotPasswordHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	var req ForgotPasswordRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.HandleBadRequest(w, "Format data tidak valid, silakan periksa input Anda.")
		return
	}

	if !middleware.ValidateAndRespond(w, req) {
		return
	}

	var user database.User
	if err := database.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
		utils.ErrorResponse(w, http.StatusNotFound, "Email not registered")
		return
	}

	// Generate reset token
	token := uuid.New().String()
	
	// Store hash of token in DB
	hash := sha256.Sum256([]byte(token))
	hashHex := hex.EncodeToString(hash[:])
	
	expires := time.Now().Add(1 * time.Hour)
	
	user.ResetToken = hashHex
	user.ResetTokenExpires = &expires
	if err := database.DB.Save(&user).Error; err != nil {
		utils.HandleDBError(w, err, "save reset token")
		return
	}

	// Log the reset URL for simulated debugging purposes (dev mode)
	utils.Log.Info().Str("email", req.Email).Str("token", token).Msg("Simulating sending password reset email")

	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Reset link sent successfully",
	})
}
type ResetPasswordRequest struct {
	Token    string `json:"token" validate:"required"`
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

func ResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	var req ResetPasswordRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.HandleBadRequest(w, "Format data tidak valid, silakan periksa input Anda.")
		return
	}

	if !middleware.ValidateAndRespond(w, req) {
		return
	}

	var user database.User
	if err := database.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
		utils.ErrorResponse(w, http.StatusNotFound, "Email tidak terdaftar")
		return
	}

	if user.ResetToken == "" || user.ResetTokenExpires == nil || user.ResetTokenExpires.Before(time.Now()) {
		utils.ErrorResponse(w, http.StatusBadRequest, "Token reset password tidak valid atau sudah kedaluwarsa")
		return
	}

	// Verify token hash
	hash := sha256.Sum256([]byte(req.Token))
	hashHex := hex.EncodeToString(hash[:])
	
	if user.ResetToken != hashHex {
		utils.ErrorResponse(w, http.StatusBadRequest, "Token reset password tidak valid")
		return
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		utils.HandleDBError(w, err, "process password")
		return
	}

	user.Password = string(hashedPassword)
	user.ResetToken = ""
	user.ResetTokenExpires = nil

	if err := database.DB.Save(&user).Error; err != nil {
		utils.HandleDBError(w, err, "update password")
		return
	}

	utils.JSONResponse(w, http.StatusOK, map[string]string{
		"message": "Kata sandi berhasil diperbarui",
	})
}

type SetupStatusResponse struct {
	IsSetupComplete bool `json:"isSetupComplete"`
}

// SetupStatusHandler checks if there are any registered users in the database
func SetupStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}

	var count int64
	if err := database.DB.Model(&database.User{}).Count(&count).Error; err != nil {
		utils.HandleDBError(w, err, "check user setup status")
		return
	}

	utils.JSONResponse(w, http.StatusOK, SetupStatusResponse{
		IsSetupComplete: count > 0,
	})
}
