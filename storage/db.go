package storage

import (
	"fmt"

	"github.com/qzydustin/nanoapi/config"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB wraps a GORM database connection.
type DB struct {
	Gorm *gorm.DB
}

// NewDB initializes a database connection based on configuration.
func NewDB(cfg config.StorageConfig) (*DB, error) {
	switch cfg.Driver {
	case "sqlite":
		return newSQLiteDB(cfg.DSN)
	default:
		return nil, fmt.Errorf("unsupported storage driver: %s", cfg.Driver)
	}
}

func newSQLiteDB(dsn string) (*DB, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL mode for better concurrent access.
	db.Exec("PRAGMA journal_mode=WAL")

	return &DB{Gorm: db}, nil
}

// Migrate runs auto-migrations for all registered models.
func (d *DB) Migrate(models ...any) error {
	return d.Gorm.AutoMigrate(models...)
}
