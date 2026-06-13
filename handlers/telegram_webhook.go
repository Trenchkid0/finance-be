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
	"time"

	xwebp "golang.org/x/image/webp"

	"maybe-finance-backend/database"
	"maybe-finance-backend/utils"
)

// TelegramUpdate represents the webhook payload from Telegram
type TelegramUpdate struct {
	UpdateID int64           `json:"update_id"`
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
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var update TelegramUpdate
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		utils.ErrorResponse(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	// Handle the update
	if update.Message != nil {
		handleTelegramMessage(update.Message)
	}

	// Always return 200 OK to Telegram to prevent retries
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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

func handlePhotoUpload(msg *TelegramMessage, user *database.User) {
	chatID := strconv.FormatInt(msg.Chat.ID, 10)

	// Get the largest photo size (last in array)
	largestPhoto := msg.Photo[len(msg.Photo)-1]

	// Download and convert to WebP
	imageURL, err := downloadAndConvertTelegramPhoto(largestPhoto.FileID, user.ID)
	if err != nil {
		sendTelegramMessage(chatID, "❌ Gagal memproses gambar. Silakan coba lagi.")
		return
	}

	// Parse caption for transaction details
	amount, description, categoryID := parseCaption(msg.Caption, user.ID)

	// Find default account for user
	var account database.FinanceAccount
	if err := database.DB.Where("user_id = ?", user.ID).Order("created_at ASC").First(&account).Error; err != nil {
		sendTelegramMessage(chatID, "❌ Anda belum memiliki akun keuangan. Silakan buat akun terlebih dahulu di aplikasi.")
		return
	}

	// Begin DB transaction to ensure balance consistency
	tx := database.DB.Begin()

	// Reconcile Balance (Telegram bot is expense by default)
	account.Balance -= amount
	if err := tx.Save(&account).Error; err != nil {
		tx.Rollback()
		sendTelegramMessage(chatID, "❌ Gagal memperbarui saldo akun.")
		return
	}

	// Create transaction
	transaction := database.Transaction{
		UserID:          user.ID,
		AccountID:       account.ID,
		CategoryID:      categoryID,
		Type:            database.TransactionTypeExpense,
		Amount:          amount,
		Description:     description,
		Note:            "Dicatat via Telegram Bot",
		Date:            time.Now(),
		ReceiptImageURL: &imageURL,
	}

	if err := tx.Create(&transaction).Error; err != nil {
		tx.Rollback()
		sendTelegramMessage(chatID, "❌ Gagal menyimpan transaksi.")
		return
	}

	tx.Commit()

	// Invalidate related caches
	_ = utils.CacheInvalidateUser(user.ID)

	// Send success message
	message := fmt.Sprintf("✅ Transaksi tercatat!\n\n💰 Jumlah: Rp %.0f\n📝 Deskripsi: %s\n📷 Struk: Tersimpan\n\nLihat detail di aplikasi Maybe Finance.",
		amount, description)
	sendTelegramMessage(chatID, message)
}

func handleTextTransaction(msg *TelegramMessage, user *database.User) {
	chatID := strconv.FormatInt(msg.Chat.ID, 10)
	text := strings.TrimSpace(msg.Text)

	// Try to parse amount from text (first number found)
	amount, description := parseAmountFromText(text)
	if amount <= 0 {
		sendTelegramMessage(chatID, "⚠️ Format tidak dikenali.\n\nKirim foto struk untuk mencatat transaksi, atau ketik jumlah dan deskripsi (contoh: \"50000 Makan siang\").")
		return
	}

	// Find default account
	var account database.FinanceAccount
	if err := database.DB.Where("user_id = ?", user.ID).Order("created_at ASC").First(&account).Error; err != nil {
		sendTelegramMessage(chatID, "❌ Anda belum memiliki akun keuangan.")
		return
	}

	// Begin DB transaction to ensure balance consistency
	tx := database.DB.Begin()

	// Reconcile Balance (Telegram bot is expense by default)
	account.Balance -= amount
	if err := tx.Save(&account).Error; err != nil {
		tx.Rollback()
		sendTelegramMessage(chatID, "❌ Gagal memperbarui saldo akun.")
		return
	}

	// Create transaction
	transaction := database.Transaction{
		UserID:      user.ID,
		AccountID:   account.ID,
		Type:        database.TransactionTypeExpense,
		Amount:      amount,
		Description: description,
		Note:        "Dicatat via Telegram Bot",
		Date:        time.Now(),
	}

	if err := tx.Create(&transaction).Error; err != nil {
		tx.Rollback()
		sendTelegramMessage(chatID, "❌ Gagal menyimpan transaksi.")
		return
	}

	tx.Commit()

	// Invalidate related caches
	_ = utils.CacheInvalidateUser(user.ID)

	message := fmt.Sprintf("✅ Transaksi tercatat!\n\n💰 Jumlah: Rp %.0f\n📝 Deskripsi: %s\n\nLihat detail di aplikasi Maybe Finance.",
		amount, description)
	sendTelegramMessage(chatID, message)
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

	// Step 5: Return the URL
	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	imageURL := fmt.Sprintf("%s/uploads/receipts/%s", baseURL, filename)
	return imageURL, nil
}

func sendTelegramMessage(chatID, text string) {
	token := getTelegramToken()
	if token == "" {
		log.Printf("⚠️ [Telegram] Failed to send message: TELEGRAM_BOT_TOKEN or TELEGRAM_TOKEN is not set in env")
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
		log.Printf("❌ [Telegram] HTTP request failed for chatID %s: %v", chatID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("❌ [Telegram] Telegram API returned error status: %s. Response body: %s", resp.Status, string(bodyBytes))
	} else {
		log.Printf("✅ [Telegram] Message successfully sent to chatID %s", chatID)
	}
}

func getTelegramToken() string {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		token = os.Getenv("TELEGRAM_TOKEN")
	}
	return token
}

func parseCaption(caption, userID string) (float64, string, *string) {
	if caption == "" {
		return 0, "Transaksi dari Telegram", nil
	}

	amount, description := parseAmountFromText(caption)

	// Try to find category from caption
	var category database.Category
	if err := database.DB.Where("user_id = ? AND name LIKE ?", userID, "%"+description+"%").First(&category).Error; err == nil {
		return amount, description, &category.ID
	}

	return amount, description, nil
}

func parseAmountFromText(text string) (float64, string) {
	// Remove common currency symbols and separators
	text = strings.ReplaceAll(text, ".", "")
	text = strings.ReplaceAll(text, ",", "")
	text = strings.TrimSpace(text)

	words := strings.Fields(text)
	if len(words) == 0 {
		return 0, ""
	}

	// Try to parse first word as amount
	amount, err := strconv.ParseFloat(words[0], 64)
	if err != nil {
		// Try finding number anywhere in text
		for _, word := range words {
			if amt, err := strconv.ParseFloat(word, 64); err == nil {
				amount = amt
				// Get remaining text as description
				idx := strings.Index(text, word) + len(word)
				description := strings.TrimSpace(text[idx:])
				return amount, description
			}
		}
		return 0, text
	}

	// Get remaining text as description
	description := strings.Join(words[1:], " ")
	if description == "" {
		description = "Transaksi dari Telegram"
	}

	return amount, description
}

func generateTransactionID() string {
	bytes := make([]byte, 12)
	rand.Read(bytes)
	return "tx_" + hex.EncodeToString(bytes)
}
