package billing

import (
	"errors"
	"fmt"
	"onobill/internal/domain"
	"onobill/internal/repository"
	"time"
)

type Service struct {
	repo *repository.Repository
	// OnCustomerReactivate is called when a customer's status flips
	// from "isolated" back to "active" after a successful payment.
	// It should re-enable the customer's PPPoE secret on the router.
	// Nil-safe: no-op when unset.
	OnCustomerReactivate func(tenantID, customerID uint)
	// OnPaymentRecorded is called after an invoice is paid, for accounting ledger.
	// platformFee = fee yang diambil platform (superadmin). Nil-safe.
	OnPaymentRecorded func(tenantID uint, amount, platformFee float64, ref string)
}

func NewService(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

var (
	ErrInvoiceNotFound = errors.New("invoice not found")
	ErrAlreadyPaid     = errors.New("invoice already paid")
)

// CreateInvoice creates a new invoice for a customer
func (s *Service) CreateInvoice(tenantID, customerID uint, amount float64, dueDays int, notes string) (*domain.Invoice, error) {
	cust, err := s.repo.GetCustomerByID(tenantID, customerID)
	if err != nil {
		return nil, errors.New("customer not found")
	}

	if dueDays <= 0 {
		dueDays = 7
	}

	now := time.Now()
	number := s.generateNumber(tenantID, now)

	inv := &domain.Invoice{
		TenantID:   tenantID,
		CustomerID: cust.ID,
		Number:     number,
		Amount:     amount,
		Tax:        0,
		Total:      amount,
		Status:     "unpaid",
		IssuedAt:   now,
		DueAt:      now.AddDate(0, 0, dueDays),
		Notes:      notes,
	}
	if err := s.repo.CreateInvoice(inv); err != nil {
		return nil, err
	}
	return inv, nil
}

// CreateInvoiceFromPackage creates invoice from customer's package price
func (s *Service) CreateInvoiceFromPackage(tenantID, customerID uint) (*domain.Invoice, error) {
	cust, err := s.repo.GetCustomerByID(tenantID, customerID)
	if err != nil {
		return nil, errors.New("customer not found")
	}
	if cust.PackageID == nil {
		return nil, errors.New("customer has no package")
	}
	pkg, err := s.repo.GetPackageByID(tenantID, *cust.PackageID)
	if err != nil {
		return nil, errors.New("package not found")
	}

	now := time.Now()
	periodStart := now
	periodEnd := now.AddDate(0, 0, pkg.Duration)

	inv := &domain.Invoice{
		TenantID:    tenantID,
		CustomerID:  cust.ID,
		Number:      s.generateNumber(tenantID, now),
		Amount:      pkg.Price,
		Total:       pkg.Price,
		Status:      "unpaid",
		IssuedAt:    now,
		DueAt:       now.AddDate(0, 0, 7),
		PeriodStart: &periodStart,
		PeriodEnd:   &periodEnd,
		Notes:       "Invoice for " + pkg.Name,
	}
	if err := s.repo.CreateInvoice(inv); err != nil {
		return nil, err
	}
	return inv, nil
}

func (s *Service) generateNumber(tenantID uint, t time.Time) string {
	count, _ := s.repo.CountInvoices(tenantID, "")
	return fmt.Sprintf("INV-%s-%05d", t.Format("20060102"), count+1)
}

// PayInvoice records a payment and marks invoice as paid
func (s *Service) PayInvoice(tenantID, invoiceID uint, amount float64, method, reference string) (*domain.Payment, error) {
	inv, err := s.repo.GetInvoiceByID(tenantID, invoiceID)
	if err != nil {
		return nil, ErrInvoiceNotFound
	}
	if inv.Status == "paid" {
		return nil, ErrAlreadyPaid
	}

	payment := &domain.Payment{
		TenantID:  tenantID,
		InvoiceID: invoiceID,
		Amount:    amount,
		Method:    method,
		Reference: reference,
		PaidAt:    time.Now(),
	}
	if err := s.repo.CreatePayment(payment); err != nil {
		return nil, err
	}

	// Mark as paid
	now := time.Now()
	inv.Status = "paid"
	inv.PaidAt = &now
	if err := s.repo.UpdateInvoice(inv); err != nil {
		return nil, err
	}

	// Extend customer due date if applicable
	if inv.CustomerID > 0 {
		cust, err := s.repo.GetCustomerByID(tenantID, inv.CustomerID)
		if err == nil && cust != nil {
			wasIsolated := cust.Status == "isolated"
			newDue := now.AddDate(0, 0, 30)
			cust.DueDate = &newDue
			if wasIsolated {
				cust.Status = "active"
			}
			s.repo.UpdateCustomer(cust)
			// Fire reactivation hook to re-enable PPPoE secret on router.
			if wasIsolated && s.OnCustomerReactivate != nil {
				go s.OnCustomerReactivate(tenantID, cust.ID)
			}
		}
	}

	// Accounting ledger hook
	if s.OnPaymentRecorded != nil {
		platformFee := amount * 0.05 // fee platform 5% (bisa dikonfigurasi nanti)
		go s.OnPaymentRecorded(tenantID, amount, platformFee, inv.Number)
	}

	return payment, nil
}

// GetInvoice retrieves invoice by ID
func (s *Service) GetInvoice(tenantID, id uint) (*domain.Invoice, error) {
	return s.repo.GetInvoiceByID(tenantID, id)
}

// ListInvoices lists invoices with optional status filter
func (s *Service) ListInvoices(tenantID uint, status string) ([]domain.Invoice, error) {
	return s.repo.ListInvoices(tenantID, status)
}

// ListPayments lists recent payments
func (s *Service) ListPayments(tenantID uint, limit int) ([]domain.Payment, error) {
	return s.repo.ListPayments(tenantID, limit)
}

// MarkOverdue marks invoices as overdue if past due date
func (s *Service) MarkOverdue(tenantID uint) (int, error) {
	invoices, err := s.repo.ListInvoices(tenantID, "unpaid")
	if err != nil {
		return 0, err
	}
	now := time.Now()
	count := 0
	for _, inv := range invoices {
		if inv.DueAt.Before(now) {
			inv.Status = "overdue"
			s.repo.UpdateInvoice(&inv)
			count++
		}
	}
	return count, nil
}

// Stats returns billing stats for dashboard
func (s *Service) Stats(tenantID uint) map[string]interface{} {
	revenue, _ := s.repo.SumRevenue(tenantID)
	unpaid, _ := s.repo.CountInvoices(tenantID, "unpaid")
	overdue, _ := s.repo.CountInvoices(tenantID, "overdue")
	paid, _ := s.repo.CountInvoices(tenantID, "paid")
	return map[string]interface{}{
		"total_revenue": revenue,
		"unpaid_count":  unpaid,
		"overdue_count": overdue,
		"paid_count":    paid,
	}
}

// IsolateOverdueCustomers sets customers with overdue invoices to "isolated"
func (s *Service) IsolateOverdueCustomers(tenantID uint) (int, error) {
	invoices, err := s.repo.ListInvoices(tenantID, "overdue")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, inv := range invoices {
		cust, err := s.repo.GetCustomerByID(tenantID, inv.CustomerID)
		if err != nil || cust == nil {
			continue
		}
		if cust.Status == "active" {
			cust.Status = "isolated"
			s.repo.UpdateCustomer(cust)
			count++
		}
	}
	return count, nil
}
