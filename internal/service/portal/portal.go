// Package portal — portal pelanggan self-service.
// Pelanggan login -> lihat tagihan -> bayar -> ubah SSID/password WiFi sendiri.
package portal

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"
)

var (
	ErrInvalidCredentials = errors.New("username/password salah")
	ErrSessionExpired     = errors.New("sesi berakhir, silakan login ulang")
)

// CustomerAuth kredensial login pelanggan ke portal.
type CustomerAuth struct {
	ID         uint   `gorm:"primaryKey"`
	CustomerID uint   `gorm:"column:customer_id;uniqueIndex"`
	TenantID   uint   `gorm:"column:tenant_id;index"`
	Username   string `gorm:"column:username;size:100;uniqueIndex"`
	Password   string `gorm:"column:password;size:255"` // hashed di produksi
}

// Session sesi login pelanggan.
type Session struct {
	ID         uint      `gorm:"primaryKey"`
	Token      string    `gorm:"column:token;size:100;uniqueIndex"`
	CustomerID uint      `gorm:"column:customer_id;index"`
	TenantID   uint      `gorm:"column:tenant_id;index"`
	ExpiresAt  time.Time `gorm:"column:expires_at"`
}

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func (s *Service) AutoMigrate() error {
	return s.db.AutoMigrate(&CustomerAuth{}, &Session{})
}

// Register membuat akun portal untuk pelanggan.
func (s *Service) Register(tenantID, customerID uint, username, password string) error {
	a := &CustomerAuth{CustomerID: customerID, TenantID: tenantID, Username: username, Password: password}
	return s.db.Create(a).Error
}

// Login verifikasi kredensial -> buat sesi.
func (s *Service) Login(username, password string) (*Session, error) {
	var a CustomerAuth
	if err := s.db.Where("username=? AND password=?", username, password).First(&a).Error; err != nil {
		return nil, ErrInvalidCredentials
	}
	tok := make([]byte, 24)
	rand.Read(tok)
	sess := &Session{
		Token:      hex.EncodeToString(tok),
		CustomerID: a.CustomerID,
		TenantID:   a.TenantID,
		ExpiresAt:  time.Now().Add(24 * time.Hour),
	}
	if err := s.db.Create(sess).Error; err != nil {
		return nil, err
	}
	return sess, nil
}

// ValidateSession cek token -> kembalikan customer & tenant.
func (s *Service) ValidateSession(token string) (customerID, tenantID uint, err error) {
	var sess Session
	if err := s.db.Where("token=?", token).First(&sess).Error; err != nil {
		return 0, 0, ErrInvalidCredentials
	}
	if time.Now().After(sess.ExpiresAt) {
		return 0, 0, ErrSessionExpired
	}
	return sess.CustomerID, sess.TenantID, nil
}

// Logout hapus sesi.
func (s *Service) Logout(token string) error {
	return s.db.Where("token=?", token).Delete(&Session{}).Error
}
