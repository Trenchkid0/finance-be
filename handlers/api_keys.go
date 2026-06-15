package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/google/uuid"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

type ApiKeyCreateRequest struct {
	Name string `json:"name"`
}

// ApiKeysHandler lists and creates API keys
func ApiKeysHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	switch r.Method {
	case http.MethodGet:
		var apiKeys []database.ApiKey
		// Only select non-revoked ones
		if err := database.DB.Where("user_id = ? AND revoked_at IS NULL", userID).Find(&apiKeys).Error; err != nil {
			utils.HandleDBError(w, err, "retrieve API keys")
			return
		}
		utils.JSONResponse(w, http.StatusOK, apiKeys)

	case http.MethodPost:
		var req ApiKeyCreateRequest
		if err := utils.ParseJSON(r, &req); err != nil {
			utils.HandleBadRequest(w, "Invalid request body")
			return
		}

		if req.Name == "" {
			utils.HandleBadRequest(w, "API Key Name is required")
			return
		}

		// Generate a long random secret
		plainSecret := "mb_" + uuid.New().String() + uuid.New().String()
		plainSecret = plainSecret[:64] // Keep it standard length (64 chars)


		// Hash with SHA-256
		hash := sha256.Sum256([]byte(plainSecret))
		hashHex := hex.EncodeToString(hash[:])

		apiKey := database.ApiKey{
			ID:        uuid.New().String(),
			UserID:    userID,
			Name:      req.Name,
			KeyPrefix: plainSecret,
			KeyHash:   hashHex,
			CreatedAt: time.Now(),
		}

		if err := database.DB.Create(&apiKey).Error; err != nil {
			utils.HandleDBError(w, err, "create API key record")
			return
		}

		// Return both metadata AND the plain key (ONLY ONCE)
		utils.JSONResponse(w, http.StatusCreated, map[string]interface{}{
			"id":        apiKey.ID,
			"name":      apiKey.Name,
			"keyPrefix": apiKey.KeyPrefix,
			"plainKey":  plainSecret, // Placed only once for display
			"createdAt": apiKey.CreatedAt,
		})

	default:
		utils.HandleMethodNotAllowed(w)
	}
}

// ApiKeyDetailHandler handles revoking (deleting) a single API key
func ApiKeyDetailHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	keyID := r.PathValue("id")
	if keyID == "" {
		utils.HandleBadRequest(w, "Missing API key ID")
		return
	}

	var apiKey database.ApiKey
	if err := database.DB.Where("id = ? AND user_id = ?", keyID, userID).First(&apiKey).Error; err != nil {
		utils.HandleNotFound(w, "API key")
		return
	}

	if r.Method == http.MethodDelete {
		now := time.Now()
		apiKey.RevokedAt = &now
		if err := database.DB.Save(&apiKey).Error; err != nil {
			utils.HandleDBError(w, err, "revoke API key")
			return
		}
		utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "API key revoked successfully"})
	} else {
		utils.HandleMethodNotAllowed(w)
	}
}
