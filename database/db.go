package database

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"
	_ "time/tzdata"

	sqlite "github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	mysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB
var DbPath string

// InitDB initializes the database and returns the connection.
// It runs auto-migrations and seeds default data if the database is empty.
func InitDB(connectionString string) (*gorm.DB, error) {
	DbPath = connectionString
	// ✅ PERF: Make SQL logger env-aware — silent in production, verbose only when DEBUG_SQL=true
	logLevel := logger.Silent
	if os.Getenv("DEBUG_SQL") == "true" {
		logLevel = logger.Info
	}

	config := &gorm.Config{
		Logger:        logger.Default.LogMode(logLevel),
		PrepareStmt:   true, // ✅ PERF: Cache prepared statements to reduce parse overhead
	}

	var dialeg gorm.Dialector
	
	// Detect if connectionString is a MySQL connection (contains tcp( or mysql:// or is not a file ending in .db)
	isMySQL := strings.Contains(connectionString, "@tcp(") || 
		strings.Contains(connectionString, "mysql://") || 
		strings.Contains(connectionString, "Host=") || 
		strings.Contains(connectionString, "charset=")

	if isMySQL {
		dsn := strings.TrimPrefix(connectionString, "mysql://")
		
		// Force local timezone parsing in GORM to use Asia/Jakarta instead of server Local (which is UTC)
		if strings.Contains(dsn, "loc=Local") {
			dsn = strings.ReplaceAll(dsn, "loc=Local", "loc=Asia%2FJakarta")
		} else if strings.Contains(dsn, "loc=UTC") {
			dsn = strings.ReplaceAll(dsn, "loc=UTC", "loc=Asia%2FJakarta")
		} else if !strings.Contains(dsn, "loc=") {
			if strings.Contains(dsn, "?") {
				dsn += "&loc=Asia%2FJakarta"
			} else {
				dsn += "?loc=Asia%2FJakarta"
			}
		}

		// GORM MySQL driver needs parseTime=True to map TIME/DATETIME fields to time.Time in Go
		if !strings.Contains(dsn, "parseTime=") {
			if strings.Contains(dsn, "?") {
				dsn += "&parseTime=True"
			} else {
				dsn += "?parseTime=True"
			}
		}
		dialeg = mysql.Open(dsn)
	} else {
		dialeg = sqlite.Open(connectionString)
	}

	db, err := gorm.Open(dialeg, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// ✅ PERF: Tune connection pool — prevents connection exhaustion and stale connections
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	sqlDB.SetConnMaxIdleTime(3 * time.Minute)

	// Run Auto-migrations
	err = db.AutoMigrate(
		&User{},
		&FinanceAccount{},
		&Category{},
		&ApiKey{},
		&Budget{},
		&Transaction{},
		&SavingsGoal{},
		&RecurringBill{},
		&AssetHolding{},
		&Notification{},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	// Manually alter key_prefix column to varchar(255) to support storing the full key
	if db.Dialector.Name() == "mysql" {
		if err := db.Exec("ALTER TABLE api_keys MODIFY COLUMN key_prefix VARCHAR(255) NOT NULL").Error; err != nil {
			fmt.Printf("⚠️ Failed to modify key_prefix column to VARCHAR(255): %v\n", err)
		} else {
			fmt.Println("🚀 Successfully modified key_prefix column to VARCHAR(255)")
		}
	}

	// Run SQL migrations
	runSQLMigrations(db)

	DB = db

	// Check if categories are empty or demo user is missing. If so, seed defaults.
	var count int64
	db.Model(&Category{}).Count(&count)
	var userCount int64
	db.Model(&User{}).Where("email = ?", "demo@maybe.local").Count(&userCount)
	if count == 0 || userCount == 0 {
		fmt.Println("🌱 Database is empty or demo user is missing. Seeding default data...")
		err = SeedDemoData(db)
		if err != nil {
			fmt.Printf("⚠️ Seeding failed: %v\n", err)
		} else {
			fmt.Println("✅ Seeding complete.")
		}
	}

	return db, nil
}

// slug helper for category IDs
func slug(input string) string {
	res := strings.ToLower(input)
	res = strings.ReplaceAll(res, "&", "and")
	fields := strings.Fields(res)
	return strings.Join(fields, "-")
}

// SeedDemoData replicates the Prisma seed script in Go
func SeedDemoData(db *gorm.DB) error {
	appEnv := strings.ToLower(os.Getenv("APP_ENV"))
	goEnv := strings.ToLower(os.Getenv("GO_ENV"))
	if appEnv == "production" || appEnv == "prod" || goEnv == "production" || goEnv == "prod" {
		fmt.Println("⚠️ SeedDemoData: Seeding is disabled in production/prod environment.")
		return nil
	}

	// 1. Seed default categories
	expenseCategories := []struct {
		name  string
		icon  string
		color string
	}{
		{"Makanan & Minuman", "🍔", "#F85149"},
		{"Transportasi", "🚗", "#388BFD"},
		{"Belanja", "🛍️", "#A371F7"},
		{"Tagihan", "🧾", "#D29922"},
		{"Hiburan", "🎬", "#39D353"},
		{"Kesehatan", "💊", "#2EA043"},
		{"Pendidikan", "📚", "#8B949E"},
		{"Lainnya", "📦", "#8B949E"},
	}

	incomeCategories := []struct {
		name  string
		icon  string
		color string
	}{
		{"Gaji", "💼", "#2EA043"},
		{"Bonus", "🎁", "#39D353"},
		{"Investasi", "📈", "#388BFD"},
		{"Lainnya", "💰", "#8B949E"},
	}

	// ✅ PERF: Batch-insert categories instead of one-by-one saves
	allCategories := make([]Category, 0, len(expenseCategories)+len(incomeCategories))
	for _, c := range expenseCategories {
		allCategories = append(allCategories, Category{
			ID: fmt.Sprintf("default-expense-%s", slug(c.name)), Name: c.name,
			Type: CategoryTypeExpense, Icon: c.icon, Color: c.color,
			IsDefault: true, CreatedAt: time.Now(),
		})
	}
	for _, c := range incomeCategories {
		allCategories = append(allCategories, Category{
			ID: fmt.Sprintf("default-income-%s", slug(c.name)), Name: c.name,
			Type: CategoryTypeIncome, Icon: c.icon, Color: c.color,
			IsDefault: true, CreatedAt: time.Now(),
		})
	}
	db.CreateInBatches(&allCategories, 20)
	seededExpenses := allCategories[:len(expenseCategories)]
	seededIncomes := allCategories[len(expenseCategories):]

	// 2. Seed Demo User
	demoEmail := "demo@maybe.local"
	// Clean up old demo user and related structures explicitly to prevent duplicate key constraint errors
	db.Where("user_id = ? OR id = ?", "demo-user-id", "demo-api-key-id").Delete(&ApiKey{})
	db.Where("user_id = ?", "demo-user-id").Delete(&Transaction{})
	db.Where("user_id = ? OR id LIKE ?", "demo-user-id", "demo-acc-%").Delete(&FinanceAccount{})
	db.Where("email = ? OR id = ?", demoEmail, "demo-user-id").Delete(&User{})

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	demoUser := User{
		ID:        "demo-user-id",
		Name:      "Demo User",
		Email:     demoEmail,
		Password:  string(hashedPassword),
		Image:     "",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := db.Create(&demoUser).Error; err != nil {
		return err
	}

	// 3. Seed Demo Accounts
	demoAccounts := []struct {
		name    string
		accType AccountType
		balance float64
		icon    string
		color   string
	}{
		{"BCA Tahapan", AccountTypeBank, 25000000.0, "🏦", "#388BFD"},
		{"GoPay", AccountTypeWallet, 850000.0, "📱", "#2EA043"},
		{"Tunai", AccountTypeCash, 400000.0, "💵", "#D29922"},
	}

	var accounts []FinanceAccount
	for i, a := range demoAccounts {
		acc := FinanceAccount{
			ID:        fmt.Sprintf("demo-acc-%d", i+1),
			UserID:    demoUser.ID,
			Name:      a.name,
			Type:      a.accType,
			Balance:   a.balance,
			Currency:  "IDR",
			Icon:      a.icon,
			Color:     a.color,
			IsActive:  true,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := db.Create(&acc).Error; err != nil {
			return err
		}
		accounts = append(accounts, acc)
	}

	// Find Gaji Category
	var salaryCategory Category
	for _, c := range seededIncomes {
		if c.Name == "Gaji" {
			salaryCategory = c
			break
		}
	}

	// 4. Seed Transactions (60 Days history)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	var transactions []Transaction

	// Seeding Gaji (Monthly Salary)
	now := time.Now()
	for _, offset := range []int{0, 1} {
		payDate := time.Date(now.Year(), now.Month()-time.Month(offset), 1, 9, 0, 0, 0, now.Location())
		if payDate.Before(now) {
			salaryTx := Transaction{
				UserID:      demoUser.ID,
				AccountID:   accounts[0].ID, // BCA Tahapan
				CategoryID:  &salaryCategory.ID,
				Type:        TransactionTypeIncome,
				Amount:      8500000.0,
				Description: "Gaji bulanan",
				Note:        "Pemberian gaji rutin",
				Date:        payDate,
				CreatedAt:   payDate,
				UpdatedAt:   payDate,
			}
			transactions = append(transactions, salaryTx)
		}
	}

	// Random expense generator helpers
	amountFor := func(categoryName string) float64 {
		ranges := map[string][2]float64{
			"Makanan & Minuman": {25000, 150000},
			"Transportasi":      {15000, 80000},
			"Belanja":           {50000, 500000},
			"Tagihan":           {100000, 1500000},
			"Hiburan":           {30000, 250000},
			"Kesehatan":         {50000, 400000},
			"Pendidikan":        {100000, 600000},
			"Lainnya":           {20000, 200000},
		}
		rVal := ranges[categoryName]
		if rVal[0] == 0 {
			rVal = [2]float64{20000, 200000}
		}
		delta := rVal[1] - rVal[0]
		randomAmt := rVal[0] + r.Float64()*delta
		// Round to nearest 1000
		return float64(int(randomAmt/1000) * 1000)
	}

	descriptionFor := func(categoryName string) string {
		samples := map[string][]string{
			"Makanan & Minuman": {"Kopi pagi", "Makan siang", "Gojek Food", "GrabFood", "Indomaret"},
			"Transportasi":      {"GoRide", "Grab", "BBM", "Parkir", "Tol"},
			"Belanja":           {"Tokopedia", "Shopee", "Indomaret", "Alfamart"},
			"Tagihan":           {"Listrik PLN", "Internet", "PDAM", "Pulsa & data"},
			"Hiburan":           {"Spotify", "Netflix", "Bioskop", "Game"},
			"Kesehatan":         {"Apotek", "Konsultasi dokter", "Vitamin"},
			"Pendidikan":        {"Buku", "Kursus online", "Webinar"},
			"Lainnya":           {"Lain-lain"},
		}
		list := samples[categoryName]
		if len(list) == 0 {
			return "Transaksi"
		}
		return list[r.Intn(len(list))]
	}

	txCount := 1
	for dayOffset := 0; dayOffset < 60; dayOffset++ {
		todayTxCount := r.Intn(3) + 1 // 1-3 transactions per day
		for i := 0; i < todayTxCount; i++ {
			cat := seededExpenses[r.Intn(len(seededExpenses))]
			account := accounts[r.Intn(len(accounts))]
			amount := amountFor(cat.Name)
			desc := descriptionFor(cat.Name)

			txDate := now.AddDate(0, 0, -dayOffset)
			txDate = time.Date(txDate.Year(), txDate.Month(), txDate.Day(), 8+i*4, r.Intn(60), 0, 0, txDate.Location())

			expenseTx := Transaction{
				UserID:      demoUser.ID,
				AccountID:   account.ID,
				CategoryID:  &cat.ID,
				Type:        TransactionTypeExpense,
				Amount:      amount,
				Description: desc,
				Date:        txDate,
				CreatedAt:   txDate,
				UpdatedAt:   txDate,
			}
			transactions = append(transactions, expenseTx)
			txCount++
		}
	}

	// ✅ PERF: Batch-insert transactions instead of one-by-one creates
	db.CreateInBatches(&transactions, 50)

	// 5. Reconcile account balances based on transactions
	for _, acc := range accounts {
		var totalIncome float64
		var totalExpense float64

		db.Model(&Transaction{}).
			Where("account_id = ? AND type = ?", acc.ID, TransactionTypeIncome).
			Select("COALESCE(SUM(amount), 0)").Scan(&totalIncome)

		db.Model(&Transaction{}).
			Where("account_id = ? AND type = ?", acc.ID, TransactionTypeExpense).
			Select("COALESCE(SUM(amount), 0)").Scan(&totalExpense)

		startingBalances := map[string]float64{
			"BCA Tahapan": 25000000.0,
			"GoPay":       850000.0,
			"Tunai":       400000.0,
		}
		starting := startingBalances[acc.Name]

		newBalance := starting + totalIncome - totalExpense
		db.Model(&acc).Update("balance", newBalance)
	}

	// 6. Create a Demo API Key for programmatic access testing
	apiKeyName := "Demo Bot API Key"
	prefix := "demo_api_key_"
	plainSecret := "demo_secret_key_1234567890abcdef"
	fullKey := prefix + plainSecret
	hash := sha256.Sum256([]byte(fullKey))
	hashHex := hex.EncodeToString(hash[:])

	demoApiKey := ApiKey{
		ID:        "demo-api-key-id",
		UserID:    demoUser.ID,
		Name:      apiKeyName,
		KeyPrefix: fullKey[:12],
		KeyHash:   hashHex,
		CreatedAt: time.Now(),
	}
	db.Create(&demoApiKey)

	fmt.Printf("   Demo User Email: %s\n", demoUser.Email)
	fmt.Printf("   Demo API Key (Full): %s\n", fullKey)
	return nil
}

// runSQLMigrations discovers and executes all *.sql files inside database/migrations/
// in alphabetical order. New migration files are picked up automatically on startup
// — no code changes required when adding future migrations.
func runSQLMigrations(db *gorm.DB) {
	migrationsDir := "database/migrations"

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		fmt.Printf("⚠️ Failed to read migrations directory: %v\n", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		sqlFile := migrationsDir + "/" + entry.Name()
		content, err := os.ReadFile(sqlFile)
		if err != nil {
			fmt.Printf("⚠️ Failed to read migration file %s: %v\n", entry.Name(), err)
			continue
		}

		// Split by semicolon and run each statement
		queries := strings.Split(string(content), ";")
		for _, query := range queries {
			query = strings.TrimSpace(query)
			if query == "" || strings.HasPrefix(query, "--") {
				continue
			}

			if err := db.Exec(query).Error; err != nil {
				// Non-fatal: indexes or columns may already exist
				fmt.Printf("⚠️ [%s] migration warning: %v\n", entry.Name(), err)
			}
		}
		fmt.Printf("🚀 Migration applied: %s\n", entry.Name())
	}
}
