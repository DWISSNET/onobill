package http

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"onobill/internal/domain"
	"onobill/internal/repository"
	"onobill/internal/service/auth"
	"onobill/internal/service/billing"
	"onobill/internal/service/chr"
	"onobill/internal/service/customer"
	"onobill/internal/service/ipam"
	"onobill/internal/service/isolation"
	"onobill/internal/service/mikrosync"
	"onobill/internal/service/notify"
	"onobill/internal/service/payment"
	"onobill/internal/service/pkg"
	"onobill/internal/service/portal"
	"onobill/internal/service/provisioning"
	"onobill/internal/service/router"
	"onobill/internal/service/subscription"
	"onobill/internal/service/tenant"
	"onobill/internal/service/voucher"
	"onobill/internal/service/vpn"
)

type Handler struct {
	Auth         *auth.Service
	Customer     *customer.Service
	Billing      *billing.Service
	Router       *router.Service
	Tenant       *tenant.Service
	VPN          *vpn.Service
	Isolation    *isolation.Service
	Payment      *payment.Registry
	Notify       *notify.Service
	Package      *pkg.Service
	Voucher      *voucher.Service
	Repo         *repository.Repository
	IPAM         *ipam.Service
	BaseURL      string
	Subscription *subscription.Service
	MikroSync    *mikrosync.Engine // auto-sync ke MikroTik (PPPoE/hotspot/voucher)
	Portal       *portal.Service   // portal self-service pelanggan

	loginMu      sync.Mutex
	loginAttempt map[string]*loginAttempt
}

// loginAttempt melacak percobaan login gagal per kunci (email|ip).
type loginAttempt struct {
	count   int
	blocked time.Time
}

const (
	loginMaxAttempts = 5                // gagal 5x -> blokir
	loginBlockFor    = 15 * time.Minute // durasi blokir
)

// allowLogin memeriksa apakah kunci masih boleh mencoba login (rate-limit anti brute-force).
func (h *Handler) allowLogin(key string) bool {
	h.loginMu.Lock()
	defer h.loginMu.Unlock()
	if h.loginAttempt == nil {
		h.loginAttempt = map[string]*loginAttempt{}
	}
	a, ok := h.loginAttempt[key]
	if !ok {
		return true
	}
	if time.Now().Before(a.blocked) {
		return false
	}
	if a.count >= loginMaxAttempts {
		// Blokir sudah lewat -> reset.
		delete(h.loginAttempt, key)
		return true
	}
	return true
}

// recordLoginFail menambah hitungan gagal; blokir bila melebihi batas.
func (h *Handler) recordLoginFail(key string) {
	h.loginMu.Lock()
	defer h.loginMu.Unlock()
	if h.loginAttempt == nil {
		h.loginAttempt = map[string]*loginAttempt{}
	}
	a := h.loginAttempt[key]
	if a == nil {
		a = &loginAttempt{}
		h.loginAttempt[key] = a
	}
	a.count++
	if a.count >= loginMaxAttempts {
		a.blocked = time.Now().Add(loginBlockFor)
	}
}

// resetLogin menghapus hitungan gagal setelah login sukses.
func (h *Handler) resetLogin(key string) {
	h.loginMu.Lock()
	defer h.loginMu.Unlock()
	delete(h.loginAttempt, key)
}

// clientIP mengambil IP klien (hormati X-Forwarded-For di belakang proxy).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx > 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func NewHandler(
	a *auth.Service,
	c *customer.Service,
	b *billing.Service,
	r *router.Service,
	t *tenant.Service,
	v *vpn.Service,
	i *isolation.Service,
) *Handler {
	return &Handler{Auth: a, Customer: c, Billing: b, Router: r, Tenant: t, VPN: v, Isolation: i}
}

// WithPayment melampirkan payment gateway registry, notifier, dan repo ke handler.
func (h *Handler) WithPayment(p *payment.Registry, n *notify.Service, repo *repository.Repository) *Handler {
	h.Payment = p
	h.Notify = n
	h.Repo = repo
	return h
}

