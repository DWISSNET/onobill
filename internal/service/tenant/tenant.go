package tenant

import (
	"errors"
	"onobill/internal/domain"
	"onobill/internal/repository"
	"regexp"
	"strings"
)

type Service struct {
	repo *repository.Repository
}

func NewService(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

var ErrNotFound = errors.New("tenant not found")

func (s *Service) Create(name string) (*domain.Tenant, error) {
	slug := slugify(name)
	t := &domain.Tenant{Name: name, Slug: slug}
	if err := s.repo.CreateTenant(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) Get(id uint) (*domain.Tenant, error) {
	t, err := s.repo.GetTenantByID(id)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return t, nil
}

func (s *Service) List() ([]domain.Tenant, error) {
	return s.repo.ListTenants()
}

func (s *Service) Update(id uint, name string) (*domain.Tenant, error) {
	t, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	t.Name = name
	if err := s.repo.UpdateTenant(t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) Delete(id uint) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	return s.repo.DeleteTenant(id)
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
