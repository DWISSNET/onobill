// Package subscription — manajemen langganan tenant ke platform ONOBILL.
// Jantung alur bisnis Superadmin:
//
//	daftar -> bayar via payment gateway -> aktif otomatis
//	menunggak -> grace 7 hari + peringatan WA -> suspend (login tenant dikunci)
package subscription

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"onobill/internal/domain"
)

const GraceDays = 7

var (
	ErrNotFound    = errors.New("langganan tidak ditemukan")
	ErrAlreadyPaid = errors.New("langganan sudah lunas periode ini")
)

// Plan paket langganan tenant.
type Plan struct {
	Name         string
	Price        float64
	MaxRouter    int
	MaxPelanggan int
}

// Plans daftar paket yang dijual superadmin.
var Plans = map[string]Plan{
	"basic":      {Name: "Basic", Price: 50000, MaxRouter: 1, MaxPelanggan: 100},
	"pro":        {Name: "Pro", Price: 150000, MaxRouter: 5, MaxPelanggan: 1000},
	"enterprise": {Name: "Enterprise", Price: 400000, MaxRouter: 50, MaxPelanggan: 10000},
}

type Service struct{ db *gorm.DB }

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

func (s *Service) AutoMigrate() error {
	return s.db.AutoMigrate(&domain.Subscription{})
}

// CreateForTenant membuat langganan baru (status menunggu pembayaran).
// current_end diset 0 (belum aktif) sampai pembayaran pertama lunas.
func (s *Service) CreateForTenant(tenantID uint, plan string) (*domain.Subscription, error) {
	p, ok := Plans[plan]
	if !ok {
		plan = "basic"
		p = Plans["basic"]
	}
	sub := &domain.Subscription{
		TenantID:   tenantID,
		Plan:       plan,
		Amount:     p.Price,
		Status:     "pending_payment", // belum aktif sampai bayar
		CurrentEnd: time.Now(),        // akan diperpanjang saat bayar
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	return sub, s.db.Create(sub).Error
}

func (s *Service) GetByTenant(tenantID uint) (*domain.Subscription, error) {
	var sub domain.Subscription
	if err := s.db.Where("tenant_id=?", tenantID).Order("id DESC").First(&sub).Error; err != nil {
		return nil, ErrNotFound
	}
	return &sub, nil
}

// ActivateOnPayment dipanggil saat invoice langganan LUNAS via payment gateway.
// Memperpanjang periode +30 hari & mengaktifkan tenant. (Full self-service: langsung aktif.)
func (s *Service) ActivateOnPayment(subID uint) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var sub domain.Subscription
		if err := tx.First(&sub, subID).Error; err != nil {
			return ErrNotFound
		}
		// Perpanjang dari sekarang (atau dari current_end bila masih aktif)
		base := time.Now()
		if sub.CurrentEnd.After(base) {
			base = sub.CurrentEnd
		}
		updates := map[string]interface{}{
			"status":      "active",
			"current_end": base.AddDate(0, 0, 30),
			"grace_until": nil,
			"updated_at":  time.Now(),
		}
		if err := tx.Model(&domain.Subscription{}).Where("id=?", sub.ID).Updates(updates).Error; err != nil {
			return err
		}
		// Aktifkan tenant
		return tx.Model(&domain.Tenant{}).Where("id=?", sub.TenantID).
			Updates(map[string]interface{}{"status": "active", "updated_at": time.Now()}).Error
	})
}

// ProcessOverdue dipanggil worker berkala: tandai grace (H+1..H+7) & suspend (lebih dari H+7).
// Mengembalikan jumlah tenant yang (baru grace, baru suspend).
func (s *Service) ProcessOverdue() (newGrace, newSuspended int, err error) {
	now := time.Now()
	var subs []domain.Subscription
	if err := s.db.Where("status IN ?", []string{"active", "grace"}).Find(&subs).Error; err != nil {
		return 0, 0, err
	}
	for _, sub := range subs {
		if now.Before(sub.CurrentEnd) {
			continue // masih aktif, belum jatuh tempo
		}
		overdue := now.Sub(sub.CurrentEnd)
		if overdue <= time.Duration(GraceDays)*24*time.Hour {
			// Masih dalam grace period
			if sub.Status != "grace" {
				grace := sub.CurrentEnd.AddDate(0, 0, GraceDays)
				s.db.Model(&sub).Updates(map[string]interface{}{
					"status": "grace", "grace_until": grace, "updated_at": now,
				})
				s.db.Model(&domain.Tenant{}).Where("id=?", sub.TenantID).Update("status", "grace")
				newGrace++
			}
		} else {
			// Lewat grace -> suspend (kunci login tenant)
			if sub.Status != "suspended" {
				s.db.Model(&sub).Updates(map[string]interface{}{"status": "suspended", "updated_at": now})
				s.db.Model(&domain.Tenant{}).Where("id=?", sub.TenantID).Update("status", "suspended")
				newSuspended++
			}
		}
	}
	return newGrace, newSuspended, nil
}

// DueSoon mengembalikan langganan yang jatuh tempo dalam N hari (untuk pengingat WA H-3).
func (s *Service) DueSoon(withinDays int) ([]domain.Subscription, error) {
	now := time.Now()
	limit := now.AddDate(0, 0, withinDays)
	var out []domain.Subscription
	err := s.db.Where("status='active' AND current_end BETWEEN ? AND ?", now, limit).Find(&out).Error
	return out, err
}

// Stats ringkasan untuk dashboard superadmin.
func (s *Service) Stats() (active, grace, suspended int, mrr float64, err error) {
	var subs []domain.Subscription
	if err := s.db.Find(&subs).Error; err != nil {
		return 0, 0, 0, 0, err
	}
	for _, sub := range subs {
		switch sub.Status {
		case "active":
			active++
			mrr += sub.Amount
		case "grace":
			grace++
		case "suspended":
			suspended++
		}
	}
	return active, grace, suspended, mrr, nil
}

// UpdatePlan mengganti paket langganan tenant (dipanggil superadmin).
// amount mengikuti harga paket baru. Status & periode tidak diubah.
func (s *Service) UpdatePlan(tenantID uint, plan string) error {
	p, ok := Plans[plan]
	if !ok {
		return errors.New("paket tidak dikenal: " + plan)
	}
	sub, err := s.GetByTenant(tenantID)
	if err != nil {
		return err
	}
	return s.db.Model(&domain.Subscription{}).Where("id=?", sub.ID).
		Updates(map[string]interface{}{
			"plan":       plan,
			"amount":     p.Price,
			"updated_at": time.Now(),
		}).Error
}
