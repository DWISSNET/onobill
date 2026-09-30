package worker

import (
	"log"
	"time"

	"onobill/internal/repository"
	"onobill/internal/service/billing"
	"onobill/internal/service/isolation"
	"onobill/internal/service/router"
)

// StartAll launches background workers
func StartAll(billingSvc *billing.Service, routerSvc *router.Service, isolationSvc *isolation.Service, repo *repository.Repository) {
	go overdueMarker(billingSvc, repo)
	go autoIsolator(isolationSvc, repo)
	go routerHealthCheck(routerSvc, repo)
}

// activeTenantIDs returns IDs of all tenants (scheduler berlaku untuk SEMUA tenant).
func activeTenantIDs(repo *repository.Repository) []uint {
	tenants, err := repo.ListTenants()
	if err != nil {
		log.Printf("[worker] list tenants: %v", err)
		return nil
	}
	ids := make([]uint, 0, len(tenants))
	for _, t := range tenants {
		ids = append(ids, t.ID)
	}
	return ids
}

// overdueMarker runs every hour to mark overdue invoices (semua tenant).
func overdueMarker(svc *billing.Service, repo *repository.Repository) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		for _, tid := range activeTenantIDs(repo) {
			n, err := svc.MarkOverdue(tid)
			if err != nil {
				log.Printf("[worker] overdue marker tenant %d: %v", tid, err)
			} else if n > 0 {
				log.Printf("[worker] tenant %d: marked %d invoices overdue", tid, n)
			}
		}
	}
}

// autoIsolator runs every hour to isolate customers with overdue invoices (semua tenant).
// It disables the customer's PPPoE secret on the router via RouterOS API.
func autoIsolator(svc *isolation.Service, repo *repository.Repository) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		for _, tid := range activeTenantIDs(repo) {
			n, err := svc.IsolateOverdue(tid)
			if err != nil {
				log.Printf("[worker] auto-isolate tenant %d: %v", tid, err)
			} else if n > 0 {
				log.Printf("[worker] tenant %d: auto-isolated %d customers", tid, n)
			}
		}
	}
}

// routerHealthCheck pings routers every 5 minutes (semua tenant).
func routerHealthCheck(svc *router.Service, repo *repository.Repository) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		for _, tid := range activeTenantIDs(repo) {
			routers, err := svc.List(tid)
			if err != nil {
				continue
			}
			for _, r := range routers {
				if svc.TestConnection(r.Host, r.Port) {
					svc.MarkSeen(r.TenantID, r.ID)
				}
			}
		}
	}
}
