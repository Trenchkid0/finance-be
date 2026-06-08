# Redis Cache Implementation Guide

## Overview

Redis caching has been integrated into the backend to improve performance and reduce database load.

## Configuration

Add these environment variables to your `.env` file:

```env
# Redis Configuration
REDIS_HOST=100.86.12.111      # Redis server hostname/IP
REDIS_PORT=6379               # Redis port (default: 6379)
REDIS_PASSWORD=               # Redis password (leave empty if no auth)
REDIS_DB=0                    # Redis database number (0-15)
REDIS_ENABLED=true            # Enable/disable Redis cache
```

## Features

- ✅ **Automatic Connection**: Redis connects on startup
- ✅ **Graceful Degradation**: App continues working if Redis is unavailable
- ✅ **Pattern-based Invalidation**: Efficiently clear related cache entries
- ✅ **JSON Serialization**: Automatically handles complex data structures
- ✅ **User-scoped Cache**: Isolated cache per user for security

## Available Functions

### Core Cache Functions

```go
// Check if cache is enabled
if utils.IsCacheEnabled() {
    // Cache operations...
}

// Get from cache
var data MyStruct
err := utils.CacheGet("my-key", &data)
if err != nil {
    // Cache miss - fetch from database
}

// Set cache with expiration
utils.CacheSet("my-key", data, 5*time.Minute)

// Delete specific keys
utils.CacheDelete("key1", "key2", "key3")

// Delete by pattern
utils.CacheDeletePattern("user:123:*")

// Invalidate all user data
utils.CacheInvalidateUser(userID)

// Build standardized cache keys
key := utils.BuildCacheKey("user", userID, "transactions", "list")
// Result: "user:123:transactions:list"
```

## Cache Key Conventions

Use consistent naming patterns:

```
user:<userID>:summary
user:<userID>:profile
transactions:<userID>:list
transactions:<userID>:detail:<txID>
accounts:<userID>:list
accounts:<userID>:<accountID>
categories:<userID>:list
budgets:<userID>:<year>:<month>
goals:<userID>:list
```

## Expiration Times

Recommended cache durations:

| Data Type | Duration | Use Case |
|-----------|----------|----------|
| **Short** | 5 min | Frequently changing (transactions, balances) |
| **Medium** | 30 min | Moderately changing (categories, accounts) |
| **Long** | 2 hours | Rarely changing (user profile, settings) |

## Implementation Example

### 1. In Handler (GET endpoint)

```go
func TransactionsHandler(w http.ResponseWriter, r *http.Request) {
    userID := middleware.GetUserIDFromContext(r.Context())
    
    // Build cache key
    cacheKey := utils.BuildCacheKey("transactions", userID, "list")
    
    // Try cache first
    var transactions []database.Transaction
    if err := utils.CacheGet(cacheKey, &transactions); err == nil {
        // Cache HIT - return immediately
        utils.JSONResponse(w, http.StatusOK, transactions)
        return
    }
    
    // Cache MISS - fetch from database
    db.Where("user_id = ?", userID).Find(&transactions)
    
    // Store in cache for next time
    utils.CacheSet(cacheKey, transactions, 5*time.Minute)
    
    utils.JSONResponse(w, http.StatusOK, transactions)
}
```

### 2. In Handler (POST/PUT/DELETE endpoint)

```go
func CreateTransactionHandler(w http.ResponseWriter, r *http.Request) {
    userID := middleware.GetUserIDFromContext(r.Context())
    
    // Create transaction in database
    // ...
    
    // Invalidate related caches
    utils.CacheDeletePattern(fmt.Sprintf("transactions:%s:*", userID))
    utils.CacheDeletePattern(fmt.Sprintf("accounts:%s:*", userID))
    utils.CacheDeletePattern(fmt.Sprintf("user:%s:summary", userID))
    
    utils.JSONResponse(w, http.StatusCreated, transaction)
}
```

### 3. Using Service Layer (Recommended)

See `services/cache_example.go` for complete examples:

```go
// In your handler
transactions, err := services.GetTransactionListWithCache(userID, filters)

// After modifying data
services.InvalidateTransactionCache(userID)
```

## Cache Invalidation Strategy

### When to Invalidate:

1. **POST** (Create) → Invalidate list caches
2. **PUT** (Update) → Invalidate specific item + list caches
3. **DELETE** → Invalidate specific item + list caches

### Invalidation Patterns:

```go
// Single transaction changed
utils.CacheDelete(
    utils.BuildCacheKey("transactions", userID, "detail", txID),
)
utils.CacheDeletePattern(fmt.Sprintf("transactions:%s:list*", userID))

// Account balance changed
utils.CacheDeletePattern(fmt.Sprintf("accounts:%s:*", userID))
utils.CacheDeletePattern(fmt.Sprintf("user:%s:summary*", userID))

// User-wide changes (e.g., logout, settings change)
utils.CacheInvalidateUser(userID)
```

## Testing Cache

### Test Redis Connection

```bash
# In backend directory
go run main.go
```

Look for log output:
```
✅ Redis cache connected successfully at 100.86.12.111:6379 (DB: 0)
```

### Monitor Redis (Optional)

```bash
# Connect to Redis CLI
redis-cli -h 100.86.12.111 -p 6379

# Monitor all commands in real-time
MONITOR

# Check specific keys
KEYS user:*

# Get cache value
GET "user:123:summary"

# Check cache size
DBSIZE

# Flush all cache (careful!)
FLUSHDB
```

## Performance Tips

1. **Cache High-Traffic Endpoints**
   - Dashboard summary
   - Transaction lists
   - Account balances
   - Category lists

2. **Don't Cache Everything**
   - Skip caching for rarely accessed data
   - Skip caching for real-time requirements

3. **Use Appropriate TTLs**
   - Shorter TTL for financial data
   - Longer TTL for reference data

4. **Monitor Cache Hit Rate**
   - Log cache hits/misses during development
   - Adjust strategy based on metrics

## Troubleshooting

### Cache Not Working

1. Check `REDIS_ENABLED=true` in `.env`
2. Verify Redis server is running: `redis-cli ping`
3. Check network connectivity to Redis host
4. Review logs for connection errors

### Stale Data Issues

1. Ensure invalidation is called after data changes
2. Check cache key consistency
3. Reduce TTL for frequently changing data
4. Use `FLUSHDB` to clear all cache during development

### Performance Issues

1. Reduce payload size (don't cache huge objects)
2. Use shorter TTLs
3. Implement pagination before caching
4. Consider using Redis pub/sub for real-time invalidation

## Migration to Redis (Optional)

If you want to enable caching gradually:

1. Start with **read-only endpoints**
2. Add caching to **most-used endpoints** first
3. Implement **invalidation logic** carefully
4. **Monitor and measure** performance improvements
5. **Rollback** by setting `REDIS_ENABLED=false` if issues occur

---

**Questions?** Review `utils/redis.go` and `services/cache_example.go` for implementation details.
