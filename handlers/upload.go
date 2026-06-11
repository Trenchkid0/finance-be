package handlers

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	xwebp "golang.org/x/image/webp"

	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

const (
	MaxUploadSize = 10 * 1024 * 1024 // 10 MB
	UploadDir     = "uploads/receipts"
	WebPQuality   = 85 // Quality 85 untuk balance size vs quality
)

// UploadReceiptHandler handles receipt image uploads and converts to optimized format
func UploadReceiptHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(MaxUploadSize); err != nil {
		utils.ErrorResponse(w, http.StatusBadRequest, "File too large or invalid form data")
		return
	}

	file, header, err := r.FormFile("receipt")
	if err != nil {
		utils.ErrorResponse(w, http.StatusBadRequest, "Receipt file is required")
		return
	}
	defer file.Close()

	// Validate file type
	contentType := header.Header.Get("Content-Type")
	if !isValidImageType(contentType) {
		utils.ErrorResponse(w, http.StatusBadRequest, "Only image files (JPEG, PNG, WebP) are allowed")
		return
	}

	// Validate file size
	if header.Size > MaxUploadSize {
		utils.ErrorResponse(w, http.StatusBadRequest, fmt.Sprintf("File size exceeds %d MB", MaxUploadSize/(1024*1024)))
		return
	}

	// Read file content
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to read file")
		return
	}

	// Decode image based on content type
	var img image.Image
	if strings.Contains(contentType, "jpeg") || strings.Contains(contentType, "jpg") {
		img, err = jpeg.Decode(bytes.NewReader(fileBytes))
	} else if strings.Contains(contentType, "png") {
		img, err = png.Decode(bytes.NewReader(fileBytes))
	} else if strings.Contains(contentType, "webp") {
		img, err = xwebp.Decode(bytes.NewReader(fileBytes))
	} else {
		utils.ErrorResponse(w, http.StatusBadRequest, "Unsupported image format")
		return
	}

	if err != nil {
		utils.ErrorResponse(w, http.StatusBadRequest, "Failed to decode image")
		return
	}

	// Create upload directory if not exists
	if err := os.MkdirAll(UploadDir, 0755); err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to create upload directory")
		return
	}

	// Generate unique filename with appropriate extension
	filename := generateUniqueFilename(userID) + getImageExtension()
	filePath := filepath.Join(UploadDir, filename)

	// Encode image
	outputFile, err := os.Create(filePath)
	if err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to create file")
		return
	}
	defer outputFile.Close()

	// Encode image using platform-specific encoder
	encodedSize, err := encodeImage(outputFile, img)
	if err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Failed to encode image")
		return
	}

	// Calculate compression ratio
	originalSize := len(fileBytes)
	compressionRatio := float64(originalSize-int(encodedSize)) / float64(originalSize) * 100

	// Return URL (dynamically built from request host/scheme)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)
	imageURL := fmt.Sprintf("%s/uploads/receipts/%s", baseURL, filename)

	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"url":              imageURL,
		"filename":         filename,
		"message":          "Receipt uploaded successfully",
		"originalSize":     originalSize,
		"encodedSize":      encodedSize,
		"compressionRatio": fmt.Sprintf("%.1f%%", compressionRatio),
		"format":           getImageMimeType(),
	})
}

func isValidImageType(contentType string) bool {
	validTypes := []string{
		"image/jpeg",
		"image/jpg",
		"image/png",
		"image/webp",
	}
	for _, t := range validTypes {
		if strings.EqualFold(contentType, t) {
			return true
		}
	}
	return false
}

func generateUniqueFilename(userID string) string {
	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)
	randomStr := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("%s_%d_%s", userID, getCurrentTimestamp(), randomStr)
}

func getCurrentTimestamp() int64 {
	return time.Now().Unix()
}
