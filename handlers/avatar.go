package handlers

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	xwebp "golang.org/x/image/webp"

	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

// UploadAvatarHandler handles user avatar image uploads (decoded & re-encoded safely)
func UploadAvatarHandler(w http.ResponseWriter, r *http.Request) {
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

	file, header, err := r.FormFile("avatar")
	if err != nil {
		utils.HandleBadRequest(w, "Avatar file is required")
		return
	}
	defer file.Close()

	// Validate file type
	contentType := header.Header.Get("Content-Type")
	if !isValidImageType(contentType) {
		utils.HandleBadRequest(w, "Only image files (JPEG, PNG, WebP) are allowed")
		return
	}

	// Read file content
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		utils.HandleDBError(w, err, "read uploaded file")
		return
	}

	// SECURITY: Sniff magic bytes to verify actual file content matches claimed type
	detectedType := http.DetectContentType(fileBytes)
	if !strings.HasPrefix(detectedType, "image/") {
		utils.HandleBadRequest(w, "File content does not match an image type")
		return
	}

	// Decode image safely
	var img image.Image
	var decodeErr error
	if strings.Contains(detectedType, "jpeg") || strings.Contains(detectedType, "jpg") {
		img, decodeErr = jpeg.Decode(bytes.NewReader(fileBytes))
	} else if strings.Contains(detectedType, "png") {
		img, decodeErr = png.Decode(bytes.NewReader(fileBytes))
	} else if strings.Contains(detectedType, "webp") {
		img, decodeErr = xwebp.Decode(bytes.NewReader(fileBytes))
	} else {
		img, _, decodeErr = image.Decode(bytes.NewReader(fileBytes))
	}

	if decodeErr != nil {
		utils.HandleBadRequest(w, "Failed to decode image file")
		return
	}

	// Create upload directory if not exists
	avatarDir := "uploads/avatars"
	if err := os.MkdirAll(avatarDir, 0755); err != nil {
		utils.HandleDBError(w, err, "create upload directory")
		return
	}

	// Generate unique filename with platform-supported extension
	filename := generateUniqueFilename(userID) + getImageExtension()
	filePath := filepath.Join(avatarDir, filename)

	outputFile, err := os.Create(filePath)
	if err != nil {
		utils.HandleDBError(w, err, "create avatar file")
		return
	}
	defer outputFile.Close()

	if _, err := encodeImage(outputFile, img); err != nil {
		utils.HandleDBError(w, err, "encode avatar image")
		return
	}

	// Return relative path so it works from any host (localhost, LAN IP, domain, etc.)
	imageURL := fmt.Sprintf("/uploads/avatars/%s", filename)

	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"url":      imageURL,
		"filename": filename,
		"message":  "Avatar uploaded successfully",
	})
}
