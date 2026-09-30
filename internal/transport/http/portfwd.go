package http

import (
	"net/http"
	"strconv"

	"onobill/internal/service/portfwd"
)

// ============ PORT FORWARDING (TENANT) ============
// Expose service di jaringan pelanggan/router (CCTV, NVR, Winbox, server lokal)
// lewat port publik unik di CHR. Port dialokasikan otomatis & anti-bentrok.

// PortfwdPage menampilkan daftar port-forward + form tambah.
func (h *Handler) PortfwdPage(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	routers, _ := h.Repo.ListRouters(tenantID)

	svc := portfwd.NewService(h.Repo.DB(), 30000, 40000)
	// Kumpulkan semua forward milik tenant (gabung semua router)
	type Row struct {
		portfwd.Forward
		RouterName string
	}
	var rows []Row
	for _, rt := range routers {
		fwds, _ := svc.ListByRouter(rt.ID)
		for _, f := range fwds {
			if f.TenantID == tenantID {
				rows = append(rows, Row{Forward: f, RouterName: rt.Name})
			}
		}
	}

	render(w, "portfwd", map[string]interface{}{
		"Title":   "Port Forwarding - ONOBILL",
		"Page":    "portfwd",
		"Rows":    rows,
		"Routers": routers,
		"Success": r.URL.Query().Get("success"),
		"Error":   r.URL.Query().Get("error"),
	})
}

// PortfwdCreate mengalokasikan port publik baru LALU push NAT rule ke CHR aktif.
// Alur: alokasi port unik (anti-dobel di DB) -> cek bentrok di CHR -> buat dst-nat rule.
func (h *Handler) PortfwdCreate(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	tenantID := getTenantID(r)
	routerID, _ := strconv.Atoi(r.FormValue("router_id"))
	destIP := r.FormValue("dest_ip")
	destPort, _ := strconv.Atoi(r.FormValue("dest_port"))
	purpose := r.FormValue("purpose")
	if purpose == "" {
		purpose = "custom"
	}
	if routerID == 0 || destIP == "" {
		http.Redirect(w, r, "/portfwd?error=router+dan+IP+tujuan+wajib", http.StatusSeeOther)
		return
	}
	svc := portfwd.NewService(h.Repo.DB(), 30000, 40000)
	f, err := svc.Allocate(tenantID, uint(routerID), destIP, destPort, purpose)
	if err != nil {
		http.Redirect(w, r, "/portfwd?error="+err.Error(), http.StatusSeeOther)
		return
	}

	// Push NAT rule ke CHR aktif (kalau ada CHR terdaftar). Cek dobel port di CHR juga.
	if chrServer, err := h.Repo.ActiveCHR(); err == nil && chrServer != nil {
		if err := portfwd.PushNATRule(chrServer.Host, chrServer.APIPort, chrServer.Username, chrServer.Password, f); err != nil {
			// Rule gagal -> hapus alokasi agar port tidak menggantung, lalu laporkan.
			_ = svc.Delete(f.ID)
			http.Redirect(w, r, "/portfwd?error="+err.Error(), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/portfwd?success=NAT+aktif+di+CHR+port+"+strconv.Itoa(f.PublicPort), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/portfwd?success=created+(belum+ada+CHR,+NAT+belum+dipush)", http.StatusSeeOther)
}

// PortfwdDelete menghapus port-forward (membebaskan port) + hapus NAT rule di CHR.
func (h *Handler) PortfwdDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := parseID(r, "id")
	// Hapus rule di CHR dulu (kalau ada), lalu hapus alokasi di DB.
	if chrServer, err := h.Repo.ActiveCHR(); err == nil && chrServer != nil {
		_ = portfwd.RemoveNATRule(chrServer.Host, chrServer.APIPort, chrServer.Username, chrServer.Password, id)
	}
	svc := portfwd.NewService(h.Repo.DB(), 30000, 40000)
	if err := svc.Delete(id); err != nil {
		http.Redirect(w, r, "/portfwd?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/portfwd?success=deleted", http.StatusSeeOther)
}
