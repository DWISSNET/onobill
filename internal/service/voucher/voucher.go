package voucher

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"onobill/internal/domain"
	"onobill/internal/repository"

	"gorm.io/gorm"
)

type Service struct {
	repo *repository.Repository
}

func NewService(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

var (
	ErrNotFound   = errors.New("voucher not found")
	ErrCodeExists = errors.New("voucher code already exists")
)

// GenerateInput berisi parameter untuk generate satu batch voucher.
type GenerateInput struct {
	Count        int    // jumlah voucher yang digenerate (1..1000)
	Prefix       string // awalan kode, mis. "WIFI"
	CodeLength   int    // panjang bagian acak kode (4..20)
	Profile      string // nama profile hotspot MikroTik
	RouterID     *uint  // router tujuan (opsional)
	PackageID    *uint  // paket terkait (opsional)
	ValidityDays int    // masa berlaku dalam hari sejak dibuat (0 = tanpa expiry)
}

// charset tanpa karakter ambigu (0/O, 1/I/L) agar mudah diketik pelanggan.
const charset = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func randomCode(n int) (string, error) {
	if n < 4 {
		n = 4
	}
	if n > 20 {
		n = 20
	}
	b := make([]byte, n)
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	for i := range raw {
		b[i] = charset[int(raw[i])%len(charset)]
	}
	return string(b), nil
}

// GenerateBatch membuat `in.Count` voucher sekaligus dalam satu transaksi DB.
// Kode dijamin unik: dicek terhadap DB, dan retry dengan kode baru bila bentrok.
func (s *Service) GenerateBatch(tenantID uint, in GenerateInput) ([]domain.Voucher, error) {
	if in.Count < 1 || in.Count > 1000 {
		return nil, fmt.Errorf("jumlah voucher harus antara 1 dan 1000")
	}
	if strings.TrimSpace(in.Profile) == "" {
		return nil, fmt.Errorf("profile wajib diisi")
	}
	prefix := strings.ToUpper(strings.TrimSpace(in.Prefix))

	var expiredAt *time.Time
	if in.ValidityDays > 0 {
		t := time.Now().AddDate(0, 0, in.ValidityDays)
		expiredAt = &t
	}

	vouchers := make([]domain.Voucher, 0, in.Count)
	seen := map[string]bool{}

	err := s.repo.DB().Transaction(func(tx *gorm.DB) error {
		for len(vouchers) < in.Count {
			code, err := randomCode(in.CodeLength)
			if err != nil {
				return err
			}
			if prefix != "" {
				code = prefix + code
			}
			if seen[code] {
				continue
			}
			// Cek unik terhadap DB (ada uniqueIndex di kolom code).
			var n int64
			if err := tx.Model(&domain.Voucher{}).Where("code = ?", code).Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			seen[code] = true
			vouchers = append(vouchers, domain.Voucher{
				TenantID:  tenantID,
				RouterID:  in.RouterID,
				PackageID: in.PackageID,
				Code:      code,
				Password:  code, // hotspot umumnya user=pass; bisa diubah kemudian
				Profile:   in.Profile,
				Status:    "unused",
				ExpiredAt: expiredAt,
			})
		}
		return tx.Create(&vouchers).Error
	})
	if err != nil {
		return nil, err
	}
	return vouchers, nil
}

func (s *Service) List(tenantID uint, status string) ([]domain.Voucher, error) {
	return s.repo.ListVouchers(tenantID, status)
}

func (s *Service) Stats(tenantID uint) (map[string]int64, error) {
	rows := []struct {
		Status string
		N      int64
	}{}
	if err := s.repo.DB().Model(&domain.Voucher{}).
		Select("status, COUNT(*) as n").
		Where("tenant_id = ?", tenantID).
		Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]int64{"total": 0, "unused": 0, "active": 0, "expired": 0, "disabled": 0}
	for _, r := range rows {
		out[r.Status] = r.N
		out["total"] += r.N
	}
	return out, nil
}

// Disable mengubah status voucher menjadi disabled (hanya jika belum dipakai).
func (s *Service) Disable(tenantID, id uint) error {
	var v domain.Voucher
	if err := s.repo.DB().Where("tenant_id = ? AND id = ?", tenantID, id).First(&v).Error; err != nil {
		if repository.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	if v.Status == "active" {
		return fmt.Errorf("voucher sudah dipakai, tidak bisa dinonaktifkan")
	}
	v.Status = "disabled"
	return s.repo.UpdateVoucher(&v)
}

// Reset mengembalikan voucher ke status unused dan membersihkan UsedAt.
func (s *Service) Reset(tenantID, id uint) error {
	var v domain.Voucher
	if err := s.repo.DB().Where("tenant_id = ? AND id = ?", tenantID, id).First(&v).Error; err != nil {
		if repository.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	v.Status = "unused"
	v.UsedAt = nil
	return s.repo.UpdateVoucher(&v)
}

// Delete menghapus voucher (hanya jika belum pernah dipakai).
func (s *Service) Delete(tenantID, id uint) error {
	var v domain.Voucher
	if err := s.repo.DB().Where("tenant_id = ? AND id = ?", tenantID, id).First(&v).Error; err != nil {
		if repository.IsNotFound(err) {
			return ErrNotFound
		}
		return err
	}
	if v.Status == "active" {
		return fmt.Errorf("voucher sudah dipakai, tidak bisa dihapus")
	}
	return s.repo.DeleteVoucher(tenantID, id)
}
