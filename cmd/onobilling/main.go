package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"onobill/internal/config"
	"onobill/internal/repository"
	"onobill/internal/service/accounting"
	"onobill/internal/service/auth"
	"onobill/internal/service/backup"
	"onobill/internal/service/billing"
	"onobill/internal/service/customer"
	"onobill/internal/service/ipam"
	"onobill/internal/service/isolation"
	"onobill/internal/service/mikrosync"
	"onobill/internal/service/notify"
	"onobill/internal/service/payment"
	"onobill/internal/service/pkg"
	"onobill/internal/service/portal"
	"onobill/internal/service/portfwd"
	"onobill/internal/service/reseller"
	"onobill/internal/service/router"
	"onobill/internal/service/subscription"
	"onobill/internal/service/tenant"
	"onobill/internal/service/voucher"
	"onobill/internal/service/vpn"
	"onobill/internal/service/wallet"
	httptransport "onobill/internal/transport/http"
	"onobill/internal/worker"
)

func main() {
	// Load config
	cfg := config.Load()

	// Connect DB
	db, err := config.ConnectDB(cfg)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}

	// Seed default admin
	if err := config.Seed(db); err != nil {
		log.Printf("seed warning: %v", err)
	}

	// Init repository
	repo := repository.New(db)

	// Init services
	authSvc := auth.NewService(repo, cfg.JWTSecret)
	customerSvc := customer.NewService(repo)
	billingSvc := billing.NewService(repo)
	routerSvc := router.NewService(repo)
	tenantSvc := tenant.NewService(repo)
	vpnSvc := vpn.NewService(repo)
	isolationSvc := isolation.NewService(repo)
	voucherSvc := voucher.NewService(repo)
	pkgSvc := pkg.NewService(repo)
	notifySvc := notify.NewService()

	// IPAM (auto-alokasi IP anti-bentrok) + Port-forward Winbox
	ipamSvc := ipam.NewService(db)
	if err := ipamSvc.AutoMigrate(); err != nil {
		log.Printf("[ipam] migrate warning: %v", err)
	}
	portfwdSvc := portfwd.NewService(db, 30000, 40000)
	if err := portfwdSvc.AutoMigrate(); err != nil {
		log.Printf("[portfwd] migrate warning: %v", err)
	}

	// Reseller (saldo + komisi) + Accounting (ledger tenant & superadmin)
	resellerSvc := reseller.NewService(db)
	if err := resellerSvc.AutoMigrate(); err != nil {
		log.Printf("[reseller] migrate warning: %v", err)
	}
	acctSvc := accounting.NewService(db)
	if err := acctSvc.AutoMigrate(); err != nil {
		log.Printf("[accounting] migrate warning: %v", err)
	}
	_ = resellerSvc

	// Wire auto-unisolate: when invoice is paid, re-enable PPPoE on router.
	billingSvc.OnCustomerReactivate = func(tenantID, customerID uint) {
		if err := isolationSvc.UnIsolate(tenantID, customerID); err != nil {
			log.Printf("[billing] auto-unisolate customer %d failed: %v", customerID, err)
		}
	}
	// Wire accounting: setiap pembayaran tercatat ke ledger (tenant + platform fee superadmin).
	billingSvc.OnPaymentRecorded = func(tenantID uint, amount, platformFee float64, ref string) {
		if err := acctSvc.RecordPayment(tenantID, amount, platformFee, ref); err != nil {
			log.Printf("[accounting] record payment %s: %v", ref, err)
		}
	}

	// Auto-backup DB terjadwal (SQLite VACUUM INTO / MySQL mysqldump) + retensi.
	backupSvc := backup.NewService(db, backup.Config{
		Driver: cfg.DBDriver,
		Path:   cfg.DBPath,
		Host:   cfg.DBHost,
		Port:   cfg.DBPort,
		Name:   cfg.DBName,
		User:   cfg.DBUser,
		Pass:   cfg.DBPassword,
	}, "backups", 7)

	// Start background workers (recurring invoice, overdue marker, auto-isolator,
	// router health, reminder H-3, auto-backup). ctx worker dibatalkan saat shutdown.
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()
	worker.StartAll(workerCtx, billingSvc, routerSvc, isolationSvc, repo, notifySvc, cfg.BaseURL, backupSvc)

	// Init payment gateway registry (Duitku + KlikQRIS)
	payReg := payment.NewRegistry()
	callbackURL := cfg.BaseURL + "/webhooks/payment"
	if cfg.DuitkuMerchant != "" && cfg.DuitkuAPIKey != "" {
		payReg.Register(payment.NewDuitku(cfg.DuitkuMerchant, cfg.DuitkuAPIKey, cfg.DuitkuBaseURL,
			callbackURL+"/duitku", cfg.BaseURL))
		log.Printf("[payment] Duitku gateway aktif")
	}
	if cfg.KlikQRISMerchant != "" && cfg.KlikQRISAPIKey != "" {
		payReg.Register(payment.NewKlikQRIS(cfg.KlikQRISMerchant, cfg.KlikQRISAPIKey, cfg.KlikQRISBaseURL,
			callbackURL+"/klikqris", cfg.BaseURL))
		log.Printf("[payment] KlikQRIS gateway aktif")
	}

	// Subscription (langganan tenant -> sumber uang superadmin)
	subSvc := subscription.NewService(db)
	if err := subSvc.AutoMigrate(); err != nil {
		log.Printf("[subscription] migrate warning: %v", err)
	}

	// Portal pelanggan self-service (login mandiri, lihat tagihan, bayar)
	portalSvc := portal.NewService(db)
	if err := portalSvc.AutoMigrate(); err != nil {
		log.Printf("[portal] migrate warning: %v", err)
	}

	// Platform wallet untuk top-up QRIS dan debit layanan.
	walletSvc := wallet.NewService(db)

	// Init HTTP handler
	handler := httptransport.NewHandler(authSvc, customerSvc, billingSvc, routerSvc, tenantSvc, vpnSvc, isolationSvc).
		WithPayment(payReg, notifySvc, repo).
		WithIPAM(ipamSvc).
		WithBaseURL(cfg.BaseURL).
		WithSubscription(subSvc).
		WithVoucher(voucherSvc).
		WithPackage(pkgSvc).
		WithPortal(portalSvc).
		WithWallet(walletSvc)

	// Muat konfigurasi payment gateway yang tersimpan di DB (dari UI Superadmin)
	// — menimpa / melengkapi yang dari env var, berlaku tanpa restart.
	handler.LoadPaymentGatewayFromDB()

	// Auto-sync engine: push data billing (PPPoE profile/secret, hotspot user, voucher)
	// ke router MikroTik tenant secara background. Berjalan periodik + saat ada aksi
	// (tambah pelanggan, isolir, generate voucher) lewat hook di handler.
	syncEngine := mikrosync.NewEngine(repo)
	syncCtx, syncCancel := context.WithCancel(context.Background())
	defer syncCancel()
	syncEngine.Start(syncCtx)
	handler.WithMikroSync(syncEngine)
	log.Printf("[mikrosync] engine auto-sync aktif")

	// Load templates
	if err := httptransport.InitTemplates("web/templates/*.html"); err != nil {
		log.Fatalf("template load failed: %v", err)
	}

	// Setup routes
	mux := http.NewServeMux()
	httptransport.SetupRoutes(mux, handler)

	// Start server dengan graceful shutdown: stop worker + mikrosync bersih saat SIGINT/SIGTERM.
	addr := ":" + cfg.AppPort
	server := &http.Server{Addr: addr, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		fmt.Printf("╔════════════════════════════════════════╗\n")
		fmt.Printf("║   ONOBILL Server                       ║\n")
		fmt.Printf("║   Listening: http://localhost%s      ║\n", addr)
		fmt.Printf("║   Login: admin@onobill.local           ║\n")
		fmt.Printf("║   Pass:  admin123                      ║\n")
		fmt.Printf("╚════════════════════════════════════════╝\n")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("[server] shutdown diterima, membersihkan...")
	stop()
	syncCancel()
	workerCancel()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("[server] graceful shutdown error: %v", err)
	}
	log.Println("[server] berhenti bersih.")
}
