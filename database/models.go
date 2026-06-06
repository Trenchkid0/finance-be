package database

import (
	"time"
)

type AccountType string

const (
	AccountTypeBank       AccountType = "bank"
	AccountTypeWallet     AccountType = "wallet"
	AccountTypeCash       AccountType = "cash"
	AccountTypeInvestment AccountType = "investment"
)

type TransactionType string

const (
	TransactionTypeIncome   TransactionType = "income"
	TransactionTypeExpense  TransactionType = "expense"
	TransactionTypeTransfer TransactionType = "transfer"
)

type CategoryType string

const (
	CategoryTypeIncome  CategoryType = "income"
	CategoryTypeExpense CategoryType = "expense"
)

// User represents the NextAuth equivalent user model.
type User struct {
	ID            string           `gorm:"primaryKey;type:varchar(191)" json:"id"`
	Name          string           `gorm:"type:varchar(191)" json:"name"`
	Email         string           `gorm:"uniqueIndex;type:varchar(191)" json:"email"`
	EmailVerified *time.Time       `json:"emailVerified,omitempty"`
	Image         string           `gorm:"type:varchar(191)" json:"image"`
	Password      string           `gorm:"type:varchar(255)" json:"-"` // Hashed, never expose in JSON
	CreatedAt     time.Time        `json:"createdAt"`
	UpdatedAt     time.Time        `json:"updatedAt"`
	Accounts      []FinanceAccount `gorm:"foreignKey:UserID;constraint:OnDelete:Cascade" json:"-"`
	Categories    []Category       `gorm:"foreignKey:UserID;constraint:OnDelete:Cascade" json:"-"`
	Transactions  []Transaction    `gorm:"foreignKey:UserID;constraint:OnDelete:Cascade" json:"-"`
	ApiKeys       []ApiKey         `gorm:"foreignKey:UserID;constraint:OnDelete:Cascade" json:"-"`
	Budgets       []Budget         `gorm:"foreignKey:UserID;constraint:OnDelete:Cascade" json:"-"`
}

// FinanceAccount corresponds to the finance_accounts table (source/dest of funds).
type FinanceAccount struct {
	ID        string      `gorm:"primaryKey;type:varchar(191)" json:"id"`
	UserID    string      `gorm:"index;type:varchar(191);not null" json:"userId"`
	Name      string      `gorm:"type:varchar(191);not null" json:"name"`
	Type      AccountType `gorm:"type:varchar(50);not null" json:"type"`
	Balance   float64     `gorm:"type:decimal(15,2);default:0;not null" json:"balance"`
	Currency  string      `gorm:"type:varchar(10);default:'IDR';not null" json:"currency"`
	Icon      string      `gorm:"type:varchar(100)" json:"icon"`
	Color     string      `gorm:"type:varchar(50)" json:"color"`
	IsActive  bool        `gorm:"default:true;not null" json:"isActive"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

// Category represents transaction categories.
type Category struct {
	ID        string       `gorm:"primaryKey;type:varchar(191)" json:"id"`
	UserID    *string      `gorm:"index;type:varchar(191)" json:"userId"` // Nullable for global defaults
	Name      string       `gorm:"type:varchar(191);not null" json:"name"`
	Type      CategoryType `gorm:"type:varchar(50);not null" json:"type"`
	Icon      string       `gorm:"type:varchar(100)" json:"icon"`
	Color     string       `gorm:"type:varchar(50)" json:"color"`
	IsDefault bool         `gorm:"default:false;not null" json:"isDefault"`
	CreatedAt time.Time    `json:"createdAt"`
}

// ApiKey stores hashed API tokens for script/bot access.
type ApiKey struct {
	ID         string     `gorm:"primaryKey;type:varchar(191)" json:"id"`
	UserID     string     `gorm:"index;type:varchar(191);not null" json:"userId"`
	Name       string     `gorm:"type:varchar(191);not null" json:"name"`
	KeyPrefix  string     `gorm:"type:varchar(12);not null" json:"keyPrefix"`
	KeyHash    string     `gorm:"uniqueIndex;type:varchar(64);not null" json:"-"` // SHA-256 hash
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// Budget represents monthly category spending limit for a user.
type Budget struct {
	ID         string    `gorm:"primaryKey;type:varchar(191)" json:"id"`
	UserID     string    `gorm:"uniqueIndex:idx_user_category;type:varchar(191);not null" json:"userId"`
	CategoryID string    `gorm:"uniqueIndex:idx_user_category;type:varchar(191);not null" json:"categoryId"`
	Limit      float64   `gorm:"type:decimal(15,2);not null" json:"limit"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// Transaction represents a single income, expense, or transfer.
type Transaction struct {
	ID           string          `gorm:"primaryKey;type:varchar(191)" json:"id"`
	UserID       string          `gorm:"index;type:varchar(191);not null" json:"userId"`
	AccountID    string          `gorm:"index;type:varchar(191);not null" json:"accountId"`
	CategoryID   *string         `gorm:"index;type:varchar(191)" json:"categoryId"` // Nullable (especially for transfers)
	Type         TransactionType `gorm:"type:varchar(50);not null" json:"type"`
	Amount       float64         `gorm:"type:decimal(15,2);not null" json:"amount"`
	Description  string          `gorm:"type:varchar(191)" json:"description"`
	Note         string          `gorm:"type:text" json:"note"`
	Date         time.Time       `gorm:"index;not null" json:"date"`
	TransferToID *string         `gorm:"type:varchar(191)" json:"transferToId"` // ID of target account if transfer

	// Relations (only populated when Preloaded, GORM handles them)
	Category   *Category       `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
	Account    *FinanceAccount `gorm:"foreignKey:AccountID" json:"account,omitempty"`
	TransferTo *FinanceAccount `gorm:"foreignKey:TransferToID" json:"transferTo,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
