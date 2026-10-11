package services

import (
	"crypto/tls"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"maybe-finance-backend/database"
	"maybe-finance-backend/utils"
)

// ParsedTransactionResult holds extracted transaction metadata from an email
type ParsedTransactionResult struct {
	MessageID   string
	Subject     string
	Sender      string
	BankName    string
	Type        database.TransactionType
	Amount      float64
	Description string
	Merchant    string
	Date        time.Time
}

// TestIMAPConnection validates if the given IMAP credentials work
func TestIMAPConnection(serverAddr, emailAddr, appPassword string) error {
	if serverAddr == "" {
		serverAddr = "imap.gmail.com:993"
	}

	// Connect with TLS
	c, err := client.DialTLS(serverAddr, &tls.Config{InsecureSkipVerify: false})
	if err != nil {
		return fmt.Errorf("gagal terhubung ke server IMAP (%s): %w", serverAddr, err)
	}
	defer c.Logout()

	// Clean password from spaces (Google app password is often shown as 4x4 with spaces)
	cleanPass := strings.ReplaceAll(appPassword, " ", "")

	// Login
	if err := c.Login(emailAddr, cleanPass); err != nil {
		return fmt.Errorf("login IMAP gagal. Pastikan alamat email & Google App Password (16 digit) benar: %w", err)
	}

	// Try selecting INBOX
	_, err = c.Select("INBOX", true)
	if err != nil {
		return fmt.Errorf("berhasil login, namun gagal membuka INBOX: %w", err)
	}

	return nil
}

// EmailSyncOptions defines filter and behavior options for bank email syncing
type EmailSyncOptions struct {
	Days         int
	StartDate    string // format "YYYY-MM-DD"
	EndDate      string // format "YYYY-MM-DD"
	ResetHistory bool
}

// SyncUserEmails connects to IMAP, searches recent emails within specified date range or days,
// parses financial emails, and records transactions.
func SyncUserEmails(userID string, opts EmailSyncOptions) (int, error) {
	if opts.ResetHistory {
		database.DB.Where("user_id = ?", userID).Delete(&database.ProcessedEmail{})
		database.DB.Model(&database.EmailSyncConfig{}).Where("user_id = ?", userID).Update("total_parsed", 0)
	}

	var cfg database.EmailSyncConfig
	if err := database.DB.Where("user_id = ? AND is_enabled = ?", userID, true).First(&cfg).Error; err != nil {
		return 0, fmt.Errorf("konfigurasi email sync tidak ditemukan atau non-aktif: %w", err)
	}

	// Update status to syncing
	database.DB.Model(&cfg).Updates(map[string]interface{}{
		"last_sync_status": "syncing",
		"last_sync_error":  "",
	})

	serverAddr := cfg.IMAPServer
	if serverAddr == "" {
		serverAddr = "imap.gmail.com:993"
	}

	cleanPass := strings.ReplaceAll(cfg.AppPassword, " ", "")

	// Connect to IMAP
	c, err := client.DialTLS(serverAddr, &tls.Config{InsecureSkipVerify: false})
	if err != nil {
		failErr := fmt.Errorf("koneksi IMAP gagal: %w", err)
		database.DB.Model(&cfg).Updates(map[string]interface{}{
			"last_sync_status": "failed",
			"last_sync_error":  failErr.Error(),
		})
		return 0, failErr
	}
	defer c.Logout()

	if err := c.Login(cfg.Email, cleanPass); err != nil {
		failErr := fmt.Errorf("otentikasi IMAP gagal: %w", err)
		database.DB.Model(&cfg).Updates(map[string]interface{}{
			"last_sync_status": "failed",
			"last_sync_error":  failErr.Error(),
		})
		return 0, failErr
	}

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		failErr := fmt.Errorf("buka INBOX gagal: %w", err)
		database.DB.Model(&cfg).Updates(map[string]interface{}{
			"last_sync_status": "failed",
			"last_sync_error":  failErr.Error(),
		})
		return 0, failErr
	}

	if mbox.Messages == 0 {
		now := time.Now()
		database.DB.Model(&cfg).Updates(map[string]interface{}{
			"last_sync_status": "success",
			"last_synced_at":   &now,
		})
		return 0, nil
	}

	// Build search criteria using date range if provided
	criteria := imap.NewSearchCriteria()
	if strings.TrimSpace(opts.StartDate) != "" {
		if tStart, err := time.Parse("2006-01-02", strings.TrimSpace(opts.StartDate)); err == nil {
			criteria.Since = tStart
		}
	}
	if strings.TrimSpace(opts.EndDate) != "" {
		if tEnd, err := time.Parse("2006-01-02", strings.TrimSpace(opts.EndDate)); err == nil {
			// In IMAP RFC 3501, SEARCH BEFORE matches messages strictly before the date,
			// so add 1 day to make the end date inclusive.
			criteria.Before = tEnd.AddDate(0, 0, 1)
		}
	}

	// Fallback to days if StartDate not given or invalid
	if criteria.Since.IsZero() {
		days := opts.Days
		if days <= 0 {
			days = 30
		}
		criteria.Since = time.Now().AddDate(0, 0, -days)
	}

	uids, err := c.Search(criteria)
	if err != nil || len(uids) == 0 {
		// Fallback: check up to 250 most recent messages by sequence
		from := uint32(1)
		if mbox.Messages > 250 {
			from = mbox.Messages - 249
		}
		seqset := new(imap.SeqSet)
		seqset.AddRange(from, mbox.Messages)
		return processSeqMessages(c, seqset, userID, &cfg, opts)
	}

	// Scan up to 300 UIDs for comprehensive date range coverage
	if len(uids) > 300 {
		uids = uids[len(uids)-300:]
	}

	seqset := new(imap.SeqSet)
	seqset.AddNum(uids...)

	return processUIDMessages(c, seqset, userID, &cfg, opts)
}

