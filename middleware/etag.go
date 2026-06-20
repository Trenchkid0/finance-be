package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

// captureWriter captures the response body and status code without
// writing to the underlying ResponseWriter immediately.
type captureWriter struct {
	header     http.Header
	buf        bytes.Buffer
	statusCode int
}

func (w *captureWriter) Header() http.Header {
	return w.header
}

func (w *captureWriter) WriteHeader(code int) {
	w.statusCode = code
}

func (w *captureWriter) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

// ETagMiddleware adds ETag support for GET requests.
//
// How it works:
// 1. Captures the full response body for GET requests.
// 2. Computes a weak ETag (SHA-256 hash of the body).
// 3. Compares with the client's If-None-Match header.
// 4. If they match → returns 304 Not Modified (zero body transfer).
// 5. If they don't match → sends the response with the ETag header.
//
// This reduces bandwidth on repeated identical requests and allows
// browser/proxy caches to serve cached responses efficiently.
func ETagMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only intercept GET requests
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}

		clientETag := r.Header.Get("If-None-Match")

		// Capture the handler's response
		captured := &captureWriter{
			header:     make(http.Header),
			statusCode: http.StatusOK,
		}

		next.ServeHTTP(captured, r)

		// Only apply ETag logic to successful JSON responses
		contentType := captured.header.Get("Content-Type")
		if captured.statusCode != http.StatusOK || !strings.Contains(contentType, "application/json") {
			// Copy captured headers
			for k, v := range captured.header {
				for _, val := range v {
					w.Header().Add(k, val)
				}
			}
			w.WriteHeader(captured.statusCode)
			w.Write(captured.buf.Bytes())
			return
		}

		body := captured.buf.Bytes()
		if len(body) == 0 {
			// Empty body — pass through
			for k, v := range captured.header {
				for _, val := range v {
					w.Header().Add(k, val)
				}
			}
			w.WriteHeader(captured.statusCode)
			return
		}

		// Compute weak ETag: W/"sha256-prefix"
		hash := sha256.Sum256(body)
		etag := `W/"` + hex.EncodeToString(hash[:16]) + `"`

		// 304 Not Modified — body unchanged since client's last request
		if clientETag != "" && clientETag == etag {
			// Copy relevant headers but do NOT send body
			w.Header().Set("ETag", etag)
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
			w.WriteHeader(http.StatusNotModified)
			return
		}

		// Body changed or first request — send full response with ETag
		for k, v := range captured.header {
			for _, val := range v {
				w.Header().Add(k, val)
			}
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.WriteHeader(captured.statusCode)
		w.Write(body)
	})
}
