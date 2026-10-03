//go:build ignore

package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/go-sql-driver/mysql"
)

func main() {
	// Connect to MySQL without database (to create the database)
	dsn := "cloudbeaver:passwordmu@tcp(192.168.18.60:3306)/?charset=utf8mb4&parseTime=True&loc=Asia%2FJakarta&timeout=5s"
	
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to MySQL: %v", err)
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping MySQL: %v", err)
	}
	fmt.Println("✅ Connected to MySQL server")

	// Create database
	query := "CREATE DATABASE IF NOT EXISTS finance_apps CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
	if _, err := db.Exec(query); err != nil {
		log.Fatalf("Failed to create database: %v", err)
	}
	fmt.Println("✅ Database 'finance_apps' created successfully")
}
