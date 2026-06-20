package utils

import (
	"net/http"
	"strconv"
)

// HandleDBError logs a database error and returns a user-friendly 500 response.
// The raw error is ALWAYS logged server-side but NEVER exposed to the client.
func HandleDBError(w http.ResponseWriter, err error, operation string) {
	Log.Error().Err(err).Str("operation", operation).Msg("Database operation failed")
	ErrorResponse(w, http.StatusInternalServerError,
		"Terjadi kesalahan pada server. Silakan coba lagi dalam beberapa saat.")
}

// HandleNotFound returns a 404 response for a missing resource.
func HandleNotFound(w http.ResponseWriter, resource string) {
	ErrorResponse(w, http.StatusNotFound, resource+" tidak ditemukan.")
}

// HandleUnauthorized returns a 401 response.
// An optional message can be provided; defaults to a user-friendly message.
func HandleUnauthorized(w http.ResponseWriter, message ...string) {
	msg := "Sesi Anda telah berakhir. Silakan login kembali."
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	ErrorResponse(w, http.StatusUnauthorized, msg)
}

// HandleValidationError returns a 400 response with validation error details.
func HandleValidationError(w http.ResponseWriter, errs []string) {
	msg := "Data yang dikirim tidak valid."
	if len(errs) > 0 {
		msg = errs[0]
		if len(errs) > 1 {
			msg += " (+" + strconv.Itoa(len(errs)-1) + " lainnya)"
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
	ErrorResponse(w, http.StatusMethodNotAllowed, "Metode tidak diizinkan.")
}


