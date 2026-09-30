package customer

import (
	"errors"
	"fmt"
	"onobill/internal/domain"
	"onobill/internal/repository"
	"time"
)

type Service struct {
	repo *repository.Repository
}

func NewService(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

var ErrNotFound = errors.New("customer not found")

// CreateCustomerInput holds input for creating customer
type CreateCustomerInput struct {
	Name        string
	Email       string
	Phone       string
	Address     string
	Username    string
	Password    string
	ServiceType string
	RouterID    *uint
	PackageID   *uint
}

func (s *Service) Create(tenantID uint, in CreateCustomerInput) (*domain.Customer, error) {
	code := s.generateCode(tenantID)
	c := &domain.Customer{
		TenantID:    tenantID,
		RouterID:    in.RouterID,
		PackageID:   in.PackageID,
		Code:        code,
		Name:        in.Name,
		Email:       in.Email,
		Phone:       in.Phone,
		Address:     in.Address,
		Username:    in.Username,
		Password:    in.Password,
		ServiceType: in.ServiceType,
		Status:      "active",
		JoinedAt:    time.Now(),
	}
	if c.ServiceType == "" {
		c.ServiceType = "pppoe"
	}
	// Set due date 30 days from now
	due := time.Now().AddDate(0, 0, 30)
	c.DueDate = &due

	if err := s.repo.CreateCustomer(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) generateCode(tenantID uint) string {
	count, _ := s.repo.CountCustomers(tenantID)
	return fmt.Sprintf("CUST-%05d", count+1)
}

func (s *Service) Get(tenantID, id uint) (*domain.Customer, error) {
	c, err := s.repo.GetCustomerByID(tenantID, id)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

func (s *Service) List(tenantID uint, status, keyword string) ([]domain.Customer, error) {
	if keyword != "" {
		return s.repo.SearchCustomers(tenantID, keyword)
	}
	return s.repo.ListCustomers(tenantID, status)
}

func (s *Service) Update(tenantID, id uint, in CreateCustomerInput) (*domain.Customer, error) {
	c, err := s.Get(tenantID, id)
	if err != nil {
		return nil, err
	}
	c.Name = in.Name
	c.Email = in.Email
	c.Phone = in.Phone
	c.Address = in.Address
	c.Username = in.Username
	if in.Password != "" {
		c.Password = in.Password
	}
	c.ServiceType = in.ServiceType
	c.RouterID = in.RouterID
	c.PackageID = in.PackageID
	if err := s.repo.UpdateCustomer(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) SetStatus(tenantID, id uint, status string) (*domain.Customer, error) {
	c, err := s.Get(tenantID, id)
	if err != nil {
		return nil, err
	}
	c.Status = status
	if err := s.repo.UpdateCustomer(c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) Delete(tenantID, id uint) error {
	if _, err := s.Get(tenantID, id); err != nil {
		return err
	}
	return s.repo.DeleteCustomer(tenantID, id)
}

func (s *Service) Count(tenantID uint) map[string]int64 {
	total, _ := s.repo.CountCustomers(tenantID)
	active, _ := s.repo.CountCustomersByStatus(tenantID, "active")
	isolated, _ := s.repo.CountCustomersByStatus(tenantID, "isolated")
	suspended, _ := s.repo.CountCustomersByStatus(tenantID, "suspended")
	return map[string]int64{
		"total":     total,
		"active":    active,
		"isolated":  isolated,
		"suspended": suspended,
	}
}
