package wallet

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"onobill/internal/domain"
)

func testService(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:wallettest-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Tenant{}, &domain.Wallet{}, &domain.WalletLedger{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.Tenant{Name: "Tenant Test", Slug: fmt.Sprintf("tenant-test-%d", time.Now().UnixNano())}).Error; err != nil {
		t.Fatal(err)
	}
	return NewService(db)
}

func TestCreditTopUpIsIdempotent(t *testing.T) {
	s := testService(t)
	if err := s.CreditTopUp(1, "MC-TU-1", 50000); err != nil {
		t.Fatal(err)
	}
	if err := s.CreditTopUp(1, "MC-TU-1", 50000); !errors.Is(err, ErrDuplicateReference) {
		t.Fatalf("expected duplicate, got %v", err)
	}
	w, err := s.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance != 50000 {
		t.Fatalf("balance=%d, want 50000", w.Balance)
	}
	rows, err := s.Ledger(1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("ledger rows=%d, want 1", len(rows))
	}
}

func TestDebitRejectsInsufficientFunds(t *testing.T) {
	s := testService(t)
	if err := s.CreditTopUp(1, "MC-TU-2", 10000); err != nil {
		t.Fatal(err)
	}
	if err := s.Debit(1, "subscription", "SUB-1", 10001, "renewal"); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("expected insufficient funds, got %v", err)
	}
	w, err := s.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance != 10000 {
		t.Fatalf("balance=%d, want 10000", w.Balance)
	}
}

func TestDebitIsIdempotent(t *testing.T) {
	s := testService(t)
	if err := s.CreditTopUp(1, "MC-TU-3", 20000); err != nil {
		t.Fatal(err)
	}
	if err := s.Debit(1, "subscription", "SUB-2", 12000, "renewal"); err != nil {
		t.Fatal(err)
	}
	if err := s.Debit(1, "subscription", "SUB-2", 12000, "renewal"); !errors.Is(err, ErrDuplicateReference) {
		t.Fatalf("expected duplicate, got %v", err)
	}
	w, err := s.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if w.Balance != 8000 {
		t.Fatalf("balance=%d, want 8000", w.Balance)
	}
}
