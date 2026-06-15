package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

// TestTelegramHandler sends a test message to the user's registered Telegram Chat ID
func TestTelegramHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	var user database.User
	if err := database.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		utils.HandleNotFound(w, "User")
		return
	}

	if user.TelegramChatID == "" {
		utils.HandleBadRequest(w, "Telegram Chat ID belum terdaftar. Silakan kirim pesan apa saja (misal: /start) ke bot Telegram Anda terlebih dahulu.")
		return
	}

	// Read token from environment
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		token = os.Getenv("TELEGRAM_TOKEN")
	}

	if token == "" {
		utils.HandleDBError(w, nil, "TELEGRAM_BOT_TOKEN not configured")
		return
	}

	// Prepare payload for Telegram Bot API
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]interface{}{
		"chat_id":    user.TelegramChatID,
		"text":       "✅ <b>Koneksi bot Telegram berhasil terhubung!</b>\n\nAnda akan menerima notifikasi tagihan jatuh tempo otomatis di sini setiap jam 08:00 pagi.",
		"parse_mode": "HTML",
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		utils.HandleDBError(w, err, "create request payload")
		return
	}

	// Send HTTP request to Telegram
	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		utils.Log.Error().Err(err).Msg("Failed to contact Telegram API")
		utils.ErrorResponse(w, http.StatusBadGateway, "Gagal menghubungi Telegram API")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var tgErr struct {
			Description string `json:"description"`
		}
		json.NewDecoder(resp.Body).Decode(&tgErr)
		errorMsg := "Gagal mengirim pesan via Telegram."
		if tgErr.Description != "" {
			errorMsg = fmt.Sprintf("Telegram API Error: %s", tgErr.Description)
		}
		utils.Log.Error().Int("status", resp.StatusCode).Str("description", errorMsg).Msg("Telegram API error")
		utils.ErrorResponse(w, resp.StatusCode, errorMsg)
		return
	}

	utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Pesan tes berhasil dikirim!"})
}
