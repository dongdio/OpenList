// Package initialize handles the initialization of OpenList's components
package initialize

import (
	"fmt"
	stdlog "log"
	"strings"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"

	"github.com/dongdio/OpenList/global"
	"github.com/dongdio/OpenList/internal/conf"
	"github.com/dongdio/OpenList/internal/db"
)

const (
	// SQLiteMemoryDSN is the DSN for in-memory SQLite database with shared cache
	SQLiteMemoryDSN = "file::memory:?cache=shared"

	// SQLiteJournalMode sets the journal mode for SQLite databases
	SQLiteJournalMode = "WAL"

	// SQLiteVacuumMode sets the vacuum mode for SQLite databases
	SQLiteVacuumMode = "incremental"

	// DefaultTimeZone is the default timezone for database connections
	DefaultTimeZone = "Asia/Shanghai"
)

// initializeDB initializes the database connection based on configuration
// This function sets up the appropriate database driver and connection,
// then initializes the database models
func initializeDB() {
	// Create GORM configuration
	gormConfig := createGormConfig()

	var dbConn *gorm.DB
	var err error

	// Use in-memory SQLite for development mode
	if global.Dev {
		dbConn, err = gorm.Open(sqlite.Open(SQLiteMemoryDSN), gormConfig)
		conf.Conf.Database.Type = "sqlite3"
	} else {
		// Initialize database based on configured type
		dbConn, err = connectToDatabase(gormConfig)
	}

	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	// Initialize database models
	db.Init(dbConn)
	log.Info("database connection established successfully")
}

// createGormConfig creates a GORM configuration with appropriate logger and naming strategy
func createGormConfig() *gorm.Config {
	// Set log level based on application mode
	logLevel := logger.Silent
	if global.Debug || global.Dev {
		logLevel = logger.Info
	}

	// Create GORM logger
	gormLogger := logger.New(
		stdlog.New(log.StandardLogger().Out, "\r\n", stdlog.LstdFlags),
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logLevel,
			IgnoreRecordNotFoundError: true,
			Colorful:                  true,
		},
	)

	// Create GORM config
	return &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: conf.Conf.Database.TablePrefix,
		},
		Logger: gormLogger,
	}
}

// connectToDatabase establishes a database connection based on the configuration
func connectToDatabase(gormConfig *gorm.Config) (*gorm.DB, error) {
	dbConfig := conf.Conf.Database

	switch dbConfig.Type {
	case "sqlite3":
		return connectToSQLite(dbConfig, gormConfig)
	case "mysql":
		return connectToMySQL(dbConfig, gormConfig)
	case "postgres":
		return connectToPostgres(dbConfig, gormConfig)
	default:
		return nil, errors.Errorf("unsupported database type: %s", dbConfig.Type)
	}
}

// connectToSQLite creates a connection to a SQLite database
func connectToSQLite(dbConfig conf.Database, gormConfig *gorm.Config) (*gorm.DB, error) {
	// Validate SQLite database file name
	if !isValidSQLiteFileName(dbConfig.DBFile) {
		return nil, errors.Errorf("invalid SQLite database file name: %s", dbConfig.DBFile)
	}

	// Construct DSN with journal and vacuum settings
	dsn := fmt.Sprintf("%s?_journal=%s&_vacuum=%s",
		dbConfig.DBFile,
		SQLiteJournalMode,
		SQLiteVacuumMode)

	return gorm.Open(sqlite.Open(dsn), gormConfig)
}

// isValidSQLiteFileName checks if the SQLite file name is valid
func isValidSQLiteFileName(fileName string) bool {
	return strings.HasSuffix(fileName, ".db") && len(fileName) > 3
}

// connectToMySQL creates a connection to a MySQL database
func connectToMySQL(dbConfig conf.Database, gormConfig *gorm.Config) (*gorm.DB, error) {
	dsn := dbConfig.DSN
	if dsn == "" {
		// Construct DSN for MySQL
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local&tls=%s",
			dbConfig.User,
			dbConfig.Password,
			dbConfig.Host,
			dbConfig.Port,
			dbConfig.Name,
			dbConfig.SSLMode)
	}

	return gorm.Open(mysql.Open(dsn), gormConfig)
}

// connectToPostgres creates a connection to a PostgreSQL database
func connectToPostgres(dbConfig conf.Database, gormConfig *gorm.Config) (*gorm.DB, error) {
	dsn := dbConfig.DSN
	if dsn == "" {
		// Construct DSN for PostgreSQL
		dsn = buildPostgresDSN(dbConfig)
	}

	return gorm.Open(postgres.Open(dsn), gormConfig)
}

// buildPostgresDSN builds a DSN string for PostgreSQL connection
func buildPostgresDSN(dbConfig conf.Database) string {
	// Base DSN template
	dsnTemplate := "host=%s user=%s dbname=%s port=%d sslmode=%s TimeZone=%s"

	// Add password if provided
	if dbConfig.Password != "" {
		dsnTemplate = "host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=%s"
		return fmt.Sprintf(dsnTemplate,
			dbConfig.Host,
			dbConfig.User,
			dbConfig.Password,
			dbConfig.Name,
			dbConfig.Port,
			dbConfig.SSLMode,
			DefaultTimeZone)
	}

	return fmt.Sprintf(dsnTemplate,
		dbConfig.Host,
		dbConfig.User,
		dbConfig.Name,
		dbConfig.Port,
		dbConfig.SSLMode,
		DefaultTimeZone)
}