// WithIPAM melampirkan service IPAM (untuk provisioning router).
func (h *Handler) WithIPAM(i *ipam.Service) *Handler {
	h.IPAM = i
	return h
}

// WithBaseURL melampirkan base URL publik (untuk check-in router & callback).
func (h *Handler) WithBaseURL(u string) *Handler {
	h.BaseURL = u
	return h
}

// WithSubscription melampirkan service langganan tenant (untuk dashboard superadmin).
func (h *Handler) WithSubscription(s *subscription.Service) *Handler {
	h.Subscription = s
	return h
}

// WithVoucher melampirkan service voucher hotspot ke handler.
func (h *Handler) WithVoucher(s *voucher.Service) *Handler {
	h.Voucher = s
	return h
}

// WithPackage melampirkan service paket/profile (PPPoE & hotspot) ke handler.
func (h *Handler) WithPackage(s *pkg.Service) *Handler {
	h.Package = s
	return h
}

// WithMikroSync melampirkan engine auto-sync MikroTik ke handler.
// Setelah dipasang, aksi create/isolir pelanggan otomatis push ke router.
func (h *Handler) WithMikroSync(e *mikrosync.Engine) *Handler {
	h.MikroSync = e
	return h
}

// ============ HELPERS ============

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func parseID(r *http.Request, key string) (uint, error) {
	idStr := r.PathValue(key)
	id, err := strconv.ParseUint(idStr, 10, 32)
	return uint(id), err
}

func parseUint(s string) *uint {
	if s == "" {
		return nil
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return nil
	}
	v := uint(n)
	return &v
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// RequireAuth middleware - returns 401 JSON for API, redirect for HTML
func (h *Handler) RequireAuth(next http.HandlerFunc, isAPI bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, err := h.Auth.GetClaims(r)
		if err != nil {
			if isAPI {
				writeError(w, http.StatusUnauthorized, "unauthorized")
			} else {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
			}
			return
		}
		// Store claims in context via header (simple approach)
		r.Header.Set("X-User-ID", strconv.Itoa(int(claims.UserID)))
		r.Header.Set("X-Tenant-ID", strconv.Itoa(int(claims.TenantID)))
		r.Header.Set("X-User-Email", claims.Email)
		r.Header.Set("X-User-Role", claims.Role)
		// Blokir tenant suspended (kecuali superadmin) — alur bisnis: menunggak -> login dikunci
		if claims.Role != "superadmin" && h.Repo != nil {
			if t, err := h.Repo.GetTenantByID(uint(claims.TenantID)); err == nil && t.Status == "suspended" {
				if isAPI {
					writeError(w, http.StatusPaymentRequired, "langganan tenant ditangguhkan — lunasi tagihan untuk membuka kembali")
				} else {
					render(w, "suspended", map[string]interface{}{"Tenant": t})
				}
				return
			}
		}
		next(w, r)
	}
}

func getTenantID(r *http.Request) uint {
	id, _ := strconv.Atoi(r.Header.Get("X-Tenant-ID"))
	return uint(id)
}

func getUserID(r *http.Request) uint {
	id, _ := strconv.Atoi(r.Header.Get("X-User-ID"))
	return uint(id)
}

// ============ AUTH HANDLERS ============

func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	// If already logged in, redirect to dashboard
	if _, err := h.Auth.GetClaims(r); err == nil {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	render(w, "login", map[string]interface{}{"Title": "Login - ONOBILL"})
}

func (h *Handler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	email := r.FormValue("email")
	password := r.FormValue("password")
	key := email + "|" + clientIP(r)

	if !h.allowLogin(key) {
		render(w, "login", map[string]interface{}{
			"Title": "Login - ONOBILL",
			"Error": "Terlalu banyak percobaan gagal. Coba lagi dalam 15 menit.",
		})
		return
	}

	token, user, err := h.Auth.Login(email, password)
	if err != nil {
		h.recordLoginFail(key)
		render(w, "login", map[string]interface{}{
			"Title": "Login - ONOBILL",
			"Error": "Email atau password salah",
		})
		return
	}

	h.resetLogin(key)
	h.Auth.SetAuthCookie(w, token)
	_ = user
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (h *Handler) LogoutSubmit(w http.ResponseWriter, r *http.Request) {
	h.Auth.ClearAuthCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// API login returns JWT
func (h *Handler) APILogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	token, user, err := h.Auth.Login(req.Email, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":        user.ID,
			"email":     user.Email,
			"name":      user.Name,
			"role":      user.Role,
			"tenant_id": user.TenantID,
		},
	})
}

