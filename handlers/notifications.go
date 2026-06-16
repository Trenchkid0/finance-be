package handlers

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

// GetNotificationsHandler returns all notifications for the user
func GetNotificationsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}

	var notifications []database.Notification
	if err := database.DB.Where("user_id = ?", userID).Order("created_at DESC").Find(&notifications).Error; err != nil {
		utils.HandleDBError(w, err, "fetch notifications")
		return
	}

	utils.JSONResponse(w, http.StatusOK, notifications)
}

// ReadNotificationsHandler marks notifications as read
func ReadNotificationsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	var req struct {
		IDs []string `json:"ids"`
	}
	_ = utils.ParseJSON(r, &req)

	query := database.DB.Model(&database.Notification{}).Where("user_id = ?", userID)
	if len(req.IDs) > 0 {
		query = query.Where("id IN ?", req.IDs)
	}

	if err := query.Update("is_read", true).Error; err != nil {
		utils.HandleDBError(w, err, "mark notifications read")
		return
	}

	utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Notifications marked as read"})
}

// DeleteNotificationHandler deletes a specific notification
func DeleteNotificationHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodDelete {
		utils.HandleMethodNotAllowed(w)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		utils.HandleBadRequest(w, "Missing notification ID")
		return
	}

	if err := database.DB.Where("id = ? AND user_id = ?", id, userID).Delete(&database.Notification{}).Error; err != nil {
		utils.HandleDBError(w, err, "delete notification")
		return
	}

	utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "Notification deleted successfully"})
}

// ClearNotificationsHandler deletes all notifications for the user
func ClearNotificationsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if r.Method != http.MethodDelete {
		utils.HandleMethodNotAllowed(w)
		return
	}

	if err := database.DB.Where("user_id = ?", userID).Delete(&database.Notification{}).Error; err != nil {
		utils.HandleDBError(w, err, "clear notifications")
		return
	}

	utils.JSONResponse(w, http.StatusOK, map[string]string{"message": "All notifications cleared"})
}

// CreateNotificationHelper is an internal helper to push notifications
func CreateNotificationHelper(userID, title, message, notifType string) error {
	notif := database.Notification{
		ID:        uuid.New().String(),
		UserID:    userID,
		Title:     title,
		Message:   message,
		Type:      notifType,
		IsRead:    false,
		CreatedAt: time.Now(),
	}
	err := database.DB.Create(&notif).Error
	if err != nil {
		utils.Log.Error().Err(err).Str("user_id", userID).Str("title", title).Msg("Failed to create notification in DB")
	}
	return err
}

