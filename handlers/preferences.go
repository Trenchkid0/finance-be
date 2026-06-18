package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

// PreferencesData represents the JSON structure stored in UserPreference.Data.
type PreferencesData struct {
	ThemeID              string                 `json:"themeId"`
	CustomThemeVars      map[string]string      `json:"customThemeVars"`
	FontID               string                 `json:"fontId"`
	CardStyles           map[string]interface{} `json:"cardStyles"`
	ButtonStyles         map[string]interface{} `json:"buttonStyles"`
	TypographyStyles     map[string]interface{} `json:"typographyStyles"`
	NotificationSettings map[string]interface{} `json:"notificationSettings"`
	Language             string                 `json:"language"`
	DashboardLayout      string                 `json:"dashboardLayout"`
}

// defaultPreferences returns the default preferences for new users.
func defaultPreferences() PreferencesData {
	return PreferencesData{
		ThemeID:         "github-dark",
		CustomThemeVars: map[string]string{},
		FontID:          "jakarta",
		CardStyles: map[string]interface{}{
			"radius":         "16px",
			"borderWidth":    "1px",
			"blur":           "12px",
			"opacity":        "0.75",
			"dropdownRadius": "9999px",
		},
		ButtonStyles: map[string]interface{}{
			"radius": "12px",
			"size":   "default",
			"weight": "semibold",
		},
		TypographyStyles: map[string]interface{}{
			"normal":   "400",
			"medium":   "500",
			"semibold": "600",
			"bold":     "700",
		},
		NotificationSettings: map[string]interface{}{
			"position": "top-right",
			"theme":    "dark",
			"duration": 4000,
			"expand":   false,
		},
		Language: "id",
		DashboardLayout: "default",
	}
}

// preferencesCacheKey builds the Redis cache key for a user's preferences.
func preferencesCacheKey(userID string) string {
	return utils.BuildCacheKey("preferences", userID)
}

// PreferencesHandler handles GET and PUT /api/preferences.
// GET  — returns the user's preferences (from cache or DB, falls back to defaults).
// PUT  — accepts a full preferences JSON body, upserts the row, invalidates cache.
func PreferencesHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	switch r.Method {
	case http.MethodGet:
		getPreferences(w, userID)
	case http.MethodPut:
		updatePreferences(w, r, userID)
	default:
		utils.HandleMethodNotAllowed(w)
	}
}

// getPreferences fetches the user's preferences, using Redis cache when available.
func getPreferences(w http.ResponseWriter, userID string) {
	var prefs PreferencesData

	// Try Redis cache first
	fetchFunc := func() (PreferencesData, error) {
		var pref database.UserPreference
		if err := database.DB.Where("user_id = ?", userID).First(&pref).Error; err != nil {
			// No row yet — return defaults
			return defaultPreferences(), nil
		}

		var data PreferencesData
		if err := json.Unmarshal([]byte(pref.Data), &data); err != nil {
			utils.Log.Warn().Err(err).Str("user_id", userID).Msg("Failed to parse preferences JSON, returning defaults")
			return defaultPreferences(), nil
		}
		return data, nil
	}

	result, err := utils.CacheOrFetch(preferencesCacheKey(userID), utils.TTLLong, fetchFunc)
	if err != nil {
		utils.HandleDBError(w, err, "fetch preferences")
		return
	}
	prefs = result

	utils.JSONResponse(w, http.StatusOK, prefs)
}

// updatePreferences validates and upserts the user's preferences.
func updatePreferences(w http.ResponseWriter, r *http.Request, userID string) {
	var incoming PreferencesData
	if err := utils.ParseJSON(r, &incoming); err != nil {
		utils.HandleBadRequest(w, "Format data preferensi tidak valid.")
		return
	}

	// Re-serialize to validate it's well-formed JSON
	dataBytes, err := json.Marshal(incoming)
	if err != nil {
		utils.HandleBadRequest(w, "Gagal memproses data preferensi.")
		return
	}

	// Upsert: find existing row or create new one
	var pref database.UserPreference
	result := database.DB.Where("user_id = ?", userID).First(&pref)
	if result.Error != nil {
		// Create new row
		pref = database.UserPreference{
			ID:        uuid.New().String(),
			UserID:    userID,
			Data:      string(dataBytes),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := database.DB.Create(&pref).Error; err != nil {
			utils.HandleDBError(w, err, "create preferences")
			return
		}
	} else {
		// Update existing row
		if err := database.DB.Model(&pref).Updates(map[string]interface{}{
			"data":       string(dataBytes),
			"updated_at": time.Now(),
		}).Error; err != nil {
			utils.HandleDBError(w, err, "update preferences")
			return
		}
	}

	// Invalidate Redis cache so next GET fetches fresh data
	cacheKey := preferencesCacheKey(userID)
	_ = utils.CacheDelete(cacheKey)

	utils.JSONResponse(w, http.StatusOK, incoming)
}