// ============ DASHBOARD ============

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	custStats := h.Customer.Count(tenantID)
	billStats := h.Billing.Stats(tenantID)
	routers, _ := h.Router.List(tenantID)
	recentPayments, _ := h.Billing.ListPayments(tenantID, 5)
	recentInvoices, _ := h.Billing.ListInvoices(tenantID, "unpaid")
	if len(recentInvoices) > 5 {
		recentInvoices = recentInvoices[:5]
	}

	render(w, "dashboard", map[string]interface{}{
		"Title":          "Dashboard - ONOBILL",
		"Page":           "dashboard",
		"CustStats":      custStats,
		"BillStats":      billStats,
		"RouterCount":    len(routers),
		"RecentPayments": recentPayments,
		"RecentInvoices": recentInvoices,
	})
}

// ============ CUSTOMER HANDLERS ============

func (h *Handler) CustomerList(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	status := r.URL.Query().Get("status")
	keyword := r.URL.Query().Get("q")

	customers, _ := h.Customer.List(tenantID, status, keyword)
	routers, _ := h.Router.List(tenantID)
	packages, _ := h.listPackages(tenantID)

	render(w, "customer", map[string]interface{}{
		"Title":     "Pelanggan - ONOBILL",
		"Page":      "customer",
		"Customers": customers,
		"Routers":   routers,
		"Packages":  packages,
		"Status":    status,
		"Keyword":   keyword,
	})
}

func (h *Handler) CustomerCreate(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	r.ParseForm()

	in := customer.CreateCustomerInput{
		Name:        r.FormValue("name"),
		Email:       r.FormValue("email"),
		Phone:       r.FormValue("phone"),
		Address:     r.FormValue("address"),
		Username:    r.FormValue("username"),
		Password:    r.FormValue("password"),
		ServiceType: r.FormValue("service_type"),
		RouterID:    parseUint(r.FormValue("router_id")),
		PackageID:   parseUint(r.FormValue("package_id")),
	}

	c, err := h.Customer.Create(tenantID, in)
	if err != nil {
		http.Redirect(w, r, "/customers?error="+err.Error(), http.StatusSeeOther)
		return
	}
	// Auto-sync ke MikroTik: push PPPoE secret / hotspot user pelanggan baru.
	h.enqueueCustomerSync(tenantID, c)
	http.Redirect(w, r, "/customers?success=created", http.StatusSeeOther)
}

// enqueueCustomerSync mengirim task sync pelanggan ke engine (aman bila engine belum dipasang).
func (h *Handler) enqueueCustomerSync(tenantID uint, c *domain.Customer) {
	if h.MikroSync == nil || c == nil || c.RouterID == nil {
		return
	}
	var ttype mikrosync.TaskType
	switch c.ServiceType {
	case "hotspot":
		ttype = mikrosync.TaskHotspotUser
	default: // pppoe
		ttype = mikrosync.TaskPPPoESecret
	}
	h.MikroSync.Enqueue(mikrosync.Task{
		TenantID: tenantID,
		RouterID: *c.RouterID,
		Type:     ttype,
		RefID:    c.ID,
	})
}