func processUIDMessages(c *client.Client, seqset *imap.SeqSet, userID string, cfg *database.EmailSyncConfig, opts EmailSyncOptions) (int, error) {
	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchFlags, imap.FetchInternalDate, section.FetchItem()}

	messages := make(chan *imap.Message, 40)
	done := make(chan error, 1)

	go func() {
		done <- c.UidFetch(seqset, items, messages)
	}()

	return consumeAndParseMessages(messages, done, userID, cfg, opts)
}

func processSeqMessages(c *client.Client, seqset *imap.SeqSet, userID string, cfg *database.EmailSyncConfig, opts EmailSyncOptions) (int, error) {
	section := &imap.BodySectionName{}
	items := []imap.FetchItem{imap.FetchEnvelope, imap.FetchFlags, imap.FetchInternalDate, section.FetchItem()}

	messages := make(chan *imap.Message, 40)
	done := make(chan error, 1)

	go func() {
		done <- c.Fetch(seqset, items, messages)
	}()

	return consumeAndParseMessages(messages, done, userID, cfg, opts)
}

func consumeAndParseMessages(messages chan *imap.Message, done chan error, userID string, cfg *database.EmailSyncConfig, opts EmailSyncOptions) (int, error) {
	importedCount := 0

	// Pre-load user accounts and categories for auto-matching
	var accounts []database.FinanceAccount
	database.DB.Where("user_id = ? AND is_active = ?", userID, true).Find(&accounts)

	var categories []database.Category
	database.DB.Where("user_id = ? OR is_default = ?", userID, true).Find(&categories)

	for msg := range messages {
		if msg == nil || msg.Envelope == nil {
			continue
		}

		msgID := msg.Envelope.MessageId
		if msgID == "" {
			msgID = fmt.Sprintf("msg_%s_%d", msg.Envelope.Subject, msg.InternalDate.Unix())
		}

		// 1. Idempotency check: has this email already been parsed?
		var exists int64
		database.DB.Model(&database.ProcessedEmail{}).
			Where("user_id = ? AND message_id = ?", userID, msgID).
			Count(&exists)
		if exists > 0 {
			continue
		}

		// 2. Filter sender / subject relevant to Indonesian financial institutions
		sender := ""
		if len(msg.Envelope.From) > 0 {
			sender = strings.ToLower(msg.Envelope.From[0].Address())
		}
		subject := msg.Envelope.Subject

		if !isFinancialEmail(sender, subject) {
			continue
		}

		// 3. Extract body text
		bodyText := ""
		for _, literal := range msg.Body {
			if literal == nil {
				continue
			}
			mr, err := mail.CreateReader(literal)
			if err != nil {
				// Fallback to raw bytes
				buf := new(strings.Builder)
				io.Copy(buf, literal)
				bodyText = buf.String()
				break
			}

			// Traverse parts
			for {
				p, err := mr.NextPart()
				if err == io.EOF {
					break
				} else if err != nil {
					break
				}

				contentType := ""
				switch h := p.Header.(type) {
				case *mail.InlineHeader:
					contentType = h.Get("Content-Type")
				case *mail.AttachmentHeader:
					contentType = h.Get("Content-Type")
				}
				if contentType == "" || strings.Contains(contentType, "text/plain") || strings.Contains(contentType, "text/html") {
					b, err := io.ReadAll(p.Body)
					if err == nil {
						bodyText += " " + string(b)
					}
				}
			}
		}

		if bodyText == "" {
			bodyText = subject
		} else {
			bodyText = cleanHTMLAndEntities(bodyText)
		}

		// 4. Parse transaction details from body & subject
		bestDate := msg.InternalDate
		if msg.Envelope != nil && !msg.Envelope.Date.IsZero() {
			bestDate = msg.Envelope.Date
		}
		parsed := ParseFinancialEmail(sender, subject, bodyText, bestDate)
		if parsed == nil || parsed.Amount <= 0 {
			continue
		}

		// 4b. Strict Date Range Check: Ensure transaction is within requested date range
		txDateStr := parsed.Date.Format("2006-01-02")
		cleanStart := strings.TrimSpace(opts.StartDate)
		cleanEnd := strings.TrimSpace(opts.EndDate)
		if cleanStart != "" && txDateStr < cleanStart {
			continue // Skip: transaction date is before StartDate
		}
		if cleanEnd != "" && txDateStr > cleanEnd {
			continue // Skip: transaction date is after EndDate
		}

		parsed.MessageID = msgID

		// 5. Match Account
		accountID := matchAccount(parsed, accounts, cfg.DefaultAccountID)
		if accountID == "" {
			// Skip if no account can be linked
			continue
		}

		// 6. Match Category
		var categoryID *string
		if cat := matchCategory(parsed, categories); cat != nil {
			categoryID = &cat.ID
		}

		// 7. Insert Transaction with Balance Adjustment in DB transaction
		err := database.DB.Transaction(func(tx *gorm.DB) error {
			// Adjust Account Balance
			adminFee := 0.0
			if err := AdjustBalances(tx, userID, accountID, nil, parsed.Type, parsed.Amount, adminFee, 1); err != nil {
				return err
			}

			// Create Transaction
			desc := parsed.Description
			if desc == "" {
				desc = fmt.Sprintf("Transaksi %s", parsed.BankName)
			}
			note := fmt.Sprintf("Sinkronisasi otomatis dari email (%s)", parsed.Subject)

			newTx := database.Transaction{
				ID:          uuid.New().String(),
				UserID:      userID,
				AccountID:   accountID,
				CategoryID:  categoryID,
				Type:        parsed.Type,
				Amount:      parsed.Amount,
				AdminFee:    0,
				Description: desc,
				Note:        note,
				Date:        parsed.Date,
			}

			if err := tx.Create(&newTx).Error; err != nil {
				return err
			}

			// Record processed email
			proc := database.ProcessedEmail{
				ID:        uuid.New().String(),
				UserID:    userID,
				MessageID: parsed.MessageID,
				Subject:   parsed.Subject,
				Sender:    parsed.Sender,
				Amount:    parsed.Amount,
				CreatedAt: time.Now(),
			}

			return tx.Create(&proc).Error
		})

		if err == nil {
			importedCount++
		}
	}

	if err := <-done; err != nil {
		utils.Log.Warn().Err(err).Msg("IMAP fetch finished with error")
	}

	// Update config summary
	now := time.Now()
	database.DB.Model(cfg).Updates(map[string]interface{}{
		"last_sync_status": "success",
		"last_synced_at":   &now,
		"last_sync_error":  "",
		"total_parsed":     cfg.TotalParsed + importedCount,
	})

	// Create In-App Notification if any transactions were imported
	if importedCount > 0 {
		notif := database.Notification{
			ID:        uuid.New().String(),
			UserID:    userID,
			Title:     "📧 Sinkronisasi Email Berhasil",
			Message:   fmt.Sprintf("Berhasil mencatat otomatis %d transaksi baru dari email bank!", importedCount),
			Type:      "info",
			IsRead:    false,
			CreatedAt: time.Now(),
		}
		database.DB.Create(&notif)
	}

	return importedCount, nil
}

