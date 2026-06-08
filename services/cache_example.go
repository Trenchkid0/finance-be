package services

import (
	"fmt"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/utils"
)

// Example: Caching user summary data
// This demonstrates how to use Redis cache in your handlers

const (
	// Cache expiration times
	CacheExpirationShort  = 5 * time.Minute   // For frequently changing data
	CacheExpirationMedium = 30 * time.Minute  // For moderately changing data
	CacheExpirationLong   = 2 * time.Hour     // For rarely changing data
)

// GetUserSummaryWithCache retrieves user summary with caching
func GetUserSummaryWithCache(userID string) (*database.User, error) {
	// 1. Try to get from cache first
	cacheKey := utils.BuildCacheKey("user", userID, "summary")
	var cachedUser database.User
	
	if err := utils.CacheGet(cacheKey, &cachedUser); err == nil {
		// Cache hit!
		return &cachedUser, nil
	}

	// 2. Cache miss - fetch from database
	var user database.User
	if err := database.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}

	// 3. Store in cache for next time
	_ = utils.CacheSet(cacheKey, user, CacheExpirationMedium)

	return &user, nil
}

// GetTransactionListWithCache retrieves transactions with caching
func GetTransactionListWithCache(userID string, filters map[string]interface{}) ([]database.Transaction, error) {
	// Build cache key based on filters
	cacheKey := buildTransactionCacheKey(userID, filters)
	
	var cachedTransactions []database.Transaction
	if err := utils.CacheGet(cacheKey, &cachedTransactions); err == nil {
		return cachedTransactions, nil
	}

	// Fetch from database
	query := database.DB.Where("user_id = ?", userID)
	
	// Apply filters...
	if accountID, ok := filters["accountId"].(string); ok && accountID != "" {
		query = query.Where("account_id = ?", accountID)
	}
	if txType, ok := filters["type"].(string); ok && txType != "" {
		query = query.Where("type = ?", txType)
	}
	
	var transactions []database.Transaction
	if err := query.Preload("Account").Preload("Category").Order("date DESC").Find(&transactions).Error; err != nil {
		return nil, err
	}

	// Cache the result
	_ = utils.CacheSet(cacheKey, transactions, CacheExpirationShort)

	return transactions, nil
}

// InvalidateTransactionCache invalidates transaction cache when data changes
func InvalidateTransactionCache(userID string) {
	pattern := utils.BuildCacheKey("transactions", userID, "*")
	_ = utils.CacheDeletePattern(pattern)
}

// InvalidateAccountCache invalidates account cache when data changes
func InvalidateAccountCache(userID string) {
	pattern := utils.BuildCacheKey("accounts", userID, "*")
	_ = utils.CacheDeletePattern(pattern)
}

// buildTransactionCacheKey builds a cache key for transaction queries
func buildTransactionCacheKey(userID string, filters map[string]interface{}) string {
	base := utils.BuildCacheKey("transactions", userID, "list")
	
	// Add filter parameters to make unique keys
	if accountID, ok := filters["accountId"].(string); ok && accountID != "" {
		base += fmt.Sprintf(":acc_%s", accountID)
	}
	if txType, ok := filters["type"].(string); ok && txType != "" {
		base += fmt.Sprintf(":type_%s", txType)
	}
	if limit, ok := filters["limit"].(int); ok && limit > 0 {
		base += fmt.Sprintf(":limit_%d", limit)
	}
	
	return base
}

// Example usage in your handlers:
//
// GET /api/transactions
// Instead of directly querying DB, use:
//   transactions, err := services.GetTransactionListWithCache(userID, filters)
//
// POST /api/transactions (after creating)
// Invalidate cache:
//   services.InvalidateTransactionCache(userID)
//   services.InvalidateAccountCache(userID)
//
// This ensures data consistency while improving performance