func (h *Handler) CustomerUpdate(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	r.ParseForm()

	in := customer.CreateCustomerInput{
		Name:        r.FormValue("name"),
		Email:       r.FormValue("email"),
		Phone:       r.FormValue("phone"),
		Address:     r.FormValue("address"),
		Username:    r.FormValue("username"),
		Password:    r.FormValue("password"),
		ServiceType: r.FormValue("service_type"),
		RouterID:    parseUint(r.FormValue("router_id")),
		PackageID:   parseUint(r.FormValue("package_id")),
	}

	_, err = h.Customer.Update(tenantID, id, in)
	if err != nil {
		http.Redirect(w, r, "/customers?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/customers?success=updated", http.StatusSeeOther)
}

func (h *Handler) CustomerDelete(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, _ := parseID(r, "id")
	h.Customer.Delete(tenantID, id)
	http.Redirect(w, r, "/customers?success=deleted", http.StatusSeeOther)
}

func (h *Handler) CustomerSetStatus(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, _ := parseID(r, "id")
	r.ParseForm()
	status := r.FormValue("status")

	// Route isolation status changes through isolation service so the
	// router's PPPoE secret is disabled/enabled in sync.
	var err error
	switch status {
	case "isolated":
		err = h.Isolation.Isolate(tenantID, id)
	case "active":
		err = h.Isolation.UnIsolate(tenantID, id)
	default:
		_, err = h.Customer.SetStatus(tenantID, id, status)
	}
	if err != nil {
		http.Redirect(w, r, "/customers?error="+err.Error(), http.StatusSeeOther)
		return
	}
	// Auto-sync: disable/enable PPPoE secret / hotspot user di MikroTik sesuai status baru.
	if h.MikroSync != nil {
		if c, err := h.Customer.Get(tenantID, id); err == nil && c.RouterID != nil {
			ttype := mikrosync.TaskDisableCustomer
			if status == "active" {
				// aktif kembali -> push ulang secret/user
				if c.ServiceType == "hotspot" {
					ttype = mikrosync.TaskHotspotUser
				} else {
					ttype = mikrosync.TaskPPPoESecret
				}
			}
			h.MikroSync.Enqueue(mikrosync.Task{TenantID: tenantID, RouterID: *c.RouterID, Type: ttype, RefID: c.ID})
		}
	}
	http.Redirect(w, r, "/customers?success=status_changed", http.StatusSeeOther)
}

// API: customer list JSON
func (h *Handler) APICustomerList(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	status := r.URL.Query().Get("status")
	keyword := r.URL.Query().Get("q")
	customers, err := h.Customer.List(tenantID, status, keyword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, customers)
}

func (h *Handler) APICustomerGet(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, _ := parseID(r, "id")
	c, err := h.Customer.Get(tenantID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "customer not found")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// APICustomerIsolate disables customer's PPPoE on router and sets status isolated.
func (h *Handler) APICustomerIsolate(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, _ := parseID(r, "id")
	if err := h.Isolation.Isolate(tenantID, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "isolated"})
}

// APICustomerUnIsolate re-enables customer's PPPoE on router and sets status active.
func (h *Handler) APICustomerUnIsolate(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, _ := parseID(r, "id")
	if err := h.Isolation.UnIsolate(tenantID, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
}

// ============ BILLING HANDLERS ============

func (h *Handler) BillingList(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	status := r.URL.Query().Get("status")
	invoices, _ := h.Billing.ListInvoices(tenantID, status)
	customers, _ := h.Customer.List(tenantID, "", "")
	stats := h.Billing.Stats(tenantID)

	render(w, "billing", map[string]interface{}{
		"Title":     "Billing - ONOBILL",
		"Page":      "billing",
		"Invoices":  invoices,
		"Customers": customers,
		"Stats":     stats,
		"Status":    status,
	})
}

func (h *Handler) BillingCreateInvoice(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	r.ParseForm()
	customerID, _ := strconv.ParseUint(r.FormValue("customer_id"), 10, 32)

	var err error
	if r.FormValue("from_package") == "1" {
		_, err = h.Billing.CreateInvoiceFromPackage(tenantID, uint(customerID))
	} else {
		amount := parseFloat(r.FormValue("amount"))
		dueDays, _ := strconv.Atoi(r.FormValue("due_days"))
		notes := r.FormValue("notes")
		_, err = h.Billing.CreateInvoice(tenantID, uint(customerID), amount, dueDays, notes)
	}

	if err != nil {
		http.Redirect(w, r, "/billing?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/billing?success=created", http.StatusSeeOther)
}

func (h *Handler) BillingPayInvoice(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, _ := parseID(r, "id")
	r.ParseForm()

	amount := parseFloat(r.FormValue("amount"))
	method := r.FormValue("method")
	reference := r.FormValue("reference")

	_, err := h.Billing.PayInvoice(tenantID, id, amount, method, reference)
	if err != nil {
		http.Redirect(w, r, "/billing?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/billing?success=paid", http.StatusSeeOther)
}

func (h *Handler) APIInvoiceList(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	status := r.URL.Query().Get("status")
	invoices, err := h.Billing.ListInvoices(tenantID, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, invoices)
}

// ============ ROUTER HANDLERS ============

func (h *Handler) RouterList(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	routers, _ := h.Router.List(tenantID)
	render(w, "router", map[string]interface{}{
		"Title":   "Router - ONOBILL",
		"Page":    "router",
		"Routers": routers,
	})
}

// sanitizeSecretName membersihkan nama router menjadi nama PPP secret yang aman
// untuk MikroTik: huruf kecil, hanya alfanumerik + '-' + '_', spasi jadi '-'.
func sanitizeSecretName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ', r == '/', r == '\\', r == '.':
			b.WriteRune('-')
		}
	}
	out := b.String()
	if out == "" {
		out = "router"
	}
	return out
}

// RouterCreate — alur sederhana: admin cuma isi nama router, pilih jenis koneksi,
// versi MikroTik (v6/v7), tipe layanan (app/hotspot/pppoe), dan expired billing.
// Sistem otomatis: generate kredensial API + alokasi IP + script copas.
func (h *Handler) RouterCreate(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	r.ParseForm()

	if h.IPAM == nil {
		http.Redirect(w, r, "/routers?error=IPAM+tidak+tersedia", http.StatusSeeOther)
		return
	}
	in := provisioning.RouterInput{
		TenantID:       tenantID,
		Name:           r.FormValue("name"),
		ConnectionType: r.FormValue("connection_type"), // direct | l2tp
		ROSVersion:     r.FormValue("ros_version"),     // v6 | v7
		ServiceType:    r.FormValue("service_type"),    // app | hotspot | pppoe
		ExpiredMode:    r.FormValue("expired_mode"),    // app | mikrotik
		DirectHost:     strings.TrimSpace(r.FormValue("direct_host")), // IP/host router (direct)
	}
	plan, err := provisioning.BuildPlan(in, h.IPAM, 0)
	if err != nil {
		http.Redirect(w, r, "/routers?error="+err.Error(), http.StatusSeeOther)
		return
	}

	// Jika koneksi L2TP: auto-buat PPP secret di CHR agar router bisa langsung dial.
	// Secret name = user L2TP yang sama dipakai di script router.
	if in.ConnectionType == "l2tp" && h.Repo != nil {
		if c, err := h.Repo.ActiveCHR(); err == nil && c != nil {
			useTLS := c.APIPort == 8729
			secretName := "onobill-" + sanitizeSecretName(in.Name)
			comment := "ONOBILL router " + in.Name
			if err := chr.EnsurePPPSecret(c.Host, c.APIPort, c.Username, c.Password, useTLS,
				secretName, plan.APIPassword, comment); err != nil {
				log.Printf("[provisioning] gagal buat PPP secret di CHR %s: %v", c.Host, err)
			} else {
				log.Printf("[provisioning] PPP secret %s dibuat di CHR %s", secretName, c.Host)
			}
			// Script router memakai user L2TP yang sama dengan secret di CHR.
			plan.Script = strings.Replace(plan.Script, "user=onobill ", "user="+secretName+" ", 1)
		}
	}

	// Sisipkan check-in agar router otomatis tersimpan setelah paste script
	token := h.Repo.EnsureOnboardToken(tenantID)
	checkinURL := h.BaseURL + "/api/onboard/" + token +
		"?name=" + url.QueryEscape(in.Name) + "&user=" + url.QueryEscape(plan.APIUsername) +
		"&pass=" + url.QueryEscape(plan.APIPassword) + "&mode=" + url.QueryEscape(in.ServiceType)
	// Untuk koneksi direct: kirim IP/host router agar ONOBILL tahu alamat untuk dial balik.
	if in.ConnectionType == "direct" && in.DirectHost != "" {
		checkinURL += "&host=" + url.QueryEscape(in.DirectHost)
	}
	plan.Script += "\n\n# --- Daftar ke ONOBILL (otomatis tersimpan) ---\n" +
		"/tool fetch url=\"" + checkinURL + "\" keep-result=no\n"

	// Tampilkan halaman berisi script siap copas
	render(w, "router_script", map[string]interface{}{
		"Title": "Script Router - ONOBILL",
		"Page":  "router",
		"Plan":  plan,
	})
}

func (h *Handler) RouterDelete(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, _ := parseID(r, "id")
	h.Router.Delete(tenantID, id)
	http.Redirect(w, r, "/routers?success=deleted", http.StatusSeeOther)
}

func (h *Handler) RouterTest(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, _ := parseID(r, "id")
	rt, err := h.Router.Get(tenantID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "router not found")
		return
	}
	ok := h.Router.TestConnection(rt.Host, rt.Port)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"reachable": ok,
		"host":      rt.Host,
		"port":      rt.Port,
	})
}