// isFinancialEmail detects if sender or subject matches Indonesian banks & e-wallets
func isFinancialEmail(sender, subject string) bool {
	s := strings.ToLower(sender)
	sub := strings.ToLower(subject)

	bankKeywords := []string{
		"bca", "klikbca", "mandiri", "livin", "bri", "brimo", "bni", "cimb", "niaga",
		"jago", "jenius", "btpn", "permata", "danamon", "seabank", "bank",
		"gopay", "gojek", "shopee", "shopeepay", "ovo", "dana", "linkaja", "grab",
		"tokopedia", "transfer", "mutasi", "debit", "kredit", "rekening", "qris",
		"transaksi", "pembayaran", "payment", "receipt", "struk", "notifikasi",
	}

	for _, kw := range bankKeywords {
		if strings.Contains(s, kw) || strings.Contains(sub, kw) {
			return true
		}
	}
	return false
}

// cleanHTMLAndEntities strips HTML tags and decodes entities into clean plain text
func cleanHTMLAndEntities(raw string) string {
	s := raw
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&#160;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")

	// Replace HTML break/cell/row tags with space and newline so table text does not concatenate
	reBreaks := regexp.MustCompile(`(?i)<(?:br|/p|/tr|/div|/td|/th)[^>]*>`)
	s = reBreaks.ReplaceAllString(s, " \n ")

	// Strip remaining HTML tags
	reTags := regexp.MustCompile(`<[^>]*>`)
	s = reTags.ReplaceAllString(s, " ")

	// Normalize repeated spaces
	reSpaces := regexp.MustCompile(`[ \t]+`)
	s = reSpaces.ReplaceAllString(s, " ")

	return strings.TrimSpace(s)
}

