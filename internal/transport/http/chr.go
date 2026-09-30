package http

import (
	"net/http"
	"strconv"

	"onobill/internal/domain"
	"onobill/internal/service/chr"
)

// CHRWithStatus gabungan data CHR + status real-time hasil probe.
type CHRWithStatus struct {
	domain.CHRServer
	Status chr.Status
}

// CHRList menampilkan daftar server CHR + form tambah + status real-time.
func (h *Handler) CHRList(w http.ResponseWriter, r *http.Request) {
	list, _ := h.Repo.ListCHR()
	rows := make([]CHRWithStatus, 0, len(list))
	for _, c := range list {
		var st chr.Status
		if c.IsActive {
			st = chr.Probe(c.Host, c.APIPort, c.Username, c.Password, false)
		} else {
			st = chr.Status{Error: "nonaktif"}
		}
		rows = append(rows, CHRWithStatus{CHRServer: c, Status: st})
	}
	render(w, "chr", map[string]interface{}{
		"Title":   "CHR - ONOBILL",
		"Page":    "chr",
		"CHRs":    rows,
		"Success": r.URL.Query().Get("success"),
		"Error":   r.URL.Query().Get("error"),
	})
}

// CHRCreate menambah server CHR baru.
func (h *Handler) CHRCreate(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	port, _ := strconv.Atoi(r.FormValue("api_port"))
	if port == 0 {
		port = 8728
	}
	c := &domain.CHRServer{
		Name:      r.FormValue("name"),
		Host:      r.FormValue("host"),
		APIPort:   port,
		Username:  r.FormValue("username"),
		Password:  r.FormValue("password"),
		L2TPRange: r.FormValue("l2tp_range"),
		Secret:    r.FormValue("secret"),
		Note:      r.FormValue("note"),
		IsActive:  true,
	}
	if c.Name == "" || c.Host == "" {
		http.Redirect(w, r, "/superadmin/chr?error=nama+dan+host+wajib", http.StatusSeeOther)
		return
	}
	if err := h.Repo.CreateCHR(c); err != nil {
		http.Redirect(w, r, "/superadmin/chr?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/superadmin/chr?success=created", http.StatusSeeOther)
}

// CHRDelete menghapus server CHR.
func (h *Handler) CHRDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := parseID(r, "id")
	if err := h.Repo.DeleteCHR(id); err != nil {
		http.Redirect(w, r, "/superadmin/chr?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/superadmin/chr?success=deleted", http.StatusSeeOther)
}

// CHRToggle mengaktifkan/nonaktifkan CHR.
func (h *Handler) CHRToggle(w http.ResponseWriter, r *http.Request) {
	id, _ := parseID(r, "id")
	c, err := h.Repo.GetCHRByID(id)
	if err != nil {
		http.Redirect(w, r, "/superadmin/chr?error=chr+tidak+ditemukan", http.StatusSeeOther)
		return
	}
	c.IsActive = !c.IsActive
	if err := h.Repo.UpdateCHR(c); err != nil {
		http.Redirect(w, r, "/superadmin/chr?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/superadmin/chr?success=updated", http.StatusSeeOther)
}
