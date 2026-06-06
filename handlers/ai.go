package handlers

import (
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/services"
	"maybe-finance-backend/utils"
)

type AIScanRequest struct {
	Text string `json:"text"`
}

type AIScanCandidate struct {
	Type         database.TransactionType `json:"type"`
	Amount       float64                  `json:"amount"`
	Date         *string                  `json:"date"` // YYYY-MM-DD
	Description  *string                  `json:"description"`
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

// AIScanHandler parses raw receipt / notification texts using DeepSeek
func AIScanHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.ErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.ErrorResponse(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if !services.IsDeepSeekConfigured() {
		utils.JSONResponse(w, http.StatusOK, AIScanResponse{
			OK:    false,
			Code:  "ai_disabled",
			Error: "Scan AI belum aktif. Masukkan DEEPSEEK_API_KEY di file environment server Anda.",
		})
		return
	}

	var req AIScanRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.JSONResponse(w, http.StatusOK, AIScanResponse{
			OK:    false,
			Code:  "invalid_input",
			Error: "Invalid request body",
		})
		return
	}

	inputText := strings.TrimSpace(req.Text)
	if len(inputText) < 10 {
		utils.JSONResponse(w, http.StatusOK, AIScanResponse{
			OK:    false,
			Code:  "invalid_input",
			Error: "Teks terlalu pendek. Tempel minimal 1 baris struk atau notifikasi.",
		})
		return
	}
	if len(inputText) > 4000 {
		utils.JSONResponse(w, http.StatusOK, AIScanResponse{
			OK:    false,
			Code:  "invalid_input",
			Error: "Teks terlalu panjang (maksimal 4000 karakter).",
		})
		return
	}

	// Fetch user accounts and categories
	var accounts []database.FinanceAccount
	database.DB.Where("user_id = ? AND is_active = ?", userID, true).Find(&accounts)

	if len(accounts) == 0 {
		utils.JSONResponse(w, http.StatusOK, AIScanResponse{
			OK:    false,
			Code:  "no_accounts",
			Error: "Tambahkan minimal satu rekening/dompet sebelum memakai Scan AI.",
		})
		return
	}

	var categories []database.Category
	database.DB.Where("user_id IS NULL OR user_id = ?", userID).Find(&categories)

	// Compose prompt
	var accList []string
	for _, a := range accounts {
		accList = append(accList, fmt.Sprintf("- id=\"%s\" name=\"%s\" type=%s", a.ID, a.Name, a.Type))
	}
	accountListStr := strings.Join(accList, "\n")

	var incomeCats []string
	var expenseCats []string
	for _, c := range categories {
		if c.Type == database.CategoryTypeIncome {
			incomeCats = append(incomeCats, fmt.Sprintf("- id=\"%s\" name=\"%s\"", c.ID, c.Name))
		} else {
			expenseCats = append(expenseCats, fmt.Sprintf("- id=\"%s\" name=\"%s\"", c.ID, c.Name))
		}
	}
	incomeCategoriesStr := strings.Join(incomeCats, "\n")
	expenseCategoriesStr := strings.Join(expenseCats, "\n")

	today := time.Now().Format("2006-01-02")

	systemPrompt := "Anda adalah asisten parsing transaksi keuangan berbahasa Indonesia. Tugas: ekstrak satu transaksi dari teks bebas (struk belanja, notifikasi SMS bank, screenshot e-wallet, catatan harian). Selalu jawab dengan satu objek JSON yang valid sesuai skema. Jangan tambahkan komentar atau teks di luar JSON."

	schemaHint := `{
  "type": "income" | "expense" | "transfer",
  "amount": number (whole rupiah, tanpa pemisah),
  "date": "YYYY-MM-DD" | null,
  "description": "string singkat <= 80 char" | null,
  "accountId": "salah satu id dari daftar akun" | null,
  "transferToId": "id akun tujuan untuk transfer, null untuk income/expense",
  "categoryId": "id kategori income/expense yang cocok, null untuk transfer atau jika tidak yakin",
  "confidence": "angka 0..1",
  "reasoning": "kalimat pendek menjelaskan kesimpulan"
}`

	userPrompt := fmt.Sprintf(`
Hari ini: %s (gunakan ini bila tanggal tidak disebut di teks)

Daftar akun pengguna (pilih id-nya, JANGAN bikin baru):
%s

Daftar kategori income (untuk type=income):
%s

Daftar kategori expense (untuk type=expense):
%s

Aturan:
- Jika teks menunjukkan uang masuk (gajian, pemasukan, terima dari, refund) → type=income.
- Jika teks menunjukkan pembayaran/pengeluaran (belanja, top-up, bayar tagihan) → type=expense.
- Jika menunjukkan pemindahan dana antar akun pengguna sendiri → type=transfer dan isi transferToId.
- "amount" wajib angka tanpa "Rp", titik, atau koma. Contoh: 125000 (BUKAN "Rp 125.000").
- Pilih accountId paling cocok berdasarkan nama/jenis (BCA, GoPay, OVO, Mandiri, Tunai, dll).
- Untuk income/expense, pilih categoryId yang relevan; isi null kalau tidak ada yang cocok.
- "date" harus format YYYY-MM-DD; kalau teks bilang "kemarin", "hari ini", dll., hitung dari hari ini.
- Jika ragu pada salah satu field, isi null daripada menebak.
- "confidence" 0..1 — jujur. Kalau teks ambigu, set < 0.5.

Skema JSON yang harus Anda kembalikan:
%s

Teks transaksi:
"""
%s
"""`, today, accountListStr, incomeCategoriesStr, expenseCategoriesStr, schemaHint, inputText)

	// Call DeepSeek
	var raw struct {
		Type         string      `json:"type"`
		Amount       interface{} `json:"amount"`
		Date         *string     `json:"date"`
		Description  *string     `json:"description"`
		AccountID    *string     `json:"accountId"`
		TransferToID *string     `json:"transferToId"`
		CategoryID   *string     `json:"categoryId"`
		Confidence   interface{} `json:"confidence"`
		Reasoning    *string     `json:"reasoning"`
	}

	err := services.DeepSeekJSON(r.Context(), systemPrompt, userPrompt, &raw)
	if err != nil {
		utils.JSONResponse(w, http.StatusOK, AIScanResponse{
			OK:    false,
			Code:  "ai_failed",
			Error: err.Error(),
		})
		return
	}

	// Sanitize output
	candidate := sanitizeCandidate(raw, accounts, categories)
	if candidate == nil {
		utils.JSONResponse(w, http.StatusOK, AIScanResponse{
			OK:    false,
			Code:  "unrecognized",
			Error: "AI gagal mengenali pola transaksi yang valid dari teks tersebut.",
		})
		return
	}

	utils.JSONResponse(w, http.StatusOK, AIScanResponse{
		OK:        true,
		Candidate: candidate,
	})
}

// sanitizeCandidate normalizes raw AI outputs to prevent malicious/invalid records.
func sanitizeCandidate(
	raw struct {
		Type         string      `json:"type"`
		Amount       interface{} `json:"amount"`
		Date         *string     `json:"date"`
		Description  *string     `json:"description"`
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
		AccountID:    accountID,
		TransferToID: transferToID,
		CategoryID:   categoryID,
		Confidence:   confidence,
		Reasoning:    reasoning,
	}
}