// ParseFinancialEmail extracts structured transaction data from Indonesian bank notification formats
func ParseFinancialEmail(sender, subject, body string, fallbackDate time.Time) *ParsedTransactionResult {
	text := body + " " + subject
	lowerText := strings.ToLower(text)

	res := &ParsedTransactionResult{
		Subject: subject,
		Sender:  sender,
		Date:    fallbackDate,
		Type:    database.TransactionTypeExpense, // Default to expense
	}

	// Try extracting explicit transaction date from email text if present
	if explicitDate := extractTransactionDate(text); !explicitDate.IsZero() {
		res.Date = explicitDate
	}

	// 1. Identify Bank / Provider
	if strings.Contains(lowerText, "bca") || strings.Contains(sender, "bca") {
		res.BankName = "BCA"
	} else if strings.Contains(lowerText, "mandiri") || strings.Contains(lowerText, "livin") {
		res.BankName = "Mandiri"
	} else if strings.Contains(lowerText, "bri") || strings.Contains(lowerText, "brimo") {
		res.BankName = "BRI"
	} else if strings.Contains(lowerText, "bni") {
		res.BankName = "BNI"
	} else if strings.Contains(lowerText, "jago") {
		res.BankName = "Bank Jago"
	} else if strings.Contains(lowerText, "jenius") || strings.Contains(lowerText, "btpn") {
		res.BankName = "Jenius"
	} else if strings.Contains(lowerText, "gopay") || strings.Contains(lowerText, "gojek") {
		res.BankName = "GoPay"
	} else if strings.Contains(lowerText, "shopee") {
		res.BankName = "ShopeePay"
	} else if strings.Contains(lowerText, "grab") {
		res.BankName = "Grab"
	} else if strings.Contains(lowerText, "dana") {
		res.BankName = "DANA"
	} else if strings.Contains(lowerText, "ovo") {
		res.BankName = "OVO"
	} else {
		res.BankName = "Bank Transfer"
	}

	// 2. Identify Type (Income vs Expense)
	if strings.Contains(lowerText, "transfer masuk") ||
		strings.Contains(lowerText, "uang masuk") ||
		strings.Contains(lowerText, "money in") ||
		strings.Contains(lowerText, "dana masuk") ||
		strings.Contains(lowerText, "kredit") ||
		strings.Contains(lowerText, "top up berhasil") ||
		strings.Contains(lowerText, "diterima dari") {
		res.Type = database.TransactionTypeIncome
	} else {
		res.Type = database.TransactionTypeExpense
	}

	// 3. Extract Amount using Currency Regex
	amount := extractAmount(text)
	if amount <= 0 {
		return nil
	}
	res.Amount = amount

	// 4. Extract Description / Merchant
	res.Description = extractMerchantOrDescription(text, res.BankName, res.Type)

	return res
}

