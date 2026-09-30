package http

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"

	"onobill/internal/service/chr"
	"onobill/internal/service/provisioning"
)

// ============ PROVISIONING (Tambah Router Copas) ============

// ProvisioningRouter membuat rencana provisioning + script copas untuk router baru.
// POST /routers/provision  (form: name, connection_type, ros_version, hotspot_name, hotspot_dns)
func (h *Handler) ProvisioningRouter(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	if h.IPAM == nil {
		writeError(w, http.StatusServiceUnavailable, "IPAM tidak tersedia")
		return
	}
	in := provisioning.RouterInput{
		TenantID:       tenantID,
		Name:           r.FormValue("name"),
		ConnectionType: r.FormValue("connection_type"),
		ROSVersion:     r.FormValue("ros_version"),
		HotspotName:    r.FormValue("hotspot_name"),
		HotspotDNS:     r.FormValue("hotspot_dns"),
	}
	plan, err := provisioning.BuildPlan(in, h.IPAM, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Sisipkan check-in URL + token ke script agar router menelepon balik & tersimpan
	token := h.Repo.EnsureOnboardToken(tenantID)
	checkinURL := h.BaseURL + "/api/onboard/" + token
	plan.Script += "\n\n# --- 5. Daftar ke ONOBILL (otomatis tersimpan) ---\n" +
		"/tool fetch url=\"" + checkinURL + "?name=" + in.Name + "&user=" + plan.APIUsername + "&pass=" + plan.APIPassword + "&host=$[/system identity get value-name=name]\" keep-result=no\n"
	render(w, "router_script", map[string]interface{}{
		"Plan": plan,
	})
}

// APIProvisioningRouter versi JSON dari provisioning.
func (h *Handler) APIProvisioningRouter(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	if h.IPAM == nil {
		writeError(w, http.StatusServiceUnavailable, "IPAM tidak tersedia")
		return
	}
	in := provisioning.RouterInput{
		TenantID:       tenantID,
		Name:           r.FormValue("name"),
		ConnectionType: r.FormValue("connection_type"),
		ROSVersion:     r.FormValue("ros_version"),
		HotspotName:    r.FormValue("hotspot_name"),
		HotspotDNS:     r.FormValue("hotspot_dns"),
	}
	plan, err := provisioning.BuildPlan(in, h.IPAM, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	token := h.Repo.EnsureOnboardToken(tenantID)
	plan.Script += "\n\n# --- 5. Daftar ke ONOBILL ---\n/tool fetch url=\"" + h.BaseURL + "/api/onboard/" + token + "?name=" + in.Name + "&user=" + plan.APIUsername + "&pass=" + plan.APIPassword + "\" keep-result=no\n"
	writeJSON(w, http.StatusOK, plan)
}

// RouterCheckIn endpoint yang dipanggil router setelah menjalankan script onboarding.
// GET/POST /api/onboard/{token}?name=..&user=..&pass=..&host=..
// TANPA auth sesi — divalidasi token onboarding tenant. Menyimpan router otomatis.
func (h *Handler) RouterCheckIn(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	tenantID, ok := h.Repo.TenantByOnboardToken(token)
	if !ok {
		writeError(w, http.StatusForbidden, "invalid onboard token")
		return
	}
	q := r.URL.Query()
	get := func(k string) string {
		if v := q.Get(k); v != "" {
			return v
		}
		return r.FormValue(k)
	}
	name := get("name")
	if name == "" {
		name = "router-" + token[:6]
	}
	user := get("user")
	pass := get("pass")
	host := get("host")
	if host == "" {
		host = r.RemoteAddr
	}
	port, _ := strconv.Atoi(get("port"))
	vpnIP := get("vpnip")

	// Bila host hasil deteksi adalah loopback/tunnel (fetch lewat SSH tunnel),
	// itu BUKAN IP router. Cari IP VPN L2TP router yang sebenarnya di CHR
	// agar host tersimpan benar (mis 10.99.0.3) dan bisa di-dial balik.
	if isLoopbackHost(host) {
		if vpnIP != "" {
			host = vpnIP
		} else if c, err := h.Repo.ActiveCHR(); err == nil && c != nil {
			lookup := user
			if lookup == "" {
				lookup = name
			}
			if ip := chr.FindL2TPClientIP(c.Host, c.APIPort, c.Username, c.Password, false, lookup); ip != "" {
				host = ip
			}
		}
	}

	router, created, err := h.Router.CheckIn(tenantID, name, host, user, pass, port, vpnIP)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"created": created,
		"router":  router.Name,
		"message": "Router terdaftar di ONOBILL",
	})
}

// isLoopbackHost melaporkan true bila host menunjuk loopback/tunnel lokal
// (127.x, ::1, localhost) — pertanda fetch lewat SSH tunnel, bukan IP router.
func isLoopbackHost(host string) bool {
	h := strings.TrimSpace(host)
	if h == "" {
		return false
	}
	// Pisahkan port bila ada (mis "127.0.0.1:56332").
	if hostOnly, _, err := net.SplitHostPort(h); err == nil {
		h = hostOnly
	}
	h = strings.Trim(h, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
