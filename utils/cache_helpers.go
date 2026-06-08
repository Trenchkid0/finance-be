package utils

import (
	"encoding/json"
	"fmt"
	"time"
)

// CacheOrFetch tries to get data from cache, or fetches and caches it
func CacheOrFetch[T any](key string, ttl time.Duration, fetchFunc func() (T, error)) (T, error) {
	var result T
	
	// Try cache first
	if IsCacheEnabled() {
		if err := CacheGet(key, &result); err == nil {
			return result, nil
		}
	}
	
	// Cache miss or disabled - fetch from source
	data, err := fetchFunc()
	if err != nil {
		return result, err
	}
	
	// Store in cache
	if IsCacheEnabled() {
		_ = CacheSet(key, data, ttl)
	}
	
	return data, nil
}

// CacheOrFetchJSON is similar but works with JSON serialization
func CacheOrFetchJSON[T any](key string, ttl time.Duration, fetchFunc func() (T, error)) (T, error) {
	return CacheOrFetch(key, ttl, fetchFunc)
}

// CacheHTTPResponse caches the entire HTTP response
func CacheHTTPResponse(key string, data interface{}, ttl time.Duration) {
	if !IsCacheEnabled() {
		return
	}
	_ = CacheSet(key, data, ttl)
}

// InvalidateUserCache clears all cache for a user after mutations
func InvalidateUserCache(userID string, patterns ...string) {
	if !IsCacheEnabled() {
		return
	}
	
	// Default patterns if none provided
	if len(patterns) == 0 {
		patterns = []string{"summary", "transactions", "accounts"}
	}
	
	for _, pattern := range patterns {
		key := BuildCacheKey(pattern, userID, "*")
		_ = CacheDeletePattern(key)
	}
}

// Common cache key builders
func SummaryCacheKey(userID, period, cashflowPeriod string) string {
	return BuildCacheKey("summary", userID, period, cashflowPeriod)
}

func TransactionsCacheKey(userID string, filters map[string]string) string {
	parts := []string{"transactions", userID}
	
	if accountID, ok := filters["accountId"]; ok && accountID != "" {
		parts = append(parts, "acc", accountID)
	}
	if txType, ok := filters["type"]; ok && txType != "" {
		parts = append(parts, "type", txType)
	}
	if categoryID, ok := filters["categoryId"]; ok && categoryID != "" {
		parts = append(parts, "cat", categoryID)
	}
	if limit, ok := filters["limit"]; ok && limit != "" {
		parts = append(parts, "limit", limit)
	}
	
	return BuildCacheKey(parts...)
}

func AccountsCacheKey(userID string) string {
	return BuildCacheKey("accounts", userID, "list")
}

func AccountDetailCacheKey(userID, accountID string) string {
	return BuildCacheKey("accounts", userID, accountID)
}

func CategoriesCacheKey(userID string) string {
	return BuildCacheKey("categories", userID, "list")
}

func BudgetsCacheKey(userID, year, month string) string {
	return BuildCacheKey("budgets", userID, year, month)
}

func GoalsCacheKey(userID string) string {
	return BuildCacheKey("goals", userID, "list")
}

// TTL Constants
const (
	TTLVeryShort = 1 * time.Minute   // For real-time data
	TTLShort     = 3 * time.Minute   // For frequently changing data
	TTLMedium    = 5 * time.Minute   // Default for most endpoints
	TTLLong      = 15 * time.Minute  // For rarely changing data
	TTLVeryLong  = 1 * time.Hour     // For static/reference data
)

// Debug helper
func LogCacheHit(key string) {
	if IsCacheEnabled() {
		fmt.Printf("💾 Cache HIT: %s\n", key)
	}
}

func LogCacheMiss(key string) {
	if IsCacheEnabled() {
		fmt.Printf("❌ Cache MISS: %s\n", key)
	}
}

// Serialize/Deserialize helpers for complex types
func SerializeToCache(key string, data interface{}, ttl time.Duration) error {
	if !IsCacheEnabled() {
		return nil
	}
	
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	
	return CacheSet(key, string(jsonData), ttl)
}

func DeserializeFromCache(key string, dest interface{}) error {
	if !IsCacheEnabled() {
		return fmt.Errorf("cache not enabled")
	}
	
	var jsonStr string
	if err := CacheGet(key, &jsonStr); err != nil {
		return err
	}
	
	return json.Unmarshal([]byte(jsonStr), dest)
}
