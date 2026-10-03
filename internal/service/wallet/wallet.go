package wallet

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"onobill/internal/domain"
)

var (
	ErrWalletNotFound     = errors.New("wallet tidak ditemukan")
	ErrWalletBlocked      = errors.New("wallet diblokir")
	ErrInsufficientFunds  = errors.New("saldo tidak cukup")
	ErrDuplicateReference = errors.New("referensi wallet sudah diproses")
)

// Service mengelola saldo prabayar platform secara atomik.
type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func (s *Service) EnsureWallet(tenantID uint) (*domain.Wallet, error) {
	if tenantID == 0 {
		return nil, errors.New("tenant tidak valid")
	}
	var w domain.Wallet
	err := s.db.Where("tenant_id = ?", tenantID).First(&w).Error
	if err == nil {
		return &w, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	w = domain.Wallet{TenantID: tenantID, Currency: "IDR", Status: "active"}
	if err := s.db.Create(&w).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return s.Get(tenantID)
		}
		return nil, err
	}
	return &w, nil
}

func (s *Service) Get(tenantID uint) (*domain.Wallet, error) {
	var w domain.Wallet
	if err := s.db.Where("tenant_id = ?", tenantID).First(&w).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}
	return &w, nil
}

// CreditTopUp booleans are deliberately absent: a successful top-up is credited
// only once because the unique ledger reference is checked inside one transaction.
func (s *Service) CreditTopUp(tenantID uint, topupID string, amount int64) error {
	if amount <= 0 || topupID == "" {
		return errors.New("top-up tidak valid")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var w domain.Wallet
		if err := tx.Where("tenant_id = ?", tenantID).First(&w).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				w = domain.Wallet{TenantID: tenantID, Currency: "IDR", Status: "active"}
				if err := tx.Create(&w).Error; err != nil {
					return err
				}
			} else {
				return err
			}
		}
		if w.Status != "active" {
			return ErrWalletBlocked
		}
		var existing domain.WalletLedger
		err := tx.Where("tenant_id = ? AND reference_type = ? AND reference_id = ? AND entry_type = ?", tenantID, "topup", topupID, "credit").First(&existing).Error
		if err == nil {
			return ErrDuplicateReference
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		before := w.Balance
		w.Balance += amount
		if err := tx.Model(&w).Updates(map[string]any{"balance": w.Balance, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		entry := domain.WalletLedger{TenantID: tenantID, WalletID: w.ID, ReferenceType: "topup", ReferenceID: topupID, EntryType: "credit", Amount: amount, BalanceBefore: before, BalanceAfter: w.Balance, Description: fmt.Sprintf("Top up %s", topupID)}
		return tx.Create(&entry).Error
	})
}

func (s *Service) Debit(tenantID uint, referenceType, referenceID string, amount int64, description string) error {
	if amount <= 0 || referenceType == "" || referenceID == "" {
		return errors.New("debit tidak valid")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var w domain.Wallet
		if err := tx.Where("tenant_id = ?", tenantID).First(&w).Error; err != nil {
			return err
		}
		if w.Status != "active" {
			return ErrWalletBlocked
		}
		var existing domain.WalletLedger
		err := tx.Where("tenant_id = ? AND reference_type = ? AND reference_id = ? AND entry_type = ?", tenantID, referenceType, referenceID, "debit").First(&existing).Error
		if err == nil {
			return ErrDuplicateReference
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if w.Balance < amount {
			return ErrInsufficientFunds
		}
		before := w.Balance
		w.Balance -= amount
		if err := tx.Model(&w).Updates(map[string]any{"balance": w.Balance, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		entry := domain.WalletLedger{TenantID: tenantID, WalletID: w.ID, ReferenceType: referenceType, ReferenceID: referenceID, EntryType: "debit", Amount: amount, BalanceBefore: before, BalanceAfter: w.Balance, Description: description}
		return tx.Create(&entry).Error
	})
}

func (s *Service) Ledger(tenantID uint, limit int) ([]domain.WalletLedger, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []domain.WalletLedger
	err := s.db.Where("tenant_id = ?", tenantID).Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}
