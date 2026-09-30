// Package accounting — pembukuan sederhana ONOBILL.
// Ledger double-entry ringan: setiap transaksi mencatat debit & kredit.
// Akuntansi TENANT (pendapatan tenant) + SUPERADMIN (platform fee/komisi ONOBILL).
package accounting

import (
	"time"

	"gorm.io/gorm"
)

// Account jenis akun (chart of accounts minimal).
type Account struct {
	ID    uint   `gorm:"primaryKey"`
	Code  string `gorm:"column:code;size:20;uniqueIndex"` // 1000 Kas, 4000 Pendapatan, 4100 PlatformFee
	Name  string `gorm:"column:name;size:100"`
	Type  string `gorm:"column:type;size:20"`  // asset | revenue | expense | liability
	Scope string `gorm:"column:scope;size:20"` // tenant | superadmin
}

// Entry satu baris jurnal (ledger).
type Entry struct {
	ID          uint      `gorm:"primaryKey"`
	TenantID    uint      `gorm:"column:tenant_id;index"` // 0 = superadmin/platform
	Date        time.Time `gorm:"column:date;index"`
	AccountCode string    `gorm:"column:account_code;index"`
	Debit       float64   `gorm:"column:debit"`
	Credit      float64   `gorm:"column:credit"`
	Ref         string    `gorm:"column:ref;size:100;index"` // invoice/payment ref
	Memo        string    `gorm:"column:memo;size:255"`
	CreatedAt   time.Time `gorm:"column:created_at"`
}

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func (s *Service) AutoMigrate() error {
	if err := s.db.AutoMigrate(&Account{}, &Entry{}); err != nil {
		return err
	}
	// Seed chart of accounts minimal
	defaults := []Account{
		{Code: "1000", Name: "Kas", Type: "asset", Scope: "tenant"},
		{Code: "4000", Name: "Pendapatan Layanan", Type: "revenue", Scope: "tenant"},
		{Code: "2000", Name: "Hutang Platform", Type: "liability", Scope: "tenant"},
		{Code: "4100", Name: "Pendapatan Platform Fee", Type: "revenue", Scope: "superadmin"},
		{Code: "1010", Name: "Kas Platform", Type: "asset", Scope: "superadmin"},
	}
	for _, a := range defaults {
		var cnt int64
		s.db.Model(&Account{}).Where("code=?", a.Code).Count(&cnt)
		if cnt == 0 {
			s.db.Create(&a)
		}
	}
	return nil
}

// RecordPayment mencatat pembayaran invoice pelanggan:
// Debit Kas (1000) / Kredit Pendapatan (4000). platformFee masuk akun superadmin.
func (s *Service) RecordPayment(tenantID uint, amount, platformFee float64, ref string) error {
	now := time.Now()
	entries := []Entry{
		{TenantID: tenantID, Date: now, AccountCode: "1000", Debit: amount, Ref: ref, Memo: "Kas masuk " + ref},
		{TenantID: tenantID, Date: now, AccountCode: "4000", Credit: amount, Ref: ref, Memo: "Pendapatan " + ref},
	}
	if platformFee > 0 {
		// Fee untuk platform (superadmin)
		entries = append(entries,
			Entry{TenantID: 0, Date: now, AccountCode: "1010", Debit: platformFee, Ref: ref, Memo: "Platform fee " + ref},
			Entry{TenantID: 0, Date: now, AccountCode: "4100", Credit: platformFee, Ref: ref, Memo: "Platform fee " + ref},
		)
	}
	return s.db.Create(&entries).Error
}

// Balance saldo sebuah akun (debit - kredit untuk asset; kredit - debit untuk revenue/liability).
func (s *Service) Balance(tenantID uint, accountCode string) (float64, error) {
	var acc Account
	if err := s.db.Where("code=?", accountCode).First(&acc).Error; err != nil {
		return 0, err
	}
	var sum struct{ Debit, Credit float64 }
	s.db.Model(&Entry{}).Where("tenant_id=? AND account_code=?", tenantID, accountCode).
		Select("COALESCE(SUM(debit),0) as debit, COALESCE(SUM(credit),0) as credit").Scan(&sum)
	if acc.Type == "asset" || acc.Type == "expense" {
		return sum.Debit - sum.Credit, nil
	}
	return sum.Credit - sum.Debit, nil
}

// IncomeStatement laporan laba rugi sederhana per tenant (atau superadmin bila tenantID=0).
func (s *Service) IncomeStatement(tenantID uint) (revenue, expense, net float64, err error) {
	var revAccounts, expAccounts []Account
	s.db.Where("type=?", "revenue").Find(&revAccounts)
	s.db.Where("type=?", "expense").Find(&expAccounts)
	for _, a := range revAccounts {
		b, e := s.Balance(tenantID, a.Code)
		if e == nil {
			revenue += b
		}
	}
	for _, a := range expAccounts {
		b, e := s.Balance(tenantID, a.Code)
		if e == nil {
			expense += b
		}
	}
	net = revenue - expense
	return revenue, expense, net, nil
}

// Ledger riwayat jurnal per tenant.
func (s *Service) Ledger(tenantID uint, limit int) ([]Entry, error) {
	if limit <= 0 {
		limit = 100
	}
	var out []Entry
	err := s.db.Where("tenant_id=?", tenantID).Order("date DESC, id DESC").Limit(limit).Find(&out).Error
	return out, err
}