var (
	// Matches: Rp 50.000,00 | Rp. 50.000 | IDR 100,000.00 | Rp150.000
	amountRegex = regexp.MustCompile(`(?i)(?:rp\.?|idr)\s*([0-9]{1,3}(?:[.,][0-9]{3})*(?:[.,][0-9]{2})?)`)
	// Number only fallback near "nominal" or "jumlah" or "total" (supports intervening whitespace/newlines)
	nominalRegex = regexp.MustCompile(`(?i)(?:nominal|jumlah|total|sebesar)\s*[:=]?\s*(?:rp\.?|idr)?[\s\r\n]*([0-9]{1,3}(?:[.,][0-9]{3})*(?:[.,][0-9]{2})?)`)
)

func extractAmount(text string) float64 {
	// Try nominal keyword first (more accurate)
	if matches := nominalRegex.FindStringSubmatch(text); len(matches) > 1 {
		if val := cleanAndParseFloat(matches[1]); val > 0 {
			return val
		}
	}

	// Try generic Rp regex
	matches := amountRegex.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		if len(m) > 1 {
			val := cleanAndParseFloat(m[1])
			if val >= 500 { // Ignore tiny values like admin fee Rp 5
				return val
			}
		}
	}

	return 0
}

func cleanAndParseFloat(raw string) float64 {
	s := strings.TrimSpace(raw)
	// Indonesian format: 50.000,00 -> 50000.00
	// International format: 50,000.00 -> 50000.00
	if strings.Contains(s, ",") && strings.Contains(s, ".") {
		lastComma := strings.LastIndex(s, ",")
		lastDot := strings.LastIndex(s, ".")
		if lastComma > lastDot {
			// Format Indonesia: 100.000,00
			s = strings.ReplaceAll(s, ".", "")
			s = strings.ReplaceAll(s, ",", ".")
		} else {
			// Format US: 100,000.00
			s = strings.ReplaceAll(s, ",", "")
		}
	} else if strings.Contains(s, ".") {
		// Could be 50.000 (thousands) or 50.50 (decimal)
		parts := strings.Split(s, ".")
		if len(parts) > 1 && len(parts[len(parts)-1]) == 3 {
			// Thousands separator
			s = strings.ReplaceAll(s, ".", "")
		}
	} else if strings.Contains(s, ",") {
		parts := strings.Split(s, ",")
		if len(parts) > 1 && len(parts[len(parts)-1]) == 3 {
			// Thousands separator
			s = strings.ReplaceAll(s, ",", "")
		} else {
			// Decimal
			s = strings.ReplaceAll(s, ",", ".")
		}
	}

	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return val
}

