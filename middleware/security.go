package middleware

import "net/http"

// SecurityHeaders adds standard security headers to all HTTP responses.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent MIME-sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")
		// X-XSS-Protection block
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		// Enable HSTS in production (1 year)
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		// Basic Content Security Policy (allows self, frame-ancestors none, object-src none)
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; object-src 'none';")

		next.ServeHTTP(w, r)
	})
}
