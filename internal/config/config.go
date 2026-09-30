package config

import "os"

type Config struct {
	DBDriver   string // "sqlite" or "mysql"
	DBPath     string // sqlite file path
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string
	AppPort    string
	JWTSecret  string
	AppEnv     string // "dev" or "prod"
	BaseURL    string // URL publik aplikasi (untuk callback/return payment)

	// Payment gateway: Duitku
	DuitkuMerchant string
	DuitkuAPIKey   string
	DuitkuBaseURL  string
	// Payment gateway: KlikQRIS
	KlikQRISMerchant string
	KlikQRISAPIKey   string
	KlikQRISBaseURL  string
}

func Load() Config {
	return Config{
		DBDriver:   getEnv("DB_DRIVER", "sqlite"),
		DBPath:     getEnv("DB_PATH", "./onobill.db"),
		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "3306"),
		DBName:     getEnv("DB_NAME", "onobill"),
		DBUser:     getEnv("DB_USER", "root"),
		DBPassword: getEnv("DB_PASSWORD", ""),
		AppPort:    getEnv("APP_PORT", "8080"),
		JWTSecret:  getEnv("JWT_SECRET", "onobill-secret-change-me-in-production"),
		AppEnv:     getEnv("APP_ENV", "dev"),
		BaseURL:    getEnv("BASE_URL", "http://localhost:8080"),

		DuitkuMerchant:   getEnv("DUITKU_MERCHANT", ""),
		DuitkuAPIKey:     getEnv("DUITKU_API_KEY", ""),
		DuitkuBaseURL:    getEnv("DUITKU_BASE_URL", ""),
		KlikQRISMerchant: getEnv("KLIKQRIS_MERCHANT", ""),
		KlikQRISAPIKey:   getEnv("KLIKQRIS_API_KEY", ""),
		KlikQRISBaseURL:  getEnv("KLIKQRIS_BASE_URL", ""),
	}
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
