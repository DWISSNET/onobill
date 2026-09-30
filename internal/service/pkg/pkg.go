package pkg

import (
	"errors"
	"strings"

	"onobill/internal/domain"
	"onobill/internal/repository"
)

type Service struct {
	repo *repository.Repository
}

func NewService(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

var ErrNotFound = errors.New("paket tidak ditemukan")

// Input holds input for create/update package.
type Input struct {
	Name         string
	ServiceType  string
	Description  string
	Price        float64
	SpeedDown    int
	SpeedUp      int
	QuotaGB      int
	ValidityDays int
	ProfileName  string
	AddressList  string
	ParentQueue  string
	PoolName     string
	IsActive     bool
}

func validServiceType(t string) bool {
	switch t {
	case "pppoe", "hotspot":
		return true
	}
	return false
}

func (i *Input) normalize() {
	i.ServiceType = strings.ToLower(strings.TrimSpace(i.ServiceType))
	i.Name = strings.TrimSpace(i.Name)
	if i.ValidityDays <= 0 {
		i.ValidityDays = 30
	}
}

func (s *Service) validate(in *Input) error {
	if in.Name == "" {
		return errors.New("nama paket wajib diisi")
	}
	if !validServiceType(in.ServiceType) {
		return errors.New("tipe layanan harus pppoe atau hotspot")
	}
	if in.Price < 0 {
		return errors.New("harga tidak boleh negatif")
	}
	return nil
}

func (s *Service) Create(tenantID uint, in Input) (*domain.Package, error) {
	in.normalize()
	if err := s.validate(&in); err != nil {
		return nil, err
	}
	p := &domain.Package{
		TenantID:    tenantID,
		Name:        in.Name,
		Type:        in.ServiceType,
		Description: in.Description,
		Price:       in.Price,
		SpeedDown:   in.SpeedDown,
		SpeedUp:     in.SpeedUp,
		Duration:    in.ValidityDays,
		IsActive:    in.IsActive,
	}
	if err := s.repo.CreatePackage(p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) Update(tenantID, id uint, in Input) (*domain.Package, error) {
	in.normalize()
	if err := s.validate(&in); err != nil {
		return nil, err
	}
	p, err := s.repo.GetPackageByID(tenantID, id)
	if err != nil {
		return nil, ErrNotFound
	}
	p.Name = in.Name
	p.Type = in.ServiceType
	p.Description = in.Description
	p.Price = in.Price
	p.SpeedDown = in.SpeedDown
	p.SpeedUp = in.SpeedUp
	p.Duration = in.ValidityDays
	p.IsActive = in.IsActive
	if err := s.repo.UpdatePackage(p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) List(tenantID uint) ([]domain.Package, error) {
	return s.repo.ListPackages(tenantID)
}

func (s *Service) GetByID(tenantID, id uint) (*domain.Package, error) {
	p, err := s.repo.GetPackageByID(tenantID, id)
	if err != nil {
		return nil, ErrNotFound
	}
	return p, nil
}

func (s *Service) Delete(tenantID, id uint) error {
	if _, err := s.repo.GetPackageByID(tenantID, id); err != nil {
		return ErrNotFound
	}
	return s.repo.DeletePackage(tenantID, id)
}

// Stats returns per-type package counts for the page header.
func (s *Service) Stats(tenantID uint) map[string]int64 {
	var pppoe, hotspot, active int64
	s.repo.DB().Model(&domain.Package{}).Where("tenant_id = ? AND type = 'pppoe'", tenantID).Count(&pppoe)
	s.repo.DB().Model(&domain.Package{}).Where("tenant_id = ? AND type = 'hotspot'", tenantID).Count(&hotspot)
	s.repo.DB().Model(&domain.Package{}).Where("tenant_id = ? AND is_active = 1", tenantID).Count(&active)
	return map[string]int64{"pppoe": pppoe, "hotspot": hotspot, "active": active}
}
