package accounting

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newSvc(t *testing.T) *Service {
	db, _ := gorm.Open(sqlite.Open(t.TempDir()+"/a.db"), &gorm.Config{})
	s := NewService(db)
	s.AutoMigrate()
	return s
}

func TestRecordPaymentBalance(t *testing.T) {
	s := newSvc(t)
	if err := s.RecordPayment(1, 100000, 0, "INV-1"); err != nil {
		t.Fatal(err)
	}
	kas, _ := s.Balance(1, "1000")
	pend, _ := s.Balance(1, "4000")
	if kas != 100000 || pend != 100000 {
		t.Fatalf("kas=%.0f pend=%.0f", kas, pend)
	}
}

func TestPlatformFeeSeparate(t *testing.T) {
	s := newSvc(t)
	// Pembayaran 100rb, fee platform 5rb
	s.RecordPayment(1, 100000, 5000, "INV-2")
	// Tenant dapat pendapatan penuh
	pend, _ := s.Balance(1, "4000")
	if pend != 100000 {
		t.Fatalf("pendapatan tenant salah: %.0f", pend)
	}
	// Superadmin (tenantID=0) punya pendapatan fee
	feeRev, _ := s.Balance(0, "4100")
	if feeRev != 5000 {
		t.Fatalf("platform fee salah: %.0f", feeRev)
	}
}

func TestIncomeStatement(t *testing.T) {
	s := newSvc(t)
	s.RecordPayment(1, 100000, 0, "INV-A")
	s.RecordPayment(1, 50000, 0, "INV-B")
	rev, exp, net, err := s.IncomeStatement(1)
	if err != nil {
		t.Fatal(err)
	}
	if rev != 150000 || exp != 0 || net != 150000 {
		t.Fatalf("rev=%.0f exp=%.0f net=%.0f", rev, exp, net)
	}
}

func TestTenantIsolation(t *testing.T) {
	s := newSvc(t)
	s.RecordPayment(1, 100000, 0, "INV-1")
	s.RecordPayment(2, 200000, 0, "INV-2")
	rev1, _, _, _ := s.IncomeStatement(1)
	rev2, _, _, _ := s.IncomeStatement(2)
	if rev1 != 100000 || rev2 != 200000 {
		t.Fatalf("isolasi tenant gagal: rev1=%.0f rev2=%.0f", rev1, rev2)
	}
}
