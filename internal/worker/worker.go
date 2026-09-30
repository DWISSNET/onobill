package worker

import (
	"context"
	"log"
	"time"

	"onobill/internal/repository"
	"onobill/internal/service/backup"
	"onobill/internal/service/billing"
	"onobill/internal/service/isolation"
	"onobill/internal/service/notify"
	"onobill/internal/service/router"
)

// StartAll launches background workers.
// ctx dibatalkan saat server shutdown agar worker berhenti bersih (graceful).
func StartAll(ctx context.Context, billingSvc *billing.Service, routerSvc *router.Service, isolationSvc *isolation.Service, repo *repository.Repository, notifySvc *notify.Service, baseURL string, backupSvc *backup.Service) {
	go recurringInvoice(ctx, billingSvc, repo)
	go overdueMarker(ctx, billingSvc, repo)
	go autoIsolator(ctx, isolationSvc, repo)
	go routerHealthCheck(ctx, routerSvc, repo)
	go dueReminder(ctx, billingSvc, notifySvc, repo, baseURL)
	go autoBackup(ctx, backupSvc)
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

// run menjalankan fn segera, lalu tiap interval; berhenti bersih saat ctx done.
func run(ctx context.Context, every time.Duration, fn func()) {
	fn()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}

// recurringInvoice membuat invoice baru untuk pelanggan aktif tiap hari (idempoten).
// Mengisi janji PRD: "invoice otomatis berulang".
func recurringInvoice(ctx context.Context, svc *billing.Service, repo *repository.Repository) {
	run(ctx, 24*time.Hour, func() {
		for _, tid := range activeTenantIDs(repo) {
			n, err := svc.GenerateRecurringInvoices(tid)
			if err != nil {
				log.Printf("[worker] recurring invoice tenant %d: %v", tid, err)
			} else if n > 0 {
				log.Printf("[worker] tenant %d: %d invoice berulang dibuat", tid, n)
			}
		}
	})
}

// overdueMarker runs every hour to mark overdue invoices (semua tenant).
func overdueMarker(ctx context.Context, svc *billing.Service, repo *repository.Repository) {
	run(ctx, time.Hour, func() {
		for _, tid := range activeTenantIDs(repo) {
			n, err := svc.MarkOverdue(tid)
			if err != nil {
				log.Printf("[worker] overdue marker tenant %d: %v", tid, err)
			} else if n > 0 {
				log.Printf("[worker] tenant %d: marked %d invoices overdue", tid, n)
			}
		}
	})
}

// autoIsolator runs every hour to isolate customers with overdue invoices (semua tenant).
// It disables the customer's PPPoE secret on the router via RouterOS API.
func autoIsolator(ctx context.Context, svc *isolation.Service, repo *repository.Repository) {
	run(ctx, time.Hour, func() {
		for _, tid := range activeTenantIDs(repo) {
			n, err := svc.IsolateOverdue(tid)
			if err != nil {
				log.Printf("[worker] auto-isolate tenant %d: %v", tid, err)
			} else if n > 0 {
				log.Printf("[worker] tenant %d: auto-isolated %d customers", tid, n)
			}
		}
	})
}

// routerHealthCheck pings routers every 5 minutes (semua tenant).
func routerHealthCheck(ctx context.Context, svc *router.Service, repo *repository.Repository) {
	run(ctx, 5*time.Minute, func() {
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
	})
}

// autoBackup mencadangkan DB tiap 24 jam (retensi diatur di service).
func autoBackup(ctx context.Context, svc *backup.Service) {
	if svc == nil {
		return
	}
	run(ctx, 24*time.Hour, func() {
		if err := svc.Run(); err != nil {
			log.Printf("[worker] backup error: %v", err)
		}
	})
}

// dueReminder mengirim pengingat H-3 sebelum jatuh tempo via WA tiap 12 jam.
// Mengisi janji PRD: "reminder H-3 jatuh tempo".
func dueReminder(ctx context.Context, billingSvc *billing.Service, notifySvc *notify.Service, repo *repository.Repository, baseURL string) {
	if notifySvc == nil {
		return
	}
	run(ctx, 12*time.Hour, func() {
		for _, tid := range activeTenantIDs(repo) {
			st, err := repo.GetNotifSetting(tid)
			if err != nil || st == nil || !st.WAEnabled {
				continue
			}
			tenantName := "ONOBILL"
			if tenant, terr := repo.GetTenantByID(tid); terr == nil && tenant != nil && tenant.Name != "" {
				tenantName = tenant.Name
			}
			due, err := billingSvc.ListDueSoon(tid, 3)
			if err != nil {
				continue
			}
			for i := range due {
				inv := due[i]
				if inv.Customer == nil || inv.Customer.Phone == "" {
					continue
				}
				payURL := baseURL + "/billing"
				msg := notify.ReminderMessage(&inv, inv.Customer.Name, tenantName, payURL, 3)
				if err := notifySvc.SendWA(st, inv.Customer.Phone, msg); err != nil {
					log.Printf("[worker] reminder WA invoice %s: %v", inv.Number, err)
				}
			}
		}
	})
}
