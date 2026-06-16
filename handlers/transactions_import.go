package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/services"
	"maybe-finance-backend/utils"
)

// ImportTransactionsHandler imports transactions from a CSV file
func ImportTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	// Parse multipart form (max 10MB)
	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		utils.HandleBadRequest(w, "Failed to parse form data")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		utils.HandleBadRequest(w, "File is required")
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		utils.HandleBadRequest(w, "Failed to parse CSV file")
		return
	}

	if len(records) < 2 {
		utils.HandleBadRequest(w, "CSV file is empty or has no data rows")
		return
	}

	// Expected CSV format: Date, Type, Source Account, Destination Account, Category, Amount, Description, Note
	// Skip header row
	header := records[0]
	if len(header) < 6 {
		utils.HandleBadRequest(w, "Invalid CSV format: missing required columns")
		return
	}

	// PERF: Preload all accounts and categories into maps for O(1) lookup during import
	var allAccounts []database.FinanceAccount
	database.DB.Where("user_id = ?", userID).Find(&allAccounts)
	accByName := make(map[string]*database.FinanceAccount, len(allAccounts))
	for i := range allAccounts {
		accByName[allAccounts[i].Name] = &allAccounts[i]
	}

	var allCategories []database.Category
	database.DB.Where("user_id = ? OR user_id IS NULL", userID).Find(&allCategories)
	catByName := make(map[string]*database.Category, len(allCategories))
	for i := range allCategories {
		catByName[allCategories[i].Name] = &allCategories[i]
	}

	tx := database.DB.Begin()
	imported := 0
	errors := []string{}
	var batchTx []database.Transaction // collect for batch insert

	for i, record := range records[1:] {
		if len(record) < 6 {
			errors = append(errors, fmt.Sprintf("Row %d: insufficient columns", i+2))
			continue
		}

		// Parse date
		dateStr := record[0]
		parsedDate := time.Now()
		if dateStr != "" {
			if parsed, err := time.Parse("2006-01-02", dateStr); err == nil {
				parsedDate = parsed
			} else if parsed, err := time.Parse("02/01/2006", dateStr); err == nil {
				parsedDate = parsed
			} else if parsed, err := time.Parse("01/02/2006", dateStr); err == nil {
				parsedDate = parsed
			}
		}

		// Parse type
		typeStr := record[1]
		var txType database.TransactionType
		switch strings.ToLower(strings.TrimSpace(typeStr)) {
		case "income", "pemasukan":
			txType = database.TransactionTypeIncome
		case "expense", "pengeluaran":
			txType = database.TransactionTypeExpense
		case "transfer":
			txType = database.TransactionTypeTransfer
		default:
			errors = append(errors, fmt.Sprintf("Row %d: invalid transaction type '%s'", i+2, typeStr))
			continue
		}

		// Find source account by name (PERF: O(1) map lookup instead of DB query per row)
		accountName := strings.TrimSpace(record[2])
		sourceAccPtr, found := accByName[accountName]
		if !found {
			errors = append(errors, fmt.Sprintf("Row %d: account '%s' not found", i+2, accountName))
			continue
		}
		sourceAcc := *sourceAccPtr

		// Find destination account for transfers (PERF: O(1) map lookup)
		var transferToID *string
		if txType == database.TransactionTypeTransfer && len(record) > 3 {
			destName := strings.TrimSpace(record[3])
			if destName != "" {
				if destPtr, ok := accByName[destName]; ok {
					transferToID = &destPtr.ID
				} else {
					errors = append(errors, fmt.Sprintf("Row %d: destination account '%s' not found", i+2, destName))
					continue
				}
			}
		}

		// Find category by name (PERF: O(1) map lookup)
		var categoryID *string
		if len(record) > 4 {
			categoryName := strings.TrimSpace(record[4])
			if categoryName != "" && categoryName != "Uncategorized" {
				if cat, ok := catByName[categoryName]; ok {
					categoryID = &cat.ID
				}
			}
		}

		// Parse amount
		if len(record) < 6 {
			errors = append(errors, fmt.Sprintf("Row %d: missing amount", i+2))
			continue
		}
		amountStr := strings.TrimSpace(record[5])
		amount, err := strconv.ParseFloat(strings.ReplaceAll(amountStr, ",", ""), 64)
		if err != nil || amount <= 0 {
			errors = append(errors, fmt.Sprintf("Row %d: invalid amount '%s'", i+2, amountStr))
			continue
		}

		// Parse description and note
		description := ""
		if len(record) > 6 {
			description = strings.TrimSpace(record[6])
		}
		note := ""
		if len(record) > 7 {
			note = strings.TrimSpace(record[7])
		}

		// Reconcile balance
		if err := services.AdjustBalances(tx, userID, sourceAcc.ID, transferToID, txType, amount, 0, 1); err != nil {
			errors = append(errors, fmt.Sprintf("Row %d: failed to update balance", i+2))
			continue
		}

		// Create transaction
		transaction := database.Transaction{
			UserID:       userID,
			AccountID:    sourceAcc.ID,
			CategoryID:   categoryID,
			Type:         txType,
			Amount:       amount,
			Description:  description,
			Note:         note,
			Date:         parsedDate,
			TransferToID: transferToID,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		batchTx = append(batchTx, transaction)
		imported++
	}

	if imported == 0 {
		tx.Rollback()
		utils.HandleBadRequest(w, fmt.Sprintf("No transactions imported. Errors: %v", errors))
		return
	}

	// PERF: Batch insert all transactions at once
	if err := tx.CreateInBatches(&batchTx, 50).Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "insert transactions")
		return
	}

	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		utils.HandleDBError(w, err, "commit transactions")
		return
	}

	// Invalidate cache
	_ = utils.CacheInvalidateUser(userID)
	fmt.Printf("Cache invalidated for user: %s (%d transactions imported)\n", userID, imported)

	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"imported": imported,
		"errors":   errors,
		"total":    len(records) - 1,
	})
}
