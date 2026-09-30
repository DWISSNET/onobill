package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"onobill/internal/service/mikrosync"
	"onobill/internal/service/voucher"
)

// ============ VOUCHER HANDLERS ============

// VoucherList menampilkan halaman manajemen voucher hotspot tenant.
func (h *Handler) VoucherList(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	status := r.URL.Query().Get("status")

	vouchers, _ := h.Voucher.List(tenantID, status)
	stats, _ := h.Voucher.Stats(tenantID)
	routers, _ := h.Router.List(tenantID)
	packages, _ := h.listPackages(tenantID)

	render(w, "voucher", map[string]interface{}{
		"Title":    "Voucher Hotspot - ONOBILL",
		"Page":     "voucher",
		"Vouchers": vouchers,
		"Stats":    stats,
		"Routers":  routers,
		"Packages": packages,
		"Status":   status,
	})
}

// VoucherGenerate menerima form generate batch voucher.
func (h *Handler) VoucherGenerate(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	r.ParseForm()

	count, _ := strconv.Atoi(r.FormValue("count"))
	codeLength, _ := strconv.Atoi(r.FormValue("code_length"))
	validityDays, _ := strconv.Atoi(r.FormValue("validity_days"))

	in := voucher.GenerateInput{
		Count:        count,
		Prefix:       r.FormValue("prefix"),
		CodeLength:   codeLength,
		Profile:      r.FormValue("profile"),
		RouterID:     parseUint(r.FormValue("router_id")),
		PackageID:    parseUint(r.FormValue("package_id")),
		ValidityDays: validityDays,
	}

	vouchers, err := h.Voucher.GenerateBatch(tenantID, in)
	if err != nil {
		http.Redirect(w, r, "/vouchers?error="+err.Error(), http.StatusSeeOther)
		return
	}
	// Auto-sync: push tiap voucher sebagai hotspot user ke router MikroTik.
	if h.MikroSync != nil && in.RouterID != nil {
		for _, v := range vouchers {
			h.MikroSync.Enqueue(mikrosync.Task{
				TenantID: tenantID,
				RouterID: *in.RouterID,
				Type:     mikrosync.TaskVoucher,
				RefID:    v.ID,
			})
		}
	}
	http.Redirect(w, r, "/vouchers?success=generated", http.StatusSeeOther)
}

// VoucherDisable menonaktifkan voucher yang belum dipakai.
func (h *Handler) VoucherDisable(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.Voucher.Disable(tenantID, id); err != nil {
		http.Redirect(w, r, "/vouchers?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/vouchers?success=disabled", http.StatusSeeOther)
}

// VoucherReset mengembalikan voucher ke status unused.
func (h *Handler) VoucherReset(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.Voucher.Reset(tenantID, id); err != nil {
		http.Redirect(w, r, "/vouchers?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/vouchers?success=reset", http.StatusSeeOther)
}

// VoucherDelete menghapus voucher yang belum dipakai.
func (h *Handler) VoucherDelete(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.Voucher.Delete(tenantID, id); err != nil {
		http.Redirect(w, r, "/vouchers?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/vouchers?success=deleted", http.StatusSeeOther)
}

// ============ VOUCHER API ============

func (h *Handler) APIVoucherList(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	status := r.URL.Query().Get("status")
	vouchers, err := h.Voucher.List(tenantID, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, vouchers)
}

func (h *Handler) APIVoucherGenerate(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	var req struct {
		Count        int    `json:"count"`
		Prefix       string `json:"prefix"`
		CodeLength   int    `json:"code_length"`
		Profile      string `json:"profile"`
		RouterID     *uint  `json:"router_id"`
		PackageID    *uint  `json:"package_id"`
		ValidityDays int    `json:"validity_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	vouchers, err := h.Voucher.GenerateBatch(tenantID, voucher.GenerateInput{
		Count:        req.Count,
		Prefix:       req.Prefix,
		CodeLength:   req.CodeLength,
		Profile:      req.Profile,
		RouterID:     req.RouterID,
		PackageID:    req.PackageID,
		ValidityDays: req.ValidityDays,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, vouchers)
}
