package http

import (
	"html/template"
	"net/http"
)

var templates *template.Template

// pageTemplates maps page name -> parsed template set (layout + page)
var pageTemplates = map[string]*template.Template{}

// InitTemplates parses all HTML templates
func InitTemplates(pattern string) error {
	var err error
	templates, err = template.ParseGlob(pattern)
	if err != nil {
		return err
	}

	// Parse each page separately with the layout
	pages := []string{"dashboard", "customer", "billing", "package", "router", "router_script", "vpn", "voucher", "portfwd", "tenant", "superadmin", "chr", "notify", "payment", "suspended", "change_password"}
	for _, page := range pages {
		t, err := template.ParseFiles("web/templates/layout.html", "web/templates/"+page+".html")
		if err != nil {
			return err
		}
		pageTemplates[page] = t
	}
	return nil
}

func render(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Standalone pages (tanpa layout admin): login admin + portal pelanggan.
	if name == "login" || name == "portal_login" || name == "portal_home" {
		if err := templates.ExecuteTemplate(w, name+".html", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Other pages use per-page template set
	t, ok := pageTemplates[name]
	if !ok {
		http.Error(w, "template not found: "+name, http.StatusInternalServerError)
		return
	}
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// SetupRoutes registers all HTTP routes
func SetupRoutes(mux *http.ServeMux, h *Handler) {
	// Static files
	fs := http.FileServer(http.Dir("web/static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// Public pages
	mux.HandleFunc("GET /{$}", h.LoginPage)
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.LoginSubmit)
	mux.HandleFunc("POST /logout", h.LogoutSubmit)
	mux.HandleFunc("GET /logout", h.LogoutSubmit)

	// API - public
	mux.HandleFunc("POST /api/v1/auth/login", h.APILogin)

	// Protected HTML pages
	mux.HandleFunc("GET /dashboard", h.RequireAuth(h.Dashboard, false))
	mux.HandleFunc("GET /customers", h.RequireAuth(h.CustomerList, false))
	mux.HandleFunc("POST /customers", h.RequireAuth(h.CustomerCreate, false))
	mux.HandleFunc("POST /customers/{id}/update", h.RequireAuth(h.CustomerUpdate, false))
	mux.HandleFunc("POST /customers/{id}/delete", h.RequireAuth(h.CustomerDelete, false))
	mux.HandleFunc("POST /customers/{id}/status", h.RequireAuth(h.CustomerSetStatus, false))

	mux.HandleFunc("GET /billing", h.RequireAuth(h.BillingList, false))
	mux.HandleFunc("POST /billing/invoices", h.RequireAuth(h.BillingCreateInvoice, false))
	mux.HandleFunc("POST /billing/invoices/{id}/pay", h.RequireAuth(h.BillingPayInvoice, false))

	// Paket / Profile (PPPoE & Hotspot)
	mux.HandleFunc("GET /packages", h.RequireAuth(h.PackageList, false))
	mux.HandleFunc("POST /packages", h.RequireAuth(h.PackageCreate, false))
	mux.HandleFunc("POST /packages/{id}/update", h.RequireAuth(h.PackageUpdate, false))
	mux.HandleFunc("POST /packages/{id}/delete", h.RequireAuth(h.PackageDelete, false))

	mux.HandleFunc("GET /routers", h.RequireAuth(h.RouterList, false))
	mux.HandleFunc("POST /routers", h.RequireAuth(h.RouterCreate, false))
	mux.HandleFunc("POST /routers/{id}/delete", h.RequireAuth(h.RouterDelete, false))
	mux.HandleFunc("GET /routers/{id}/test", h.RequireAuth(h.RouterTest, false))

	// Superadmin (khusus CEO)
	mux.HandleFunc("GET /superadmin", h.RequireAuth(h.RequireSuperadmin(h.SuperadminDashboard), false))
	mux.HandleFunc("POST /superadmin/tenants/{id}/suspend", h.RequireAuth(h.RequireSuperadmin(h.TenantSuspend), false))
	mux.HandleFunc("POST /superadmin/tenants/{id}/activate", h.RequireAuth(h.RequireSuperadmin(h.TenantActivate), false))

	// CHR — hub komunikasi L2TP (khusus superadmin)
	mux.HandleFunc("GET /superadmin/chr", h.RequireAuth(h.RequireSuperadmin(h.CHRList), false))
	mux.HandleFunc("POST /superadmin/chr", h.RequireAuth(h.RequireSuperadmin(h.CHRCreate), false))
	mux.HandleFunc("POST /superadmin/chr/{id}/delete", h.RequireAuth(h.RequireSuperadmin(h.CHRDelete), false))
	mux.HandleFunc("POST /superadmin/chr/{id}/toggle", h.RequireAuth(h.RequireSuperadmin(h.CHRToggle), false))

	// Notifikasi platform: WA gateway + Email + Broadcast (khusus superadmin)
	mux.HandleFunc("GET /superadmin/notify", h.RequireAuth(h.RequireSuperadmin(h.NotifyPage), false))
	mux.HandleFunc("POST /superadmin/notify/settings", h.RequireAuth(h.RequireSuperadmin(h.NotifySaveSettings), false))
	mux.HandleFunc("POST /superadmin/notify/test-wa", h.RequireAuth(h.RequireSuperadmin(h.NotifyTestWA), false))
	mux.HandleFunc("POST /superadmin/notify/test-email", h.RequireAuth(h.RequireSuperadmin(h.NotifyTestEmail), false))
	mux.HandleFunc("POST /superadmin/notify/broadcast", h.RequireAuth(h.RequireSuperadmin(h.NotifyBroadcast), false))

	// Payment Gateway (superadmin)
	mux.HandleFunc("GET /superadmin/payment", h.RequireAuth(h.RequireSuperadmin(h.PaymentGatewayPage), false))
	mux.HandleFunc("POST /superadmin/payment", h.RequireAuth(h.RequireSuperadmin(h.PaymentGatewaySave), false))

	// Ganti password (semua user)
	mux.HandleFunc("GET /settings/password", h.RequireAuth(h.ChangePasswordPage, false))
	mux.HandleFunc("POST /settings/password", h.RequireAuth(h.ChangePasswordSubmit, false))

	mux.HandleFunc("GET /vpn", h.RequireAuth(h.VPNList, false))
	mux.HandleFunc("POST /vpn/peers", h.RequireAuth(h.VPNCreatePeer, false))
	mux.HandleFunc("POST /vpn/peers/{id}/delete", h.RequireAuth(h.VPNDeletePeer, false))
	mux.HandleFunc("GET /vpn/peers/{id}/script", h.RequireAuth(h.VPNDownloadScript, false))

	// Port Forwarding (tenant): expose service internal lewat port publik unik di CHR
	mux.HandleFunc("GET /portfwd", h.RequireAuth(h.PortfwdPage, false))
	mux.HandleFunc("POST /portfwd", h.RequireAuth(h.PortfwdCreate, false))
	mux.HandleFunc("POST /portfwd/{id}/delete", h.RequireAuth(h.PortfwdDelete, false))

	// Voucher hotspot (tenant)
	mux.HandleFunc("GET /vouchers", h.RequireAuth(h.VoucherList, false))
	mux.HandleFunc("POST /vouchers/generate", h.RequireAuth(h.VoucherGenerate, false))
	mux.HandleFunc("POST /vouchers/{id}/disable", h.RequireAuth(h.VoucherDisable, false))
	mux.HandleFunc("POST /vouchers/{id}/reset", h.RequireAuth(h.VoucherReset, false))
	mux.HandleFunc("POST /vouchers/{id}/delete", h.RequireAuth(h.VoucherDelete, false))
	mux.HandleFunc("GET /api/v1/vouchers", h.RequireAuth(h.APIVoucherList, true))
	mux.HandleFunc("POST /api/v1/vouchers/generate", h.RequireAuth(h.APIVoucherGenerate, true))

	mux.HandleFunc("GET /tenants", h.RequireAuth(h.TenantList, false))
	mux.HandleFunc("POST /tenants", h.RequireAuth(h.TenantCreate, false))

	// API - protected
	mux.HandleFunc("GET /api/v1/customers", h.RequireAuth(h.APICustomerList, true))
	mux.HandleFunc("GET /api/v1/customers/{id}", h.RequireAuth(h.APICustomerGet, true))
	mux.HandleFunc("POST /api/v1/customers/{id}/isolate", h.RequireAuth(h.APICustomerIsolate, true))
	mux.HandleFunc("POST /api/v1/customers/{id}/unisolate", h.RequireAuth(h.APICustomerUnIsolate, true))
	mux.HandleFunc("GET /api/v1/invoices", h.RequireAuth(h.APIInvoiceList, true))
	mux.HandleFunc("POST /api/v1/invoices/{id}/pay", h.RequireAuth(h.APICreatePaymentLink, true))

	// Provisioning router (generate script copas)
	mux.HandleFunc("POST /routers/provision", h.RequireAuth(h.ProvisioningRouter, false))
	mux.HandleFunc("POST /api/v1/routers/provision", h.RequireAuth(h.APIProvisioningRouter, true))

	// Router check-in (router menelepon balik setelah jalankan script -> otomatis tersimpan)
	mux.HandleFunc("GET /api/onboard/{token}", h.RouterCheckIn)
	mux.HandleFunc("POST /api/onboard/{token}", h.RouterCheckIn)

	// Payment webhook (tanpa auth — divalidasi signature gateway)
	mux.HandleFunc("POST /webhooks/payment/{gateway}", h.PaymentWebhook)

	// Portal pelanggan self-service (login mandiri, lihat tagihan, bayar)
	mux.HandleFunc("GET /portal/login", h.PortalLoginPage)
	mux.HandleFunc("POST /portal/login", h.PortalLoginSubmit)
	mux.HandleFunc("GET /portal/logout", h.PortalLogout)
	mux.HandleFunc("GET /portal", h.PortalHome)
}
