package handlers

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"maybe-finance-backend/database"
)

type AIScanRequest struct {
	Text string `json:"text"`
}

type AIScanCandidate struct {
	Type         database.TransactionType `json:"type"`
	Amount       float64                  `json:"amount"`
	Date         *string                  `json:"date"` // YYYY-MM-DD
	Description  *string                  `json:"description"`
	Note         *string                  `json:"note"`
	AccountID    *string                  `json:"accountId"`
	TransferToID *string                  `json:"transferToId"`
	CategoryID   *string                  `json:"categoryId"`
	Confidence   float64                  `json:"confidence"`
	Reasoning    *string                  `json:"reasoning"`
}

type AIScanResponse struct {
	OK        bool             `json:"ok"`
	Candidate *AIScanCandidate `json:"candidate,omitempty"`
	Error     string           `json:"error,omitempty"`
	Code      string           `json:"code,omitempty"`
}

// sanitizeCandidate normalizes raw AI outputs to prevent malicious/invalid records.
func sanitizeCandidate(
	raw struct {
		Type         string      `json:"type"`
		Amount       interface{} `json:"amount"`
		Date         *string     `json:"date"`
		Description  *string     `json:"description"`
		Note         *string     `json:"note"`
		AccountID    *string     `json:"accountId"`
		TransferToID *string     `json:"transferToId"`
		CategoryID   *string     `json:"categoryId"`
		Confidence   interface{} `json:"confidence"`
		Reasoning    *string     `json:"reasoning"`
	},
	accounts []database.FinanceAccount,
	categories []database.Category,
) *AIScanCandidate {
	// 1. Type
	txType := database.TransactionType(strings.ToLower(raw.Type))
	if txType != database.TransactionTypeIncome && txType != database.TransactionTypeExpense && txType != database.TransactionTypeTransfer {
		return nil
	}

	// 2. Amount
	amount := 0.0
	switch v := raw.Amount.(type) {
	case float64:
		amount = math.Abs(v)
	case int:
		amount = math.Abs(float64(v))
	case string:
		// Remove non-numeric
		reg := regexp.MustCompile(`[^\d.-]`)
		cleaned := reg.ReplaceAllString(v, "")
		// Strip dots if thousands sep
		cleaned = strings.ReplaceAll(cleaned, ".", "")
		if parsed, err := strconv.ParseFloat(cleaned, 64); err == nil {
			amount = math.Abs(parsed)
		}
	}
	if amount <= 0 {
		return nil
	}

	// 3. Date
	var dateStr *string
	if raw.Date != nil {
		reg := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
		if reg.MatchString(*raw.Date) {
			if _, err := time.Parse("2006-01-02", *raw.Date); err == nil {
				dateStr = raw.Date
			}
		}
	}

	// 4. Description
	var description *string
	if raw.Description != nil {
		trimmed := strings.TrimSpace(*raw.Description)
		if len(trimmed) > 0 {
			if len(trimmed) > 80 {
				trimmed = trimmed[:80]
			}
			description = &trimmed
		}
	}

	// Note
	var note *string
	if raw.Note != nil {
		trimmed := strings.TrimSpace(*raw.Note)
		if len(trimmed) > 0 {
			note = &trimmed
		}
	}

	// 5. Account validation
	accountIDs := make(map[string]bool)
	for _, a := range accounts {
		accountIDs[a.ID] = true
	}

	var accountID *string
	if raw.AccountID != nil && accountIDs[*raw.AccountID] {
		accountID = raw.AccountID
	}

	var transferToID *string
	if txType == database.TransactionTypeTransfer && raw.TransferToID != nil && accountIDs[*raw.TransferToID] {
		transferToID = raw.TransferToID
	}
	if transferToID != nil && accountID != nil && *transferToID == *accountID {
		transferToID = nil
	}

	// 6. Category validation
	var categoryID *string
	if txType != database.TransactionTypeTransfer && raw.CategoryID != nil {
		catMap := make(map[string]database.Category)
		for _, c := range categories {
			if string(c.Type) == string(txType) {
				catMap[c.ID] = c
			}
		}
		if _, found := catMap[*raw.CategoryID]; found {
			categoryID = raw.CategoryID
		}
	}

	// 7. Confidence
	confidence := 0.5
	switch v := raw.Confidence.(type) {
	case float64:
		if v >= 0 && v <= 1 {
			confidence = v
		}
	case string:
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed >= 0 && parsed <= 1 {
			confidence = parsed
		}
	}

	// 8. Reasoning
	var reasoning *string
	if raw.Reasoning != nil {
		trimmed := strings.TrimSpace(*raw.Reasoning)
		if len(trimmed) > 0 {
			if len(trimmed) > 200 {
				trimmed = trimmed[:200]
			}
			reasoning = &trimmed
		}
	}

	return &AIScanCandidate{
		Type:         txType,
		Amount:       amount,
		Date:         dateStr,
		Description:  description,
		Note:         note,
		AccountID:    accountID,
		TransferToID: transferToID,
		CategoryID:   categoryID,
		Confidence:   confidence,
		Reasoning:    reasoning,
	}
}
