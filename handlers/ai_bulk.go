package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/services"
	"maybe-finance-backend/utils"
)

type AIScanBulkResponse struct {
	OK         bool               `json:"ok"`
	Candidates []*AIScanCandidate `json:"candidates,omitempty"`
	Error      string             `json:"error,omitempty"`
	Code       string             `json:"code,omitempty"`
}

// BulkAIScanHandler parses raw financial report text (e.g. from PDF) using DeepSeek into multiple transaction candidates
func BulkAIScanHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	if !services.IsDeepSeekConfigured() {
		utils.JSONResponse(w, http.StatusOK, AIScanBulkResponse{
			OK:    false,
			Code:  "ai_disabled",
			Error: "Scan AI is not active. Please insert DEEPSEEK_API_KEY in your server environment file.",
		})
		return
	}

	var req AIScanRequest
	if err := utils.ParseJSON(r, &req); err != nil {
		utils.JSONResponse(w, http.StatusOK, AIScanBulkResponse{
			OK:    false,
			Code:  "invalid_input",
			Error: "Invalid request body",
		})
		return
	}

	inputText := strings.TrimSpace(req.Text)
	if len(inputText) < 10 {
		utils.JSONResponse(w, http.StatusOK, AIScanBulkResponse{
			OK:    false,
			Code:  "invalid_input",
			Error: "Text is too short. Please provide the financial report text.",
		})
		return
	}
	// Allow up to 40,000 characters for bulk reports
	if len(inputText) > 40000 {
		utils.JSONResponse(w, http.StatusOK, AIScanBulkResponse{
			OK:    false,
			Code:  "invalid_input",
			Error: "Text is too long (maximum 40,000 characters).",
		})
		return
	}

	// Fetch user accounts and categories
	var accounts []database.FinanceAccount
	database.DB.Where("user_id = ? AND is_active = ?", userID, true).Find(&accounts)

	if len(accounts) == 0 {
		utils.JSONResponse(w, http.StatusOK, AIScanBulkResponse{
			OK:    false,
			Code:  "no_accounts",
			Error: "Add at least one account/wallet before using Scan AI.",
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

	systemPrompt := "Anda adalah asisten parsing laporan keuangan berbahasa Indonesia. Tugas: ekstrak semua transaksi yang valid dari teks laporan bulanan, mutasi rekening, e-statement, atau laporan PDF yang diberikan. Selalu jawab dengan satu objek JSON yang memiliki field \"transactions\" berupa array objek transaksi sesuai skema. Jangan tambahkan komentar atau teks di luar JSON."

	schemaHint := `{
  "transactions": [
    {
      "type": "income" | "expense" | "transfer",
      "amount": number (whole rupiah, tanpa pemisah),
      "date": "YYYY-MM-DD" | null,
      "description": "string singkat <= 80 char" | null,
      "note": "catatan tambahan seperti detail merchant, deskripsi barang, atau info transfer" | null,
      "accountId": "salah satu id dari daftar akun" | null,
      "transferToId": "id akun tujuan untuk transfer, null untuk income/expense",
      "categoryId": "id kategori income/expense yang cocok, null untuk transfer atau jika tidak yakin",
      "confidence": "angka 0..1",
      "reasoning": "kalimat pendek menjelaskan kesimpulan"
    }
  ]
}`

	userPrompt := fmt.Sprintf(`
Hari ini: %s (gunakan tahun/bulan ini bila tanggal di teks kurang spesifik)

Daftar akun pengguna (pilih id-nya, JANGAN bikin baru):
%s

Daftar kategori income (untuk type=income):
%s

Daftar kategori expense (untuk type=expense):
%s

Aturan:
- Jika transaksi menunjukkan uang masuk (gajian, transfer masuk, refund, bunga) → type=income.
- Jika menunjukkan pengeluaran/pembayaran (belanja, tarik tunai, biaya admin, transfer keluar ke pihak lain) → type=expense.
- Jika menunjukkan pemindahan dana antar akun pengguna sendiri → type=transfer dan isi transferToId.
- "amount" wajib angka tanpa "Rp", titik, atau koma. Contoh: 125000 (BUKAN "Rp 125.000").
- Pilih accountId paling cocok berdasarkan nama/jenis (BCA, GoPay, OVO, Mandiri, Tunai, dll).
- Untuk income/expense, pilih categoryId yang relevan; isi null kalau tidak ada yang cocok.
- "date" harus format YYYY-MM-DD.
- "note" diisi dengan catatan pendukung seperti nomor invoice, detail transaksi, barang belanjaan, atau nama toko lengkap.
- Jika ragu pada salah satu field, isi null daripada menebak.
- Ekstrak seluruh transaksi yang Anda temukan di dalam teks secara berurutan.

Skema JSON yang harus Anda kembalikan:
%s

Teks laporan keuangan:
"""
%s
"""`, today, accountListStr, incomeCategoriesStr, expenseCategoriesStr, schemaHint, inputText)

	// Define raw parsing structure
	var raw struct {
		Transactions []struct {
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
		} `json:"transactions"`
	}

	err := services.DeepSeekJSON(r.Context(), systemPrompt, userPrompt, &raw)
	if err != nil {
		utils.JSONResponse(w, http.StatusOK, AIScanBulkResponse{
			OK:    false,
			Code:  "ai_failed",
			Error: err.Error(),
		})
		return
	}

	// Sanitize output candidates
	var candidates []*AIScanCandidate
	for _, item := range raw.Transactions {
		candRaw := struct {
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
		}{
			Type:         item.Type,
			Amount:       item.Amount,
			Date:         item.Date,
			Description:  item.Description,
			Note:         item.Note,
			AccountID:    item.AccountID,
			TransferToID: item.TransferToID,
			CategoryID:   item.CategoryID,
			Confidence:   item.Confidence,
			Reasoning:    item.Reasoning,
		}

		sanitized := sanitizeCandidate(candRaw, accounts, categories)
		if sanitized != nil {
			candidates = append(candidates, sanitized)
		}
	}

	utils.JSONResponse(w, http.StatusOK, AIScanBulkResponse{
		OK:         true,
		Candidates: candidates,
	})
}
