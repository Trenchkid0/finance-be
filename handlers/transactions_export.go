package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/utils"
)

// ExportTransactionsHandler exports user transactions to CSV file
func ExportTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	// ✅ PERF: Stream rows via db.Rows() instead of loading all into memory
	rows, err := database.DB.Model(&database.Transaction{}).
		Select(`transactions.date, transactions.type, 
			acc_src.name as src_name,
			acc_dst.name as dst_name,
			categories.name as cat_name,
			transactions.amount,
			transactions.description,
			transactions.note`).
		Joins("LEFT JOIN categories ON categories.id = transactions.category_id").
		Joins("LEFT JOIN finance_accounts acc_src ON acc_src.id = transactions.account_id").
		Joins("LEFT JOIN finance_accounts acc_dst ON acc_dst.id = transactions.transfer_to_id").
		Where("transactions.user_id = ?", userID).
		Order("transactions.date desc").
		Rows()
	if err != nil {
		utils.HandleDBError(w, err, "retrieve transactions for export")
		return
	}
	defer rows.Close()

	// Set headers
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=transactions.csv")

	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{"Date", "Type", "Source Account", "Destination Account", "Category", "Amount", "Description", "Note"})

	for rows.Next() {
		var (
			txDate  time.Time
			txType  string
			srcName *string
			dstName *string
			catName *string
			amount  float64
			desc    string
			note    string
		)

		if err := rows.Scan(&txDate, &txType, &srcName, &dstName, &catName, &amount, &desc, &note); err != nil {
			continue
		}

		dest := ""
		if dstName != nil {
			dest = *dstName
		}
		cat := "Uncategorized"
		if catName != nil {
			cat = *catName
		}
		src := ""
		if srcName != nil {
			src = *srcName
		}

		record := []string{
			txDate.Format("2006-01-02"),
			txType,
			src,
			dest,
			cat,
			fmt.Sprintf("%.2f", amount),
			desc,
			note,
		}
		_ = writer.Write(record)
	}
}

// ExportTaxTransactionsHandler exports tax-deductible user transactions to CSV file
func ExportTaxTransactionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	// Stream rows of tax-deductible transactions
	rows, err := database.DB.Model(&database.Transaction{}).
		Select(`transactions.date, transactions.type, 
			acc_src.name as src_name,
			acc_dst.name as dst_name,
			categories.name as cat_name,
			transactions.amount,
			transactions.description,
			transactions.note`).
		Joins("LEFT JOIN categories ON categories.id = transactions.category_id").
		Joins("LEFT JOIN finance_accounts acc_src ON acc_src.id = transactions.account_id").
		Joins("LEFT JOIN finance_accounts acc_dst ON acc_dst.id = transactions.transfer_to_id").
		Where("transactions.user_id = ? AND (transactions.tax_deductible = ? OR (categories.id IS NOT NULL AND categories.tax_deductible = ?))", userID, true, true).
		Order("transactions.date desc").
		Rows()
	if err != nil {
		utils.HandleDBError(w, err, "retrieve tax transactions for export")
		return
	}
	defer rows.Close()

	// Set headers
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=tax_deductible_transactions.csv")

	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{"Date", "Type", "Source Account", "Destination Account", "Category", "Amount", "Description", "Note"})

	for rows.Next() {
		var (
			txDate  time.Time
			txType  string
			srcName *string
			dstName *string
			catName *string
			amount  float64
			desc    string
			note    string
		)

		if err := rows.Scan(&txDate, &txType, &srcName, &dstName, &catName, &amount, &desc, &note); err != nil {
			continue
		}

		dest := ""
		if dstName != nil {
			dest = *dstName
		}
		cat := "Uncategorized"
		if catName != nil {
			cat = *catName
		}
		src := ""
		if srcName != nil {
			src = *srcName
		}

		record := []string{
			txDate.Format("2006-01-02"),
			txType,
			src,
			dest,
			cat,
			fmt.Sprintf("%.2f", amount),
			desc,
			note,
		}
		_ = writer.Write(record)
	}
}
