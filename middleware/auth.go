package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"maybe-finance-backend/database"
)

type contextKey string

const UserIDContextKey contextKey = "userId"

var jwtSecret = []byte(getEnv("JWT_SECRET", "super-secret-default-key-change-me"))

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

// Claims represents the JWT claims
type Claims struct {
	UserID string `json:"userId"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// GenerateToken creates a signed JWT token for a user
func GenerateToken(userID, email string) (string, error) {
	expirationTime := time.Now().Add(7 * 24 * time.Hour) // 7 days
	claims := &Claims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

// ParseToken verifies and parses the JWT token
func ParseToken(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return jwtSecret, nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}

// GetUserIDFromContext extracts the userID string from the request context
func GetUserIDFromContext(ctx context.Context) (string, bool) {
	val := ctx.Value(UserIDContextKey)
	userID, ok := val.(string)
	return userID, ok
}

// AuthRequired is a middleware that enforces authentication via jwt cookie, Bearer JWT token, or Bearer API Key.
func AuthRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var userID string
		var authenticated bool

		// 1. Try to get auth_token cookie
		cookie, err := r.Cookie("auth_token")
		if err == nil {
			claims, err := ParseToken(cookie.Value)
			if err == nil {
				userID = claims.UserID
				authenticated = true
			}
		}

		// 2. Try to get Authorization header
		if !authenticated {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" && len(authHeader) > 7 && strings.HasPrefix(authHeader, "Bearer ") {
				tokenOrKey := authHeader[7:]

				// Try to parse as JWT first
				claims, err := ParseToken(tokenOrKey)
				if err == nil {
					userID = claims.UserID
					authenticated = true
				} else {
					// If not valid JWT, try to look up as API Key in DB
					hash := sha256.Sum256([]byte(tokenOrKey))
					hashHex := hex.EncodeToString(hash[:])

					var apiKey database.ApiKey
					if err := database.DB.Where("key_hash = ? AND revoked_at IS NULL", hashHex).First(&apiKey).Error; err == nil {
						userID = apiKey.UserID
						authenticated = true

						// Update last used time asynchronously to prevent blocking the request
						go func(keyID string) {
							now := time.Now()
							database.DB.Model(&database.ApiKey{}).Where("id = ?", keyID).Update("last_used_at", &now)
						}(apiKey.ID)
					}
				}
			}
		}

		if !authenticated {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "Unauthorized: Authentication required"}`))
			return
		}

		// Store UserID in context
		ctx := context.WithValue(r.Context(), UserIDContextKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
