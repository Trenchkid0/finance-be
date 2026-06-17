package utils

import (
	"fmt"
	"net/http"
	"os"
)

// HandleDBError logs a database error and returns a 500 response.
// In non-production environments, the real error message is included in the response
// to make debugging easier without needing direct server log access.
func HandleDBError(w http.ResponseWriter, err error, operation string) {
	Log.Error().Err(err).Str("operation", operation).Msg("Database operation failed")

	appEnv := os.Getenv("APP_ENV")
	if appEnv != "production" && appEnv != "prod" {
		// Show real error in dev/debug to simplify troubleshooting
		ErrorResponse(w, http.StatusInternalServerError, fmt.Sprintf("[%s] %v", operation, err))
		return
	}
	ErrorResponse(w, http.StatusInternalServerError, "An internal error occurred")
}

// HandleNotFound returns a 404 response for a missing resource.
func HandleNotFound(w http.ResponseWriter, resource string) {
	ErrorResponse(w, http.StatusNotFound, resource+" not found")
}

// HandleUnauthorized returns a 401 response.
// An optional message can be provided; defaults to "Unauthorized".
func HandleUnauthorized(w http.ResponseWriter, message ...string) {
	msg := "Unauthorized"
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorResponse(w, http.StatusUnauthorized, msg)
}

// HandleValidationError returns a 400 response with validation error details.
func HandleValidationError(w http.ResponseWriter, errs []string) {
	msg := "Validation failed"
	if len(errs) > 0 {
		msg = errs[0]
		if len(errs) > 1 {
			msg += " (+" + itoa(len(errs)-1) + " more)"
		}
	}
	ErrorResponse(w, http.StatusBadRequest, msg)
}

// HandleBadRequest returns a 400 response with a descriptive message.
func HandleBadRequest(w http.ResponseWriter, message string) {
	ErrorResponse(w, http.StatusBadRequest, message)
}

// HandleMethodNotAllowed returns a 405 response.
func HandleMethodNotAllowed(w http.ResponseWriter) {
	ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
}

// itoa converts int to string without importing strconv (avoids circular imports).
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
