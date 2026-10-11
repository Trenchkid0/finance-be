package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/services"
	"maybe-finance-backend/utils"
)

type SaveEmailSyncRequest struct {
	Email            string  `json:"email" validate:"required,email"`
	AppPassword      string  `json:"appPassword" validate:"required"`
	IMAPServer       string  `json:"imapServer"`
	DefaultAccountID *string `json:"defaultAccountId"`
	IsEnabled        bool    `json:"isEnabled"`
}

type TestEmailSyncRequest struct {
	Email       string `json:"email" validate:"required,email"`
	AppPassword string `json:"appPassword" validate:"required"`
	IMAPServer  string `json:"imapServer"`
}

type EmailSyncResponse struct {
	ID               string                  `json:"id"`
	UserID           string                  `json:"userId"`
	Email            string                  `json:"email"`
	IMAPServer       string                  `json:"imapServer"`
	DefaultAccountID *string                 `json:"defaultAccountId"`
	DefaultAccount   *database.FinanceAccount `json:"defaultAccount,omitempty"`
	IsEnabled        bool                    `json:"isEnabled"`
	LastSyncedAt     *time.Time              `json:"lastSyncedAt,omitempty"`
	LastSyncStatus   string                  `json:"lastSyncStatus"`
	LastSyncError    string                  `json:"lastSyncError,omitempty"`
	TotalParsed      int                     `json:"totalParsed"`
	HasPassword      bool                    `json:"hasPassword"`
}

// GetEmailSyncConfigHandler returns the user's current IMAP sync configuration
func GetEmailSyncConfigHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	var cfg database.EmailSyncConfig
	if err := database.DB.Preload("DefaultAccount").Where("user_id = ?", userID).First(&cfg).Error; err != nil {
		// Return empty / not configured status
		utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
			"isConfigured": false,
		})
		return
	}

	resp := EmailSyncResponse{
		ID:               cfg.ID,
		UserID:           cfg.UserID,
		Email:            cfg.Email,
		IMAPServer:       cfg.IMAPServer,
		DefaultAccountID: cfg.DefaultAccountID,
		DefaultAccount:   cfg.DefaultAccount,
		IsEnabled:        cfg.IsEnabled,
		LastSyncedAt:     cfg.LastSyncedAt,
		LastSyncStatus:   cfg.LastSyncStatus,
		LastSyncError:    cfg.LastSyncError,
		TotalParsed:      cfg.TotalParsed,
		HasPassword:      len(cfg.AppPassword) > 0,
	}

	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"isConfigured": true,
		"config":       resp,
	})
}

// TestEmailSyncHandler tests if the provided IMAP credentials are valid without saving
func TestEmailSyncHandler(w http.ResponseWriter, r *http.Request) {
	var req TestEmailSyncRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.HandleBadRequest(w, "Format data tidak valid")
		return
	}

	server := req.IMAPServer
	if server == "" {
		server = "imap.gmail.com:993"
	}

	if err := services.TestIMAPConnection(server, req.Email, req.AppPassword); err != nil {
		utils.HandleBadRequest(w, err.Error())
		return
	}

	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Koneksi IMAP berhasil! Server dan App Password valid.",
	})
}

// SaveEmailSyncConfigHandler saves or updates the user's email sync credentials
func SaveEmailSyncConfigHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	var req SaveEmailSyncRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.HandleBadRequest(w, "Format data tidak valid")
		return
	}

	server := req.IMAPServer
	if server == "" {
		server = "imap.gmail.com:993"
	}

	cleanPassword := strings.ReplaceAll(req.AppPassword, " ", "")

	// Test connection first
	if err := services.TestIMAPConnection(server, req.Email, cleanPassword); err != nil {
		utils.HandleBadRequest(w, fmt.Sprintf("Gagal memverifikasi akun Google: %s", err.Error()))
		return
	}

	var existing database.EmailSyncConfig
	hasExisting := database.DB.Where("user_id = ?", userID).First(&existing).Error == nil

	if hasExisting {
		existing.Email = req.Email
		if cleanPassword != "" {
			existing.AppPassword = cleanPassword
		}
		existing.IMAPServer = server
		existing.DefaultAccountID = req.DefaultAccountID
		existing.IsEnabled = req.IsEnabled
		existing.LastSyncError = ""
		existing.LastSyncStatus = "idle"

		if err := database.DB.Save(&existing).Error; err != nil {
			utils.HandleDBError(w, err, "update email sync config")
			return
		}
	} else {
		newCfg := database.EmailSyncConfig{
			ID:               uuid.New().String(),
			UserID:           userID,
			Email:            req.Email,
			AppPassword:      cleanPassword,
			IMAPServer:       server,
			DefaultAccountID: req.DefaultAccountID,
			IsEnabled:        req.IsEnabled,
			LastSyncStatus:   "idle",
		}

		if err := database.DB.Create(&newCfg).Error; err != nil {
			utils.HandleDBError(w, err, "create email sync config")
			return
		}
	}

	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Integrasi email bank berhasil disimpan dan diaktifkan!",
	})
}

type TriggerEmailSyncRequest struct {
	Days      int    `json:"days"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Reset     bool   `json:"reset"`
}

// TriggerEmailSyncHandler immediately syncs bank emails for the current user
func TriggerEmailSyncHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	var req TriggerEmailSyncRequest
	_ = utils.ParseJSON(r, &req)

	opts := services.EmailSyncOptions{
		Days:         req.Days,
		StartDate:    req.StartDate,
		EndDate:      req.EndDate,
		ResetHistory: req.Reset,
	}

	count, err := services.SyncUserEmails(userID, opts)
	if err != nil {
		utils.HandleBadRequest(w, fmt.Sprintf("Sinkronisasi gagal: %s", err.Error()))
		return
	}

	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"importedCount": count,
		"message":       fmt.Sprintf("Sinkronisasi selesai! Berhasil mengimpor %d transaksi baru.", count),
	})
}

// ResetEmailSyncHistoryHandler clears all processed email IDs for the user so emails can be rescanned
func ResetEmailSyncHistoryHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if err := database.DB.Where("user_id = ?", userID).Delete(&database.ProcessedEmail{}).Error; err != nil {
		utils.HandleDBError(w, err, "reset processed emails")
		return
	}

	database.DB.Model(&database.EmailSyncConfig{}).Where("user_id = ?", userID).Update("total_parsed", 0)

	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Riwayat cache email berhasil dibersihkan! Anda dapat melakukan sinkronisasi ulang sekarang.",
	})
}

// DeleteEmailSyncHandler removes the user's email sync configuration
func DeleteEmailSyncHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if err := database.DB.Where("user_id = ?", userID).Delete(&database.EmailSyncConfig{}).Error; err != nil {
		utils.HandleDBError(w, err, "delete email sync config")
		return
	}

	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Integrasi email bank berhasil diputuskan.",
	})
}