// ============ VPN HANDLERS ============

func (h *Handler) VPNList(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	peers, _ := h.VPN.List(tenantID)
	routers, _ := h.Router.List(tenantID)
	render(w, "vpn", map[string]interface{}{
		"Title":   "VPN - ONOBILL",
		"Page":    "vpn",
		"Peers":   peers,
		"Routers": routers,
	})
}

func (h *Handler) VPNCreatePeer(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	r.ParseForm()
	routerID := parseUint(r.FormValue("router_id"))
	name := r.FormValue("name")
	endpoint := r.FormValue("endpoint")

	_, err := h.VPN.CreatePeer(tenantID, routerID, name, endpoint)
	if err != nil {
		http.Redirect(w, r, "/vpn?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/vpn?success=created", http.StatusSeeOther)
}

func (h *Handler) VPNDeletePeer(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, _ := parseID(r, "id")
	h.VPN.Delete(tenantID, id)
	http.Redirect(w, r, "/vpn?success=deleted", http.StatusSeeOther)
}

func (h *Handler) VPNDownloadScript(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, _ := parseID(r, "id")
	peer, err := h.VPN.Get(tenantID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "peer not found")
		return
	}
	script := h.VPN.GenerateMikrotikScript(peer, "SERVER_PUBLIC_KEY_HERE", "vpn.onobill.id")
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", "attachment; filename=wireguard-"+strings.ReplaceAll(peer.Name, " ", "-")+".rsc")
	w.Write([]byte(script))
}

// ============ TENANT HANDLERS (superadmin only) ============

func (h *Handler) TenantList(w http.ResponseWriter, r *http.Request) {
	tenants, _ := h.Tenant.List()
	render(w, "tenant", map[string]interface{}{
		"Title":   "Tenant - ONOBILL",
		"Page":    "tenant",
		"Tenants": tenants,
	})
}

func (h *Handler) TenantCreate(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	name := r.FormValue("name")
	adminEmail := r.FormValue("admin_email")
	adminPassword := r.FormValue("admin_password")
	adminName := r.FormValue("admin_name")

	t, err := h.Tenant.Create(name)
	if err != nil {
		http.Redirect(w, r, "/tenants?error="+err.Error(), http.StatusSeeOther)
		return
	}

	// Create admin user for tenant
	if adminEmail != "" && adminPassword != "" {
		h.Auth.Register(t.ID, adminEmail, adminPassword, adminName, "admin")
	}

	http.Redirect(w, r, "/tenants?success=created", http.StatusSeeOther)
}

// Helper to list packages
func (h *Handler) listPackages(tenantID uint) ([]interface{}, error) {
	// Stub - will wire to package service later
	return nil, nil
}
