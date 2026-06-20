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
		utils.HandleMethodNotAllowed(w)
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(MaxUploadSize); err != nil {
		utils.HandleBadRequest(w, "File too large or invalid form data")
		return
	}

	file, header, err := r.FormFile("receipt")
	if err != nil {
		utils.HandleBadRequest(w, "Receipt file is required")
		return
	}
	defer file.Close()

	// Validate file type
	contentType := header.Header.Get("Content-Type")
	if !isValidImageType(contentType) {
		utils.HandleBadRequest(w, "Only image files (JPEG, PNG, WebP) are allowed")
		return
	}

	// Validate file size
	if header.Size > MaxUploadSize {
		utils.HandleBadRequest(w, fmt.Sprintf("File size exceeds %d MB", MaxUploadSize/(1024*1024)))
		return
	}

	// Read file content
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		utils.HandleDBError(w, err, "read uploaded file")
		return
	}

	// SECURITY: Sniff magic bytes to verify actual file content matches claimed type
	// Prevents content-type spoofing (e.g. renaming a .exe to .jpg)
	detectedType := http.DetectContentType(fileBytes)
	if !strings.HasPrefix(detectedType, "image/") {
		utils.HandleBadRequest(w, "File content does not match an image type")
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
		utils.HandleBadRequest(w, "Unsupported image format")
		return
	}

	if err != nil {
		utils.HandleBadRequest(w, "Failed to decode image")
		return
	}

	// Create upload directory if not exists
	if err := os.MkdirAll(UploadDir, 0755); err != nil {
		utils.HandleDBError(w, err, "create upload directory")
		return
	}

	// Generate unique filename with appropriate extension
	filename := generateUniqueFilename(userID) + getImageExtension()
	filePath := filepath.Join(UploadDir, filename)

	// Encode image
	outputFile, err := os.Create(filePath)
	if err != nil {
		utils.HandleDBError(w, err, "create upload file")
		return
	}
	defer outputFile.Close()

	// Encode image using platform-specific encoder
	encodedSize, err := encodeImage(outputFile, img)
	if err != nil {
		utils.HandleDBError(w, err, "encode image")
		return
	}

	// Calculate compression ratio
	originalSize := len(fileBytes)
	compressionRatio := float64(originalSize-int(encodedSize)) / float64(originalSize) * 100

	// Return relative path so it works from any host (localhost, LAN IP, domain, etc.)
	imageURL := fmt.Sprintf("/uploads/receipts/%s", filename)

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