func extractMerchantOrDescription(text, bankName string, txType database.TransactionType) string {
	// Look for beneficiary / tujuan / merchant
	merchantRegexes := []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:ke rekening|tujuan|penerima|merchant|kepada|ke)\s*[:=]?\s*([a-zA-Z0-9\s.,&'-]{3,40})`),
		regexp.MustCompile(`(?i)(?:transaksi|pembayaran)\s*(?:di|pada|untuk)\s*([a-zA-Z0-9\s.,&'-]{3,40})`),
	}

	for _, re := range merchantRegexes {
		if matches := re.FindStringSubmatch(text); len(matches) > 1 {
			found := strings.TrimSpace(matches[1])
			// Clean unwanted trailing words
			found = strings.Split(found, "\n")[0]
			found = strings.Split(found, "\r")[0]
			found = strings.TrimSpace(found)
			if len(found) >= 3 && !strings.Contains(strings.ToLower(found), "rekening") {
				return found
			}
		}
	}

	if txType == database.TransactionTypeIncome {
		return fmt.Sprintf("Pemasukan (%s)", bankName)
	}
	return fmt.Sprintf("Pembayaran (%s)", bankName)
}

func matchAccount(parsed *ParsedTransactionResult, accounts []database.FinanceAccount, defaultAccID *string) string {
	if len(accounts) == 0 {
		if defaultAccID != nil {
			return *defaultAccID
		}
		return ""
	}

	// 1. Check if user has an account matching the detected bank name
	targetBank := strings.ToLower(parsed.BankName)
	for _, acc := range accounts {
		accName := strings.ToLower(acc.Name)
		if strings.Contains(accName, targetBank) || strings.Contains(targetBank, accName) {
			return acc.ID
		}
	}

	// 2. Fallback to user-configured default account
	if defaultAccID != nil && *defaultAccID != "" {
		for _, acc := range accounts {
			if acc.ID == *defaultAccID {
				return acc.ID
			}
		}
	}

	// 3. Fallback to first bank account found
	for _, acc := range accounts {
		if acc.Type == database.AccountTypeBank {
			return acc.ID
		}
	}

	return accounts[0].ID
}

func matchCategory(parsed *ParsedTransactionResult, categories []database.Category) *database.Category {
	text := strings.ToLower(parsed.Description + " " + parsed.Subject)

	// Keyword mapping
	keywords := map[string][]string{
		"makanan":     {"kopi", "cafe", "coffee", "resto", "makan", "food", "gofood", "shopeefood", "bakso", "kfc", "mcd"},
		"belanja":     {"shopee", "tokopedia", "tiktok", "lazada", "minimarket", "indomaret", "alfamart", "mall"},
		"transport":   {"grab", "gojek", "ojol", "pertamina", "shell", "bensin", "parkir", "tol", "kereta", "tiket"},
		"tagihan":     {"listrik", "pln", "pdam", "air", "wifi", "indihome", "bpjs", "telkom", "pulsa", "paket data"},
		"hiburan":     {"netflix", "spotify", "youtube", "bioskop", "xxi", "game", "steam"},
		"langganan":   {"subscription", "langganan", "google", "apple", "icloud"},
		"transfer":    {"transfer", "kirim uang", "mutasi"},
		"gaji":        {"gaji", "salary", "payroll", "honor"},
	}

	for catName, kws := range keywords {
		for _, kw := range kws {
			if strings.Contains(text, kw) {
				for i := range categories {
					cName := strings.ToLower(categories[i].Name)
					if strings.Contains(cName, catName) {
						return &categories[i]
					}
				}
			}
		}
	}

	// Default fallback to first matching type
	for i := range categories {
		if string(categories[i].Type) == string(parsed.Type) {
			return &categories[i]
		}
	}

	return nil
}

