package handlers

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dslipak/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"maybe-finance-backend/database"
	"maybe-finance-backend/middleware"
	"maybe-finance-backend/services"
	"maybe-finance-backend/utils"
)

// ParseFileImportHandler processes PDF/CSV file uploads, handles encryption/password checks,
// extracts text, and uses DeepSeek to parse transactions in bulk.
func ParseFileImportHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.HandleMethodNotAllowed(w)
		return
	}

	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		utils.HandleUnauthorized(w)
		return
	}

	// Check if AI is configured
	if !services.IsDeepSeekConfigured() {
		utils.JSONResponse(w, http.StatusOK, AIScanBulkResponse{
			OK:    false,
			Code:  "ai_disabled",
			Error: "Scan AI is not active. Please insert DEEPSEEK_API_KEY in your server environment file.",
		})
		return
	}

	// Parse multipart form (max 15MB)
	err := r.ParseMultipartForm(15 << 20)
	if err != nil {
		utils.HandleBadRequest(w, "Gagal memproses form data")
		return
	}

	// Get uploaded file
	file, fileHeader, err := r.FormFile("file")
	if err != nil {
		utils.HandleBadRequest(w, "File wajib disertakan")
		return
	}
	defer file.Close()

	password := r.FormValue("password")
	targetAccountID := r.FormValue("accountId")

	// Create a temporary file to work with pdfcpu and dslipak
	tempDir := os.TempDir()
	tempFile, err := os.CreateTemp(tempDir, "upload-*.tmp")
	if err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Gagal membuat file temporer")
		return
	}
	tempFilePath := tempFile.Name()
	defer os.Remove(tempFilePath)

	_, err = io.Copy(tempFile, file)
	tempFile.Close() // Close immediately to avoid Windows file sharing locks!
	if err != nil {
		utils.ErrorResponse(w, http.StatusInternalServerError, "Gagal menyalin file unggahan")
		return
	}

	// Determine file type
	fileName := strings.ToLower(fileHeader.Filename)
	isPDF := strings.HasSuffix(fileName, ".pdf")
	isCSV := strings.HasSuffix(fileName, ".csv") || strings.HasSuffix(fileName, ".txt")

	if !isPDF && !isCSV {
		utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
			"ok":    false,
			"code":  "invalid_file_type",
			"error": "Tipe file tidak didukung. Harap unggah file PDF atau CSV.",
		})
		return
	}

	var extractedText string

	if isPDF {
		// 1. Check if PDF is encrypted
		isEncrypted := false
		ctx, err := api.ReadContextFile(tempFilePath)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(strings.ToLower(errMsg), "password") ||
				strings.Contains(strings.ToLower(errMsg), "encrypt") ||
				strings.Contains(strings.ToLower(errMsg), "decrypt") ||
				strings.Contains(strings.ToLower(errMsg), "protected") {
				isEncrypted = true
			}
		} else if ctx != nil && ctx.XRefTable != nil && ctx.XRefTable.Encrypt != nil {
			isEncrypted = true
		}

		// Reset error variable to clear the ReadContextFile error
		err = nil

		// 2. Handle Encryption password flow
		var finalPDFPath string
		if isEncrypted {
			if password == "" {
				utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
					"ok":    false,
					"code":  "password_required",
					"error": "File PDF diproteksi oleh sandi. Harap masukkan sandi.",
				})
				return
			}

			// Try decrypting with password
			conf := model.NewDefaultConfiguration()
			conf.ValidationMode = model.ValidationRelaxed
			conf.UserPW = password
			conf.OwnerPW = password

			decryptedTemp, tempErr := os.CreateTemp(tempDir, "decrypted-*.pdf")
			if tempErr != nil {
				utils.ErrorResponse(w, http.StatusInternalServerError, "Gagal memproses file temporer dekripsi")
				return
			}
			decryptedTempPath := decryptedTemp.Name()
			decryptedTemp.Close() // Close immediately to avoid Windows file sharing locks!
			defer os.Remove(decryptedTempPath)

			tempErr = api.DecryptFile(tempFilePath, decryptedTempPath, conf)
			if tempErr != nil {
				utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
					"ok":    false,
					"code":  "invalid_password",
					"error": "Kata sandi yang dimasukkan salah.",
				})
				return
			}

			finalPDFPath = decryptedTempPath
			extractedText, err = extractPDFText(finalPDFPath)
		} else {
			finalPDFPath = tempFilePath
			extractedText, err = extractPDFText(finalPDFPath)
		}

		if err != nil {
			utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
				"ok":    false,
				"code":  "parse_pdf_failed",
				"error": fmt.Sprintf("Gagal mengekstrak teks dari PDF: %v", err),
			})
			return
		}

	} else if isCSV {
		// Read CSV/TXT content directly
		contentBytes, err := os.ReadFile(tempFilePath)
		if err != nil {
			utils.ErrorResponse(w, http.StatusInternalServerError, "Gagal membaca data mutasi CSV")
			return
		}
		extractedText = string(contentBytes)
	}

	// Clean/Truncate extracted text if it is too long (DeepSeek bulk scanner limits)
	extractedText = strings.TrimSpace(extractedText)
	if len(extractedText) < 10 {
		utils.JSONResponse(w, http.StatusOK, map[string]interface{}{
			"ok":    false,
			"code":  "empty_file",
			"error": "File tidak berisi teks transaksi yang cukup untuk diproses.",
		})
		return
	}

	if len(extractedText) > 40000 {
		// Truncate to 40,000 characters safely at a newline
		extractedText = extractedText[:40000]
		if lastIdx := strings.LastIndex(extractedText, "\n"); lastIdx > 35000 {
			extractedText = extractedText[:lastIdx]
		}
	}

	// Fetch user accounts and categories
	var accounts []database.FinanceAccount
	database.DB.Where("user_id = ? AND is_active = ?", userID, true).Find(&accounts)

	if len(accounts) == 0 {
		utils.JSONResponse(w, http.StatusOK, AIScanBulkResponse{
			OK:    false,
			Code:  "no_accounts",
			Error: "Tambahkan minimal satu akun aktif sebelum mengimpor transaksi.",
		})
		return
	}

	var categories []database.Category
	database.DB.Where("user_id IS NULL OR user_id = ?", userID).Find(&categories)

	// Identify target account details
	var targetAccount *database.FinanceAccount
	if targetAccountID != "" {
		for i := range accounts {
			if accounts[i].ID == targetAccountID {
				targetAccount = &accounts[i]
				break
			}
		}
	}

	// Compose lists for prompt
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

	// Prompt Engineering for PDF/CSV Parsing
	systemPrompt := "Anda adalah asisten parsing laporan keuangan berbahasa Indonesia. Tugas: ekstrak semua transaksi yang valid dari teks laporan bulanan, mutasi rekening, e-statement, file CSV, atau laporan PDF yang diberikan. Selalu jawab dengan satu objek JSON yang memiliki field \"transactions\" berupa array objek transaksi sesuai skema. Jangan tambahkan komentar atau teks di luar JSON."

	schemaHint := `{
  "transactions": [
    {
      "type": "income" | "expense" | "transfer",
      "amount": number (whole rupiah, tanpa pemisah),
      "date": "YYYY-MM-DD" | null,
      "description": "string singkat <= 80 char" | null,
      "note": "catatan tambahan seperti detail merchant, nomor transaksi, atau info transfer" | null,
      "accountId": "salah satu id dari daftar akun" | null,
      "transferToId": "id akun tujuan untuk transfer, null untuk income/expense",
      "categoryId": "id kategori income/expense yang cocok, null untuk transfer atau jika tidak yakin",
      "confidence": "angka 0..1",
      "reasoning": "kalimat pendek menjelaskan kesimpulan"
    }
  ]
}`

	var targetAccountPrompt string
	if targetAccount != nil {
		targetAccountPrompt = fmt.Sprintf("\nPENTING: Default `accountId` dari transaksi (kecuali jika terdeteksi transfer dari/ke akun lain secara jelas) harus diset ke ID akun target ini: \"%s\" (nama akun: %s).", targetAccount.ID, targetAccount.Name)
	}

	userPrompt := fmt.Sprintf(`
Hari ini: %s (gunakan tahun/bulan ini bila tanggal di teks kurang spesifik)

Daftar akun pengguna (pilih id-nya, JANGAN bikin baru):
%s
%s

Daftar kategori income (untuk type=income):
%s

Daftar kategori expense (untuk type=expense):
%s

Aturan:
- Jika transaksi menunjukkan uang masuk (gajian, transfer masuk, refund, bunga, setoran) → type=income.
- Jika menunjukkan pengeluaran/pembayaran (belanja, tarik tunai, biaya admin, transfer keluar ke pihak lain, tagihan) → type=expense.
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

Teks laporan/mutasi/CSV:
"""
%s
"""`, today, accountListStr, targetAccountPrompt, incomeCategoriesStr, expenseCategoriesStr, schemaHint, extractedText)

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

	// Call DeepSeek service
	err = services.DeepSeekJSON(r.Context(), systemPrompt, userPrompt, &raw)
	if err != nil {
		utils.JSONResponse(w, http.StatusOK, AIScanBulkResponse{
			OK:    false,
			Code:  "ai_failed",
			Error: "Gagal memproses dengan DeepSeek: " + err.Error(),
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
			// If we specified a target account, pre-fill accountId if null
			if sanitized.AccountID == nil && targetAccount != nil {
				sanitized.AccountID = &targetAccount.ID
			}
			candidates = append(candidates, sanitized)
		}
	}

	utils.JSONResponse(w, http.StatusOK, AIScanBulkResponse{
		OK:         true,
		Candidates: candidates,
	})
}

// Helper to extract plain text from PDF
func extractPDFText(filePath string) (string, error) {
	r, err := pdf.Open(filePath)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	b, err := r.GetPlainText()
	if err != nil {
		return "", err
	}
	_, err = buf.ReadFrom(b)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}
