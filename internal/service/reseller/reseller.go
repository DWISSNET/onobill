// Package reseller — manajemen reseller (saldo deposit + komisi per penjualan).
// Reseller menjual voucher/paket; komisi dicatat otomatis.
package reseller

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

var (
	ErrInsufficientBalance = errors.New("saldo reseller tidak cukup")
	ErrNotFound            = errors.New("reseller tidak ditemukan")
)

// Reseller agen penjual di bawah tenant.
type Reseller struct {
	ID            uint      `gorm:"primaryKey"`
	TenantID      uint      `gorm:"column:tenant_id;index"`
	Name          string    `gorm:"column:name;size:255"`
	Phone         string    `gorm:"column:phone;size:50"`
	Email         string    `gorm:"column:email;size:255"`
	Balance       float64   `gorm:"column:balance;default:0"`        // saldo deposit
	CommissionPct float64   `gorm:"column:commission_pct;default:0"` // komisi % per penjualan
	CreatedAt     time.Time `gorm:"column:created_at"`
}

// SaleTx transaksi penjualan reseller (debit saldo / kredit komisi).
type SaleTx struct {
	ID           uint      `gorm:"primaryKey"`
	ResellerID   uint      `gorm:"column:reseller_id;index"`
	TenantID     uint      `gorm:"column:tenant_id;index"`
	Type         string    `gorm:"column:type;size:20"` // topup | sale | commission
	Amount       float64   `gorm:"column:amount"`       // + kredit / - debit
	BalanceAfter float64   `gorm:"column:balance_after"`
	Ref          string    `gorm:"column:ref;size:100"` // voucher/invoice ref
	Note         string    `gorm:"column:note;size:255"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func (s *Service) AutoMigrate() error {
	return s.db.AutoMigrate(&Reseller{}, &SaleTx{})
}

func (s *Service) Create(tenantID uint, name, phone, email string, commissionPct float64) (*Reseller, error) {
	r := &Reseller{TenantID: tenantID, Name: name, Phone: phone, Email: email, CommissionPct: commissionPct, CreatedAt: time.Now()}
	return r, s.db.Create(r).Error
}

func (s *Service) Get(tenantID, id uint) (*Reseller, error) {
	var r Reseller
	if err := s.db.Where("tenant_id=? AND id=?", tenantID, id).First(&r).Error; err != nil {
		return nil, ErrNotFound
	}
	return &r, nil
}

func (s *Service) List(tenantID uint) ([]Reseller, error) {
	var out []Reseller
	err := s.db.Where("tenant_id=?", tenantID).Find(&out).Error
	return out, err
}

// TopUp menambah saldo deposit reseller.
func (s *Service) TopUp(tenantID, id uint, amount float64, note string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		r, err := s.Get(tenantID, id)
		if err != nil {
			return err
		}
		r.Balance += amount
		if err := tx.Model(&Reseller{}).Where("id=?", r.ID).Update("balance", r.Balance).Error; err != nil {
			return err
		}
		return tx.Create(&SaleTx{ResellerID: r.ID, TenantID: tenantID, Type: "topup",
			Amount: amount, BalanceAfter: r.Balance, Note: note, CreatedAt: time.Now()}).Error
	})
}

// ChargeSale memotong saldo reseller untuk sebuah penjualan + beri komisi.
// price = harga jual ke pelanggan; cost = harga modal yang dipotong dari saldo.
func (s *Service) ChargeSale(tenantID, id uint, cost float64, ref string) (commission float64, err error) {
	err = s.db.Transaction(func(tx *gorm.DB) error {
		r, err := s.Get(tenantID, id)
		if err != nil {
			return err
		}
		if r.Balance < cost {
			return ErrInsufficientBalance
		}
		r.Balance -= cost
		commission = cost * r.CommissionPct / 100
		r.Balance += commission // komisi langsung masuk saldo
		if err := tx.Model(&Reseller{}).Where("id=?", r.ID).Update("balance", r.Balance).Error; err != nil {
			return err
		}
		if err := tx.Create(&SaleTx{ResellerID: r.ID, TenantID: tenantID, Type: "sale",
			Amount: -cost, BalanceAfter: r.Balance - commission, Ref: ref, CreatedAt: time.Now()}).Error; err != nil {
			return err
		}
		if commission > 0 {
			if err := tx.Create(&SaleTx{ResellerID: r.ID, TenantID: tenantID, Type: "commission",
				Amount: commission, BalanceAfter: r.Balance, Ref: ref, CreatedAt: time.Now()}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return commission, err
}

// Transactions riwayat transaksi reseller.
func (s *Service) Transactions(tenantID, id uint, limit int) ([]SaleTx, error) {
	if limit <= 0 {
		limit = 50
	}
	var out []SaleTx
	err := s.db.Where("tenant_id=? AND reseller_id=?", tenantID, id).
		Order("created_at DESC").Limit(limit).Find(&out).Error
	return out, err
}
