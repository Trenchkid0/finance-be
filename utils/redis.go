package utils

import (
	"context"
	"encoding/json"
	"fmt"
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
		Log.Warn().Msg("Redis cache is disabled")
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
		Log.Error().Err(err).Str("host", host).Str("port", port).Msg("Failed to connect to Redis")
		Log.Warn().Msg("Continuing without cache...")
		RedisClient = nil
		return
	}

	cacheEnabled = true
	Log.Info().Str("host", host).Str("port", port).Int("db", db).Msg("Redis cache connected successfully")
}

// CloseRedis closes Redis connection
func CloseRedis() {
	if RedisClient != nil {
		if err := RedisClient.Close(); err != nil {
			Log.Error().Err(err).Msg("Error closing Redis connection")
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

// CacheDeletePatternsPipelined deletes all keys matching multiple patterns in a single Redis pipeline round-trip
// ✅ PERF: Reduces N network round-trips (one per pattern) to just 1
func CacheDeletePatternsPipelined(patterns []string) error {
	if !IsCacheEnabled() {
		return nil
	}

	pipe := RedisClient.Pipeline()

	// Stage all SCAN commands in the pipeline
	type scanCmd struct {
		iter *redis.ScanCmd
	}
	var cmds []scanCmd
	for _, pattern := range patterns {
		cmd := pipe.Scan(ctx, 0, pattern, 100)
		cmds = append(cmds, scanCmd{iter: cmd})
	}

	// Execute pipeline (1 round-trip for all SCANs)
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return err
	}

	// Collect all keys from scan results
	var allKeys []string
	for _, cmd := range cmds {
		keys, _, _ := cmd.iter.Result()
		allKeys = append(allKeys, keys...)
	}

	// Delete all collected keys in a single pipeline batch
	if len(allKeys) > 0 {
		delPipe := RedisClient.Pipeline()
		for _, key := range allKeys {
			delPipe.Del(ctx, key)
		}
		_, err = delPipe.Exec(ctx)
	}

	return err
}

// CacheInvalidateUser invalidates all cache entries for a specific user
// ✅ PERF: Uses pipelined deletion — single round-trip instead of 8 separate SCAN+DEL operations
func CacheInvalidateUser(userID string) error {
	patterns := []string{
		fmt.Sprintf("user:%s:*", userID),
		fmt.Sprintf("transactions:%s:*", userID),
		fmt.Sprintf("accounts:%s:*", userID),
		fmt.Sprintf("categories:%s:*", userID),
		fmt.Sprintf("budgets:%s:*", userID),
		fmt.Sprintf("goals:%s:*", userID),
		fmt.Sprintf("insights:%s:*", userID),
		fmt.Sprintf("summary:%s:*", userID),
	}

	return CacheDeletePatternsPipelined(patterns)
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
