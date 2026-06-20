package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	xwebp "golang.org/x/image/webp"

	"maybe-finance-backend/database"
	"maybe-finance-backend/utils"
)

// TelegramUpdate represents the webhook payload from Telegram
type TelegramUpdate struct {
	UpdateID int64            `json:"update_id"`
	Message  *TelegramMessage `json:"message,omitempty"`
}

type TelegramMessage struct {
	MessageID int64           `json:"message_id"`
	From      *TelegramUser   `json:"from,omitempty"`
	Chat      *TelegramChat   `json:"chat,omitempty"`
	Date      int64           `json:"date"`
	Text      string          `json:"text,omitempty"`
	Photo     []TelegramPhoto `json:"photo,omitempty"`
	Caption   string          `json:"caption,omitempty"`
}

type TelegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username,omitempty"`
}

type TelegramChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type TelegramPhoto struct {
	FileID   string `json:"file_id"`
	FileSize int    `json:"file_size"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

// TelegramFileResponse represents the response from getFile API
type TelegramFileResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		FileID   string `json:"file_id"`
		FileSize int    `json:"file_size"`
		FilePath string `json:"file_path"`
	} `json:"result"`
}

// TelegramWebhookHandler handles incoming webhook updates from Telegram
func TelegramWebhookHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	secretToken := os.Getenv("TELEGRAM_WEBHOOK_SECRET")
	if secretToken != "" {
		incomingToken := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
		if incomingToken != secretToken {
			utils.Log.Warn().Msg("[Telegram Webhook] Unauthorized request: secret token mismatch")
			utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
	}

	var update TelegramUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		utils.HandleBadRequest(w, "Invalid JSON payload")
		return
	}

	// Handle the update
	if update.Message != nil {
		handleTelegramMessage(update.Message)
	}

	// Always return 200 OK to Telegram to prevent retries
	utils.JSONResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleTelegramMessage(msg *TelegramMessage) {
	if msg.Chat == nil {
		return
	}

	chatID := strconv.FormatInt(msg.Chat.ID, 10)

	// Find user by Telegram Chat ID
	var user database.User
	if err := database.DB.Where("telegram_chat_id = ?", chatID).First(&user).Error; err != nil {
		// User not registered, send welcome message
		sendTelegramMessage(chatID, "👋 Halo! Anda belum terhubung dengan akun Maybe Finance.\n\nSilakan daftar di aplikasi dan hubungkan Telegram Chat ID Anda.")
		return
	}

	// Handle /start command
	if msg.Text == "/start" {
		sendTelegramMessage(chatID, fmt.Sprintf("👋 Halo %s!\n\nBot keuangan Anda sudah aktif. Kirim foto struk/bill untuk mencatat transaksi otomatis.", user.Name))
		return
	}

	// Handle photo upload
	if len(msg.Photo) > 0 {
		handlePhotoUpload(msg, &user)
		return
	}

	// Handle text message (could be amount + description)
	if msg.Text != "" {
		handleTextTransaction(msg, &user)
	}
}

func downloadAndConvertTelegramPhoto(fileID, userID string) (string, error) {
	token := getTelegramToken()
	if token == "" {
		return "", fmt.Errorf("telegram token not configured")
	}

	// Step 1: Get file path from Telegram
	fileURL := fmt.Sprintf("https://api.telegram.org/bot%s/getFile?file_id=%s", token, fileID)
	resp, err := http.Get(fileURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var fileResp TelegramFileResponse
	if err := json.NewDecoder(resp.Body).Decode(&fileResp); err != nil {
		return "", err
	}

	if !fileResp.OK || fileResp.Result.FilePath == "" {
		return "", fmt.Errorf("failed to get file path")
	}

	// Step 2: Download the file
	downloadURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", token, fileResp.Result.FilePath)
	imgResp, err := http.Get(downloadURL)
	if err != nil {
		return "", err
	}
	defer imgResp.Body.Close()

	fileBytes, err := io.ReadAll(imgResp.Body)
	if err != nil {
		return "", err
	}

	// Step 3: Decode image
	var img image.Image
	contentType := imgResp.Header.Get("Content-Type")

	if strings.Contains(contentType, "jpeg") || strings.Contains(contentType, "jpg") {
		img, err = jpeg.Decode(bytes.NewReader(fileBytes))
	} else if strings.Contains(contentType, "png") {
		img, err = png.Decode(bytes.NewReader(fileBytes))
	} else if strings.Contains(contentType, "webp") {
		img, err = xwebp.Decode(bytes.NewReader(fileBytes))
	} else {
		// Try auto-detect
		img, _, err = image.Decode(bytes.NewReader(fileBytes))
	}

	if err != nil {
		return "", fmt.Errorf("failed to decode image: %v", err)
	}

	// Step 4: Create upload directory and save as WebP
	if err := os.MkdirAll(UploadDir, 0755); err != nil {
		return "", err
	}

	filename := generateUniqueFilename(userID) + getImageExtension()
	filePath := filepath.Join(UploadDir, filename)

	outputFile, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer outputFile.Close()

	// Encode image using platform-specific encoder
	if _, err := encodeImage(outputFile, img); err != nil {
		return "", err
	}

	// Step 5: Return relative path so it works from any host
	imageURL := fmt.Sprintf("/uploads/receipts/%s", filename)
	return imageURL, nil
}

func sendTelegramMessage(chatID, text string) {
	token := getTelegramToken()
	if token == "" {
		utils.Log.Warn().Msg("[Telegram] TELEGRAM_BOT_TOKEN or TELEGRAM_TOKEN is not set in env")
		return
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	jsonPayload, _ := json.Marshal(payload)
	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		utils.Log.Error().Err(err).Str("chat_id", chatID).Msg("[Telegram] HTTP request failed")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		utils.Log.Error().Str("status", resp.Status).Str("response", string(bodyBytes)).Msg("[Telegram] Telegram API returned error status")
	} else {
		utils.Log.Info().Str("chat_id", chatID).Msg("[Telegram] Message successfully sent")
	}
}

func getTelegramToken() string {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		token = os.Getenv("TELEGRAM_TOKEN")
	}
	return token
}

func generateTransactionID() string {
	bytes := make([]byte, 12)
	rand.Read(bytes)
	return "tx_" + hex.EncodeToString(bytes)
}
