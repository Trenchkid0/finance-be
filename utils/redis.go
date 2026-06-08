package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	RedisClient *redis.Client
	ctx         = context.Background()
	cacheEnabled = false
)

// InitRedis initializes Redis connection
func InitRedis() {
	enabled := os.Getenv("REDIS_ENABLED")
	if enabled != "true" {
		log.Println("⚠️  Redis cache is disabled")
		return
	}

	host := os.Getenv("REDIS_HOST")
	port := os.Getenv("REDIS_PORT")
	password := os.Getenv("REDIS_PASSWORD")
	dbStr := os.Getenv("REDIS_DB")

	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "6379"
	}

	db := 0
	if dbStr != "" {
		if parsedDB, err := strconv.Atoi(dbStr); err == nil {
			db = parsedDB
		}
	}

	RedisClient = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", host, port),
		Password: password,
		DB:       db,
	})

	// Test connection
	if err := RedisClient.Ping(ctx).Err(); err != nil {
		log.Printf("❌ Failed to connect to Redis at %s:%s - %v\n", host, port, err)
		log.Println("⚠️  Continuing without cache...")
		RedisClient = nil
		return
	}

	cacheEnabled = true
	log.Printf("✅ Redis cache connected successfully at %s:%s (DB: %d)\n", host, port, db)
}

// CloseRedis closes Redis connection
func CloseRedis() {
	if RedisClient != nil {
		if err := RedisClient.Close(); err != nil {
			log.Printf("Error closing Redis connection: %v\n", err)
		}
	}
}

// IsCacheEnabled checks if cache is enabled and connected
func IsCacheEnabled() bool {
	return cacheEnabled && RedisClient != nil
}

// CacheGet retrieves a value from cache
func CacheGet(key string, dest interface{}) error {
	if !IsCacheEnabled() {
		return fmt.Errorf("cache not enabled")
	}

	val, err := RedisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return fmt.Errorf("cache miss")
	}
	if err != nil {
		return err
	}

	return json.Unmarshal([]byte(val), dest)
}

// CacheSet stores a value in cache with expiration
func CacheSet(key string, value interface{}, expiration time.Duration) error {
	if !IsCacheEnabled() {
		return nil // Silent fail if cache not enabled
	}

	jsonData, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return RedisClient.Set(ctx, key, jsonData, expiration).Err()
}

// CacheDelete removes a key from cache
func CacheDelete(keys ...string) error {
	if !IsCacheEnabled() {
		return nil
	}

	return RedisClient.Del(ctx, keys...).Err()
}

// CacheDeletePattern deletes all keys matching a pattern
func CacheDeletePattern(pattern string) error {
	if !IsCacheEnabled() {
		return nil
	}

	iter := RedisClient.Scan(ctx, 0, pattern, 0).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}

	if len(keys) > 0 {
		return RedisClient.Del(ctx, keys...).Err()
	}

	return nil
}

// CacheInvalidateUser invalidates all cache entries for a specific user
func CacheInvalidateUser(userID string) error {
	patterns := []string{
		fmt.Sprintf("user:%s:*", userID),
		fmt.Sprintf("transactions:%s:*", userID),
		fmt.Sprintf("accounts:%s:*", userID),
		fmt.Sprintf("categories:%s:*", userID),
		fmt.Sprintf("budgets:%s:*", userID),
		fmt.Sprintf("goals:%s:*", userID),
	}

	for _, pattern := range patterns {
		if err := CacheDeletePattern(pattern); err != nil {
			log.Printf("Error invalidating cache pattern %s: %v\n", pattern, err)
		}
	}

	return nil
}

// BuildCacheKey builds a standardized cache key
func BuildCacheKey(parts ...string) string {
	key := ""
	for i, part := range parts {
		if i > 0 {
			key += ":"
		}
		key += part
	}
	return key
}
