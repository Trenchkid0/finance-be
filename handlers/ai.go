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

// AIScanHandler parses raw receipt / notification texts using DeepSeek
func AIScanHandler(w http.ResponseWriter, r *http.Request) {
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
  "note": "catatan tambahan seperti nomor receipt, detail merchant, atau deskripsi barang jika ada" | null,
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
- "note" diisi dengan catatan pendukung seperti nomor invoice, detail transaksi, barang belanjaan, atau nama toko lengkap.
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
		Note         *string     `json:"note"`
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

// AIStatusHandler returns whether the AI scan feature is enabled
func AIStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.HandleMethodNotAllowed(w)
		return
	}
	utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
		"enabled": services.IsDeepSeekConfigured(),
	})
}
