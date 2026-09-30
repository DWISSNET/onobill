package http

import (
	"net/http"
	"time"

	"onobill/internal/service/portal"
)

// WithPortal melampirkan service portal pelanggan (self-service).
func (h *Handler) WithPortal(s *portal.Service) *Handler {
	h.Portal = s
	return h
}

const portalCookie = "onobill_portal"

// currentPortalSession membaca sesi pelanggan dari cookie; kembalikan customerID & tenantID.
func (h *Handler) currentPortalSession(r *http.Request) (customerID, tenantID uint, err error) {
	if h.Portal == nil {
		return 0, 0, portal.ErrSessionExpired
	}
	c, err := r.Cookie(portalCookie)
	if err != nil || c.Value == "" {
		return 0, 0, portal.ErrSessionExpired
	}
	return h.Portal.ValidateSession(c.Value)
}

// PortalLoginPage menampilkan form login pelanggan.
func (h *Handler) PortalLoginPage(w http.ResponseWriter, r *http.Request) {
	if _, _, err := h.currentPortalSession(r); err == nil {
		http.Redirect(w, r, "/portal", http.StatusSeeOther)
		return
	}
	render(w, "portal_login", map[string]interface{}{"Title": "Portal Pelanggan - ONOBILL"})
}

// PortalLoginSubmit memproses login pelanggan (dengan rate-limit anti brute-force).
func (h *Handler) PortalLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if h.Portal == nil {
		http.Error(w, "portal tidak tersedia", http.StatusServiceUnavailable)
		return
	}
	r.ParseForm()
	username := r.FormValue("username")
	password := r.FormValue("password")
	key := "portal|" + username + "|" + clientIP(r)

	if !h.allowLogin(key) {
		render(w, "portal_login", map[string]interface{}{
			"Title": "Portal Pelanggan - ONOBILL",
			"Error": "Terlalu banyak percobaan gagal. Coba lagi dalam 15 menit.",
		})
		return
	}
	sess, err := h.Portal.Login(username, password)
	if err != nil {
		h.recordLoginFail(key)
		render(w, "portal_login", map[string]interface{}{
			"Title": "Portal Pelanggan - ONOBILL",
			"Error": "Username atau password salah",
		})
		return
	}
	h.resetLogin(key)
	http.SetCookie(w, &http.Cookie{
		Name:     portalCookie,
		Value:    sess.Token,
		Path:     "/portal",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
	})
	http.Redirect(w, r, "/portal", http.StatusSeeOther)
}

// PortalLogout menghapus sesi pelanggan.
func (h *Handler) PortalLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(portalCookie); err == nil && h.Portal != nil {
		_ = h.Portal.Logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:    portalCookie,
		Value:   "",
		Path:    "/portal",
		MaxAge:  -1,
		Expires: time.Unix(0, 0),
	})
	http.Redirect(w, r, "/portal/login", http.StatusSeeOther)
}

// PortalHome menampilkan tagihan pelanggan yang sedang login.
func (h *Handler) PortalHome(w http.ResponseWriter, r *http.Request) {
	customerID, tenantID, err := h.currentPortalSession(r)
	if err != nil {
		http.Redirect(w, r, "/portal/login", http.StatusSeeOther)
		return
	}
	customer, err := h.Repo.GetCustomerByID(tenantID, customerID)
	if err != nil {
		http.Error(w, "pelanggan tidak ditemukan", http.StatusNotFound)
		return
	}
	invoices, _ := h.Repo.ListInvoicesByCustomer(tenantID, customerID)
	render(w, "portal_home", map[string]interface{}{
		"Title":    "Tagihan Saya - ONOBILL",
		"Customer": customer,
		"Invoices": invoices,
		"BaseURL":  h.BaseURL,
	})
}
