package http

import (
	"net/http"

	"onobill/internal/service/subscription"
)

// ============ SUPERADMIN ============

// getUserRole membaca role dari header (diisi middleware).
func getUserRole(r *http.Request) string { return r.Header.Get("X-User-Role") }

// RequireSuperadmin middleware — hanya superadmin yang boleh akses.
func (h *Handler) RequireSuperadmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if getUserRole(r) != "superadmin" {
			if r.Header.Get("Accept") == "application/json" {
				writeError(w, http.StatusForbidden, "khusus superadmin")
			} else {
				http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			}
			return
		}
		next(w, r)
	}
}

// SuperadminDashboard menampilkan metrik SaaS: tenant, MRR, platform fee, status langganan.
// Ini halaman khusus SUPERADMIN (CEO) — terpisah dari dashboard tenant.
func (h *Handler) SuperadminDashboard(w http.ResponseWriter, r *http.Request) {
	// Statistik tenant
	tenants, _ := h.Repo.ListTenants()
	totalTenants := len(tenants)
	activeTenants := 0
	for _, t := range tenants {
		if t.Status == "active" {
			activeTenants++
		}
	}

	// Statistik langganan (MRR, grace, suspended)
	var activeSub, graceSub, suspendedSub int
	var mrr float64
	if h.Subscription != nil {
		a, g, s, m, err := h.Subscription.Stats()
		if err == nil {
			activeSub, graceSub, suspendedSub, mrr = a, g, s, m
		}
	}

	// Daftar tenant + status untuk tabel
	render(w, "superadmin", map[string]interface{}{
		"Title":         "Superadmin - ONOBILL",
		"Page":          "superadmin",
		"TotalTenants":  totalTenants,
		"ActiveTenants": activeTenants,
		"ActiveSub":     activeSub,
		"GraceSub":      graceSub,
		"SuspendedSub":  suspendedSub,
		"MRR":           mrr,
		"Tenants":       tenants,
		"Plans":         subscription.Plans,
	})
}

// TenantSuspend menangguhkan tenant (kunci login).
func (h *Handler) TenantSuspend(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	t, err := h.Repo.GetTenantByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "tenant tidak ditemukan")
		return
	}
	t.Status = "suspended"
	if err := h.Repo.UpdateTenant(t); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/superadmin", http.StatusSeeOther)
}

// TenantActivate mengaktifkan kembali tenant.
func (h *Handler) TenantActivate(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	t, err := h.Repo.GetTenantByID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "tenant tidak ditemukan")
		return
	}
	t.Status = "active"
	if err := h.Repo.UpdateTenant(t); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.Redirect(w, r, "/superadmin", http.StatusSeeOther)
}
