// Package isolation provides customer isolation (isolir) operations.
// It disables/enables the customer's PPPoE secret on the MikroTik router
// via the RouterOS API, and syncs status to the local database.
package isolation

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/go-routeros/routeros/v3"
	"onobill/internal/domain"
	"onobill/internal/repository"
)

var (
	ErrNoRouter   = errors.New("customer has no router assigned")
	ErrNoUsername = errors.New("customer has no PPPoE username")
)

// Service performs isolir/unisolir operations against MikroTik routers.
type Service struct {
	repo *repository.Repository
	// DialTimeout is the TCP+TLS handshake timeout for RouterOS API.
	DialTimeout time.Duration
	// UseTLS toggles API-SSL (port 8729). Default false (plain 8728).
	UseTLS bool
}

// NewService creates an isolation service.
func NewService(repo *repository.Repository) *Service {
	return &Service{
		repo:        repo,
		DialTimeout: 10 * time.Second,
		UseTLS:      false,
	}
}

// Isolate disables the customer's PPPoE secret on the router and
// sets local customer status to "isolated". Idempotent.
func (s *Service) Isolate(tenantID, customerID uint) error {
	cust, err := s.repo.GetCustomerByID(tenantID, customerID)
	if err != nil {
		return fmt.Errorf("get customer: %w", err)
	}
	if cust == nil {
		return errors.New("customer not found")
	}

	// Already isolated — skip router call, just ensure status.
	if cust.Status == "isolated" {
		return nil
	}

	if cust.RouterID != nil && cust.Username != "" {
		if err := s.setPPPoESecretDisabled(cust, true); err != nil {
			// Log but don't fail — we still mark in DB so admin can retry.
			log.Printf("[isolation] failed to disable PPPoE secret for %s on router %d: %v",
				cust.Username, *cust.RouterID, err)
		}
	}

	cust.Status = "isolated"
	if err := s.repo.UpdateCustomer(cust); err != nil {
		return fmt.Errorf("update customer: %w", err)
	}
	return nil
}

// UnIsolate re-enables the PPPoE secret and sets status back to "active".
func (s *Service) UnIsolate(tenantID, customerID uint) error {
	cust, err := s.repo.GetCustomerByID(tenantID, customerID)
	if err != nil {
		return fmt.Errorf("get customer: %w", err)
	}
	if cust == nil {
		return errors.New("customer not found")
	}

	if cust.Status == "active" {
		return nil
	}

	if cust.RouterID != nil && cust.Username != "" {
		if err := s.setPPPoESecretDisabled(cust, false); err != nil {
			log.Printf("[isolation] failed to enable PPPoE secret for %s on router %d: %v",
				cust.Username, *cust.RouterID, err)
			// Don't update DB if router call fails — caller can retry.
			return fmt.Errorf("router enable failed: %w", err)
		}
	}

	cust.Status = "active"
	if err := s.repo.UpdateCustomer(cust); err != nil {
		return fmt.Errorf("update customer: %w", err)
	}
	return nil
}

// IsolateOverdue isolates all customers in tenant with overdue invoices.
// Returns count of newly isolated customers.
func (s *Service) IsolateOverdue(tenantID uint) (int, error) {
	invoices, err := s.repo.ListInvoices(tenantID, "overdue")
	if err != nil {
		return 0, err
	}
	seen := make(map[uint]bool)
	count := 0
	for _, inv := range invoices {
		if seen[inv.CustomerID] {
			continue
		}
		seen[inv.CustomerID] = true
		cust, err := s.repo.GetCustomerByID(tenantID, inv.CustomerID)
		if err != nil || cust == nil {
			continue
		}
		if cust.Status != "active" {
			continue
		}
		if err := s.Isolate(tenantID, cust.ID); err != nil {
			log.Printf("[isolation] isolate customer %d: %v", cust.ID, err)
			continue
		}
		count++
	}
	return count, nil
}

// setPPPoESecretDisabled toggles the disabled flag on a PPPoE secret.
func (s *Service) setPPPoESecretDisabled(cust *domain.Customer, disabled bool) error {
	router, err := s.repo.GetRouterByID(cust.TenantID, *cust.RouterID)
	if err != nil {
		return fmt.Errorf("get router: %w", err)
	}
	if router == nil {
		return ErrNoRouter
	}

	addr := router.APIAddr()
	var client *routeros.Client

	ctx, cancel := context.WithTimeout(context.Background(), s.DialTimeout)
	defer cancel()

	if s.UseTLS {
		client, err = routeros.DialTLSContext(ctx, addr, router.Username, router.Password, &tls.Config{
			InsecureSkipVerify: true, // RouterOS uses self-signed by default
		})
	} else {
		client, err = routeros.DialContext(ctx, addr, router.Username, router.Password)
	}
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer client.Close()

	// Find the secret ID by name.
	reply, err := client.Run("/ppp/secret/print", "?name="+cust.Username, "=.proplist=.id")
	if err != nil {
		return fmt.Errorf("find secret: %w", err)
	}
	if len(reply.Re) == 0 {
		return fmt.Errorf("pppoe secret '%s' not found on router", cust.Username)
	}
	secretID := reply.Re[0].Map[".id"]

	flag := "no"
	if disabled {
		flag = "yes"
	}
	if _, err := client.Run("/ppp/secret/set", "=.id="+secretID, "=disabled="+flag); err != nil {
		return fmt.Errorf("set secret: %w", err)
	}
	return nil
}
