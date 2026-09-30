package reseller

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newSvc(t *testing.T) *Service {
	// Pakai file temp agar transaksi GORM aman (menghindari deadlock :memory:)
	path := t.TempDir() + "/test.db"
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(db)
	s.AutoMigrate()
	return s
}

func TestTopUpAndBalance(t *testing.T) {
	s := newSvc(t)
	r, _ := s.Create(1, "Agen Budi", "0812", "", 10)
	if err := s.TopUp(1, r.ID, 100000, "deposit awal"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(1, r.ID)
	if got.Balance != 100000 {
		t.Fatalf("saldo salah: %.0f", got.Balance)
	}
}

func TestChargeSaleWithCommission(t *testing.T) {
	s := newSvc(t)
	r, _ := s.Create(1, "Agen", "", "", 10) // komisi 10%
	s.TopUp(1, r.ID, 100000, "")
	// jual voucher cost 50000 -> komisi 5000 -> saldo akhir 100000-50000+5000 = 55000
	commission, err := s.ChargeSale(1, r.ID, 50000, "VCR-001")
	if err != nil {
		t.Fatal(err)
	}
	if commission != 5000 {
		t.Fatalf("komisi salah: %.0f", commission)
	}
	got, _ := s.Get(1, r.ID)
	if got.Balance != 55000 {
		t.Fatalf("saldo akhir salah: %.0f", got.Balance)
	}
}

func TestChargeSaleInsufficient(t *testing.T) {
	s := newSvc(t)
	r, _ := s.Create(1, "Agen", "", "", 0)
	s.TopUp(1, r.ID, 10000, "")
	if _, err := s.ChargeSale(1, r.ID, 50000, "VCR-X"); err != ErrInsufficientBalance {
		t.Fatalf("expected ErrInsufficientBalance, got %v", err)
	}
}

func TestTransactions(t *testing.T) {
	s := newSvc(t)
	r, _ := s.Create(1, "Agen", "", "", 10)
	s.TopUp(1, r.ID, 100000, "")
	s.ChargeSale(1, r.ID, 50000, "VCR-1")
	txs, err := s.Transactions(1, r.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	// topup + sale + commission = 3 transaksi
	if len(txs) != 3 {
		t.Fatalf("expected 3 tx, got %d", len(txs))
	}
}
