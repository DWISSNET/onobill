package http

import (
	"net/http"

	"onobill/internal/service/pkg"
)

// ============ PACKAGE / PROFILE (PPPoE & HOTSPOT) ============

func (h *Handler) PackageList(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	filterType := r.URL.Query().Get("type") // "pppoe" | "hotspot" | ""

	packages, _ := h.Package.List(tenantID)
	if filterType == "pppoe" || filterType == "hotspot" {
		filtered := packages[:0]
		for _, p := range packages {
			if p.Type == filterType {
				filtered = append(filtered, p)
			}
		}
		packages = filtered
	}

	render(w, "package", map[string]interface{}{
		"Title":      "Paket / Profile - ONOBILL",
		"Page":       "package",
		"Packages":   packages,
		"FilterType": filterType,
		"Stats":      h.Package.Stats(tenantID),
		"Flash":      r.URL.Query().Get("msg"),
	})
}

func packageInputFromForm(r *http.Request) pkg.Input {
	return pkg.Input{
		Name:         r.FormValue("name"),
		ServiceType:  r.FormValue("service_type"),
		Description:  r.FormValue("description"),
		Price:        parseFloat(r.FormValue("price")),
		SpeedDown:    int(parseFloat(r.FormValue("speed_down"))),
		SpeedUp:      int(parseFloat(r.FormValue("speed_up"))),
		QuotaGB:      int(parseFloat(r.FormValue("quota_gb"))),
		ValidityDays: int(parseFloat(r.FormValue("validity_days"))),
		ProfileName:  r.FormValue("profile_name"),
		AddressList:  r.FormValue("address_list"),
		ParentQueue:  r.FormValue("parent_queue"),
		PoolName:     r.FormValue("pool_name"),
		IsActive:     r.FormValue("is_active") == "on",
	}
}

func (h *Handler) PackageCreate(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	if _, err := h.Package.Create(tenantID, packageInputFromForm(r)); err != nil {
		http.Redirect(w, r, "/packages?msg="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/packages?msg=Paket berhasil ditambahkan", http.StatusSeeOther)
}

func (h *Handler) PackageUpdate(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, err := parseID(r, "id")
	if err != nil {
		http.Redirect(w, r, "/packages?msg=ID tidak valid", http.StatusSeeOther)
		return
	}
	if _, err := h.Package.Update(tenantID, id, packageInputFromForm(r)); err != nil {
		http.Redirect(w, r, "/packages?msg="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/packages?msg=Paket berhasil diperbarui", http.StatusSeeOther)
}

func (h *Handler) PackageDelete(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "ID tidak valid")
		return
	}
	if err := h.Package.Delete(tenantID, id); err != nil {
		writeError(w, http.StatusNotFound, "Paket tidak ditemukan")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