// RunEmailSyncWorker is a standing background loop that syncs active accounts periodically
func RunEmailSyncWorker() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		var activeConfigs []database.EmailSyncConfig
		if err := database.DB.Where("is_enabled = ?", true).Find(&activeConfigs).Error; err != nil {
			continue
		}

		for _, cfg := range activeConfigs {
			// Safe execution with recovery
			func(c database.EmailSyncConfig) {
				defer func() {
					if r := recover(); r != nil {
						utils.Log.Error().Interface("panic", r).Str("user_id", c.UserID).Msg("Recovered panic in EmailSyncWorker")
					}
				}()

				count, err := SyncUserEmails(c.UserID, EmailSyncOptions{Days: 30})
				if err != nil {
					utils.Log.Warn().Err(err).Str("email", c.Email).Msg("Background email sync failed")
				} else if count > 0 {
					utils.Log.Info().Int("imported", count).Str("email", c.Email).Msg("Background email sync imported new transactions")
				}
			}(cfg)
		}
	}
}

// extractTransactionDate parses explicit transaction date written in Indonesian banking emails
func extractTransactionDate(text string) time.Time {
	// 1. Look for DD/MM/YYYY or DD-MM-YYYY (e.g. 05/10/2026, 05-10-2026)
	reDMY := regexp.MustCompile(`(?i)(?:tanggal|tgl|date|waktu|trx date|waktu transaksi)\s*[:=]?\s*([0-3]?[0-9])[/-]([0-1]?[0-9])[/-](20[0-9]{2})`)
	if m := reDMY.FindStringSubmatch(text); len(m) == 4 {
		d, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		y, _ := strconv.Atoi(m[3])
		if d >= 1 && d <= 31 && mo >= 1 && mo <= 12 && y >= 2000 {
			return time.Date(y, time.Month(mo), d, 12, 0, 0, 0, time.Local)
		}
	}

	// 2. Look for YYYY-MM-DD
	reYMD := regexp.MustCompile(`(?i)(?:tanggal|tgl|date|waktu)\s*[:=]?\s*(20[0-9]{2})[/-]([0-1]?[0-9])[/-]([0-3]?[0-9])`)
	if m := reYMD.FindStringSubmatch(text); len(m) == 4 {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if d >= 1 && d <= 31 && mo >= 1 && mo <= 12 && y >= 2000 {
			return time.Date(y, time.Month(mo), d, 12, 0, 0, 0, time.Local)
		}
	}

	// 3. Look for DD Month YYYY (e.g. 05 Okt 2026, 5 Oktober 2026, 05 Oct 2026)
	reNamed := regexp.MustCompile(`(?i)(?:tanggal|tgl|date|waktu)\s*[:=]?\s*([0-3]?[0-9])\s+([a-zA-Z]{3,9})\s+(20[0-9]{2})`)
	if m := reNamed.FindStringSubmatch(text); len(m) == 4 {
		d, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[3])
		monthStr := strings.ToLower(m[2])
		monthMap := map[string]time.Month{
			"jan": time.January, "januari": time.January, "january": time.January,
			"feb": time.February, "februari": time.February, "february": time.February,
			"mar": time.March, "maret": time.March, "march": time.March,
			"apr": time.April, "april": time.April,
			"mei": time.May, "may": time.May,
			"jun": time.June, "juni": time.June,
			"jul": time.July, "juli": time.July,
			"agu": time.August, "agustus": time.August, "aug": time.August, "august": time.August,
			"sep": time.September, "september": time.September,
			"okt": time.October, "oktober": time.October, "oct": time.October, "october": time.October,
			"nov": time.November, "nopember": time.November,
			"des": time.December, "desember": time.December, "dec": time.December, "december": time.December,
		}
		if mo, ok := monthMap[monthStr]; ok && d >= 1 && d <= 31 && y >= 2000 {
			return time.Date(y, mo, d, 12, 0, 0, 0, time.Local)
		}
	}

	return time.Time{}
}
