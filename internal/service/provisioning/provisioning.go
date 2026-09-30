// Package provisioning — tambah router super mudah: isi form -> generate script copas.
// Prinsip: Mikrotik tenant "dumb device" — cuma perlu user+password API.
// Script menyiapkan user API, koneksi (Direct/L2TP), hotspot, address-list ISOLIR,
// sesuai versi RouterOS (v6/v7).
package provisioning

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"onobill/internal/service/ipam"
)

// RouterInput data form tambah router (sederhana — admin cuma isi ini).
type RouterInput struct {
	TenantID       uint
	Name           string
	ConnectionType string // "direct" | "l2tp"
	ROSVersion     string // "v6" | "v7"
	ServiceType    string // "app" | "hotspot" | "pppoe"
	ExpiredBilling string // tanggal expired billing (YYYY-MM-DD)
	HotspotName    string
	HotspotDNS     string
}

// RouterPlan hasil provisioning: kredensial + alokasi IP + script.
type RouterPlan struct {
	Name           string
	ConnectionType string
	ROSVersion     string
	APIUsername    string
	APIPassword    string
	HotspotCIDR    string
	HotspotGateway string
	ACSCIDR        string
	TunnelIP       string // bila L2TP
	Script         string
}

// generateAPIPassword password acak URL-safe.
func generateAPIPassword() string {
	b := make([]byte, 12)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// BuildPlan membuat rencana provisioning: alokasi IP + siapkan kredensial.
// Tidak menulis DB router — itu tugas pemanggil (service router).
func BuildPlan(in RouterInput, ipamSvc *ipam.Service, routerID uint) (*RouterPlan, error) {
	if in.ROSVersion == "" {
		in.ROSVersion = "v7"
	}
	if in.ConnectionType == "" {
		in.ConnectionType = "direct"
	}
	plan := &RouterPlan{
		Name:           in.Name,
		ConnectionType: in.ConnectionType,
		ROSVersion:     in.ROSVersion,
		APIUsername:    "onobill-api",
		APIPassword:    generateAPIPassword(),
	}
	// Alokasi IP anti-bentrok per keperluan
	hs, err := ipamSvc.Allocate("hotspot", in.TenantID, routerID)
	if err != nil {
		return nil, fmt.Errorf("alokasi hotspot: %w", err)
	}
	plan.HotspotCIDR = hs
	plan.HotspotGateway = ipam.GatewayIP(hs)
	acs, err := ipamSvc.Allocate("acs", in.TenantID, routerID)
	if err != nil {
		return nil, fmt.Errorf("alokasi acs: %w", err)
	}
	plan.ACSCIDR = acs
	if in.ConnectionType == "l2tp" {
		t, err := ipamSvc.Allocate("tunnel", in.TenantID, routerID)
		if err != nil {
			return nil, fmt.Errorf("alokasi tunnel: %w", err)
		}
		plan.TunnelIP = t
	}
	plan.Script = GenerateScript(in, plan)
	return plan, nil
}

// GenerateScript menghasilkan script RouterOS siap copas sesuai versi & koneksi.
func GenerateScript(in RouterInput, p *RouterPlan) string {
	v := in.ROSVersion
	hsName := in.HotspotName
	if hsName == "" {
		hsName = "ONOBILL-HOTSPOT"
	}
	hsDNS := in.HotspotDNS
	if hsDNS == "" {
		hsDNS = "hotspot.onobill"
	}
	gw := p.HotspotGateway

	L := []string{
		"# ============================================",
		fmt.Sprintf("# ONOBILL - Provisioning Router: %s", in.Name),
		fmt.Sprintf("# RouterOS %s | Koneksi: %s", v, in.ConnectionType),
		"# Copy-paste SELURUH baris ke terminal Mikrotik",
		"# ============================================",
		"",
		"# --- 1. User API untuk ONOBILL (jangan dihapus) ---",
		fmt.Sprintf("/user add name=%s password=%s group=full comment=\"ONOBILL API\"", p.APIUsername, p.APIPassword),
		"/ip service set api disabled=no port=8728",
		"",
	}

	// Koneksi
	if in.ConnectionType == "l2tp" {
		L = append(L,
			"# --- 2. Tunnel L2TP ke CHR ONOBILL ---",
			fmt.Sprintf("# (isi connect-to & user sesuai CHR; IP tunnel router: %s)", p.TunnelIP),
			"/interface l2tp-client add name=onobill-tunnel connect-to=CHR_IP user=onobill password="+p.APIPassword+" disabled=no comment=\"ONOBILL tunnel\"",
			"",
		)
	} else {
		L = append(L,
			"# --- 2. Koneksi Direct (API langsung) ---",
			"# Router harus bisa dijangkau ONOBILL via IP publik/DDNS",
			"",
		)
	}

	// Hotspot (nama + DNS + network)
	L = append(L,
		"# --- 3. Setup Hotspot ---",
		fmt.Sprintf("/ip pool add name=onobill-hotspot ranges=%s-%s", gw, gw),
		fmt.Sprintf("/ip hotspot profile add name=onobill-prof dns-name=%s hotspot-address=%s login-by=http-chap,http-pap", hsDNS, gw),
		fmt.Sprintf("/ip hotspot add name=%s interface=ether2 address-pool=onobill-hotspot profile=onobill-prof disabled=no", hsName),
		fmt.Sprintf("/ip address add address=%s/24 comment=\"ONOBILL hotspot\" interface=ether2", gw),
		"",
	)

	// ISOLIR (dikelola otomatis ONOBILL)
	L = append(L,
		"# --- 4. ISOLIR (dikelola otomatis oleh ONOBILL) ---",
		"/ip firewall address-list add list=ISOLIR address=0.0.0.0 comment=\"placeholder\"",
		"/ip firewall nat add chain=dstnat src-address-list=ISOLIR protocol=tcp dst-port=80 action=redirect to-ports=8080 comment=\"ONOBILL isolir redirect\"",
		"",
		"# Selesai! Router akan terhubung & dikelola ONOBILL.",
		fmt.Sprintf("# Simpan kredensial API: user=%s pass=%s", p.APIUsername, p.APIPassword),
	)

	out := ""
	for i, line := range L {
		out += line
		if i < len(L)-1 {
			out += "\n"
		}
	}
	return out
}
