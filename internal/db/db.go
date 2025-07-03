// Package db provides database connectivity and operations for OpenList
package db

import (
	"errors"
	"sync"

	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/dongdio/OpenList/v4/internal/conf"
	"github.com/dongdio/OpenList/v4/internal/model"
)

// Error definitions
var (
	// ErrDatabaseNotInitialized is returned when a database operation is attempted before initialization
	ErrDatabaseNotInitialized = errors.New("database not initialized")
)

// Global database connection and mutex for thread safety
var (
	// db holds the global database connection instance
	db *gorm.DB

	// dbMutex protects the db variable from concurrent access
	dbMutex sync.RWMutex
)

// Init initializes the database connection and performs schema migrations
// Parameters:
//   - d: database connection instance
//
// The function will terminate the application if migrations fail
func Init(d *gorm.DB) {
	if d == nil {
		log.Fatal("cannot initialize with nil database connection")
		return
	}

	dbMutex.Lock()
	db = d
	dbMutex.Unlock()

	// Models that require migration
	modelsToMigrate := []any{
		&model.Storage{},
		&model.User{},
		&model.Meta{},
		&model.SettingItem{},
		&model.SearchNode{},
		&model.TaskItem{},
		&model.SSHPublicKey{},
	}

	err := AutoMigrate(modelsToMigrate...)
	if err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	log.Info("database initialized successfully")
}

// AutoMigrate performs database schema migrations for the given models
// Applies special options for MySQL databases to ensure proper charset and engine
// Parameters:
//   - models: list of model structs to migrate
//
// Returns:
//   - error if migration fails
func AutoMigrate(models ...any) error {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	if db == nil {
		return ErrDatabaseNotInitialized
	}

	var err error
	if conf.Conf.Database.Type == "mysql" {
		// Set MySQL-specific options for tables
		err = db.Set("gorm:table_options", "ENGINE=InnoDB CHARSET=utf8mb4").AutoMigrate(models...)
	} else {
		err = db.AutoMigrate(models...)
	}

	if err != nil {
		log.Errorf("database migration failed: %v", err)
	}

	return err
}

// GetDB returns the current database connection
// Can be used to perform custom database operations
// Returns nil if the database has not been initialized
func GetDB() *gorm.DB {
	dbMutex.RLock()
	defer dbMutex.RUnlock()

	return db
}

// Close properly closes the database connection
// Should be called when the application is shutting down
func Close() {
	dbMutex.Lock()
	defer dbMutex.Unlock()

	if db == nil {
		log.Warn("attempting to close nil database connection")
		return
	}

	log.Info("closing database connection")

	sqlDB, err := db.DB()
	if err != nil {
		log.Errorf("failed to get sql.DB instance: %v", err)
		return
	}

	if err = sqlDB.Close(); err != nil {
		log.Errorf("failed to close database connection: %v", err)
	} else {
		log.Info("database connection closed successfully")
	}

	// Set db to nil to prevent further usage
	db = nil
}