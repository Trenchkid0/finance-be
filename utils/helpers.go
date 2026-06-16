package utils

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// FormatRupiah formats a float64 amount to Indonesian Rupiah representation (dot separator)
func FormatRupiah(amount float64) string {
	amountVal := int64(amount)
	amountStr := strconv.FormatInt(amountVal, 10)
	var formattedAmount []rune
	for i, r := range amountStr {
		if i > 0 && (len(amountStr)-i)%3 == 0 {
			formattedAmount = append(formattedAmount, '.')
		}
		formattedAmount = append(formattedAmount, r)
	}
	return string(formattedAmount)
}

// JSONResponse writes a JSON response to the writer
func JSONResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

// ErrorResponse writes a structured JSON error response
func ErrorResponse(w http.ResponseWriter, status int, message string) {
	JSONResponse(w, status, map[string]string{"error": message})
}

// ParseJSON parses the request body into target struct
func ParseJSON(r *http.Request, dst interface{}) error {
	return json.NewDecoder(r.Body).Decode(dst)
}
