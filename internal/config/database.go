package config

import (
	"fmt"
	"onobill/internal/domain"

	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func ConnectDB(cfg Config) (*gorm.DB, error) {
	var db *gorm.DB
	var err error

	gormCfg := &gorm.Config{}
	if cfg.AppEnv == "dev" {
		gormCfg.Logger = logger.Default.LogMode(logger.Warn)
	}

	switch cfg.DBDriver {
	case "sqlite":
		db, err = gorm.Open(sqlite.Open(cfg.DBPath), gormCfg)
	case "mysql":
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			cfg.DBUser, cfg.DBPassword, cfg.DBHost, cfg.DBPort, cfg.DBName)
		db, err = gorm.Open(mysql.Open(dsn), gormCfg)
	default:
		return nil, fmt.Errorf("unsupported DB_DRIVER: %s", cfg.DBDriver)
	}
	if err != nil {
		return nil, err
	}

	// Auto-migrate all models
	if err := db.AutoMigrate(
		&domain.Tenant{},
		&domain.User{},
		&domain.Router{},
		&domain.Package{},
		&domain.Customer{},
		&domain.Invoice{},
		&domain.Payment{},
		&domain.Voucher{},
		&domain.VPNPeer{},
		&domain.CHRServer{},
		&domain.AuditLog{},
		&domain.Job{},
		&domain.NotifSetting{},
		&domain.PaymentGatewaySetting{},
		&domain.NotifLog{},
	); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return db, nil
}

// Seed creates default superadmin tenant + user if not exists
func Seed(db *gorm.DB) error {
	var count int64
	db.Model(&domain.Tenant{}).Count(&count)
	if count > 0 {
		return nil
	}

	tenant := domain.Tenant{Name: "ONOBILL Master", Slug: "master"}
	if err := db.Create(&tenant).Error; err != nil {
		return err
	}

	hash, _ := HashPassword("admin123")
	admin := domain.User{
		TenantID:     tenant.ID,
		Email:        "admin@onobill.local",
		PasswordHash: hash,
		Name:         "Super Admin",
		Role:         "superadmin",
	}
	return db.Create(&admin).Error
}
