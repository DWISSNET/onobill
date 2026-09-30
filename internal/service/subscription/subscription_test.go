package subscription

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"onobill/internal/domain"
)

func newSvc(t *testing.T) *Service {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/sub.db"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(&domain.Tenant{}, &domain.Subscription{})
	db.Create(&domain.Tenant{ID: 1, Name: "ISP A", Slug: "isp-a", Status: "active"})
	s := NewService(db)
	s.AutoMigrate()
	return s
}

func TestCreateForTenantDefaultPlan(t *testing.T) {
	s := newSvc(t)
	sub, err := s.CreateForTenant(1, "tidak-ada")
	if err != nil {
		t.Fatal(err)
	}
	if sub.Plan != "basic" || sub.Amount != 50000 {
		t.Fatalf("default plan salah: %s %.0f", sub.Plan, sub.Amount)
	}
	if sub.Status != "pending_payment" {
		t.Fatalf("harus pending_payment, got %s", sub.Status)
	}
}

func TestActivateOnPayment(t *testing.T) {
	s := newSvc(t)
	sub, _ := s.CreateForTenant(1, "pro")
	if err := s.ActivateOnPayment(sub.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetByTenant(1)
	if got.Status != "active" {
		t.Fatalf("harus active, got %s", got.Status)
	}
	if got.CurrentEnd.Before(time.Now().AddDate(0, 0, 29)) {
		t.Fatal("current_end harus ~30 hari ke depan")
	}
	var tenant domain.Tenant
	s.db.First(&tenant, 1)
	if tenant.Status != "active" {
		t.Fatalf("tenant harus active, got %s", tenant.Status)
	}
}

func TestProcessOverdueGraceThenSuspend(t *testing.T) {
	s := newSvc(t)
	sub, _ := s.CreateForTenant(1, "basic")
	s.ActivateOnPayment(sub.ID)
	// Paksa jatuh tempo 3 hari lalu (masih dalam grace 7 hari)
	s.db.Model(&domain.Subscription{}).Where("id=?", sub.ID).
		Update("current_end", time.Now().AddDate(0, 0, -3))
	g, sus, err := s.ProcessOverdue()
	if err != nil {
		t.Fatal(err)
	}
	if g != 1 || sus != 0 {
		t.Fatalf("harus 1 grace 0 suspend, got %d %d", g, sus)
	}
	// Paksa lewat grace (10 hari)
	s.db.Model(&domain.Subscription{}).Where("id=?", sub.ID).
		Updates(map[string]interface{}{"current_end": time.Now().AddDate(0, 0, -10), "status": "grace"})
	g2, sus2, _ := s.ProcessOverdue()
	if sus2 != 1 {
		t.Fatalf("harus suspend, got grace=%d suspend=%d", g2, sus2)
	}
	var tenant domain.Tenant
	s.db.First(&tenant, 1)
	if tenant.Status != "suspended" {
		t.Fatalf("tenant harus suspended, got %s", tenant.Status)
	}
}

func TestStatsMRR(t *testing.T) {
	s := newSvc(t)
	sub1, _ := s.CreateForTenant(1, "pro") // 150000
	s.ActivateOnPayment(sub1.ID)
	active, grace, suspended, mrr, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if active != 1 || mrr != 150000 {
		t.Fatalf("stats salah: active=%d mrr=%.0f (grace=%d sus=%d)", active, mrr, grace, suspended)
	}
}
