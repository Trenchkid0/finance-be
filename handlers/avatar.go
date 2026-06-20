package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

// UploadAvatarHandler handles user avatar image uploads (saved directly as WebP)
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

	// Create upload directory if not exists
	avatarDir := "uploads/avatars"
	if err := os.MkdirAll(avatarDir, 0755); err != nil {
		utils.HandleDBError(w, err, "create upload directory")
		return
	}

	// Generate unique filename. Since the client exports WebP, save as .webp.
	filename := generateUniqueFilename(userID) + ".webp"
	filePath := filepath.Join(avatarDir, filename)

	// Write file directly to disk
	if err := os.WriteFile(filePath, fileBytes, 0644); err != nil {
		utils.HandleDBError(w, err, "save avatar file")
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
