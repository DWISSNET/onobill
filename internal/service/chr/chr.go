// Package chr menarik status real-time dari server CHR (MikroTik):
// online/offline, uptime, dan jumlah tunnel L2TP yang sedang konek.
package chr

import (
	"crypto/tls"
	"net"
	"time"

	routeros "github.com/go-routeros/routeros/v3"

	"onobill/internal/domain"
)

// Status hasil probe ke satu CHR.
type Status struct {
	Online       bool   // CHR bisa dihubungi (TCP + API login)
	Uptime       string // contoh: "3d2h15m" dari MikroTik
	Version      string // versi RouterOS CHR
	Identity     string // nama router CHR
	ActiveTunnel int    // jumlah interface L2TP yang sedang running (router tenant konek)
	TotalTunnel  int    // total L2TP server yang dikonfigurasi
	Error        string // pesan error kalau gagal
}

// Probe menghubungi CHR via API MikroTik dan mengambil statusnya.
// port=0 berarti 8728. useTLS=true kalau CHR pakai api-ssl (8729).
func Probe(host string, port int, username, password string, useTLS bool) Status {
	st := Status{}
	if host == "" {
		st.Error = "host kosong"
		return st
	}
	addr := domain.RouterAddr(host, port)

	// Cek TCP dulu (cepat, tentukan online/offline)
	conn, err := net.DialTimeout("tcp", addr, 4*time.Second)
	if err != nil {
		st.Online = false
		st.Error = "tidak terhubung: " + err.Error()
		return st
	}
	conn.Close()

	// Login API untuk ambil uptime + jumlah tunnel
	var client *routeros.Client
	dial := &net.Dialer{Timeout: 6 * time.Second}
	if useTLS {
		client, err = routeros.DialTLS(addr, username, password, &tls.Config{
			InsecureSkipVerify: true, // CHR sering pakai self-signed
			MinVersion:         tls.VersionTLS12,
		})
	} else {
		client, err = routeros.Dial(addr, username, password)
	}
	_ = dial
	if err != nil {
		// TCP hidup tapi login API gagal -> tetap online, tapi tanpa detail
		st.Online = true
		st.Error = "login API gagal: " + err.Error()
		return st
	}
	defer client.Close()
	st.Online = true

	// Resource: uptime + version + identity
	if rep, err := client.Run("/system/resource/print"); err == nil && len(rep.Re) > 0 {
		st.Uptime = rep.Re[0].Map["uptime"]
		st.Version = rep.Re[0].Map["version"]
	}
	if rep, err := client.Run("/system/identity/print"); err == nil && len(rep.Re) > 0 {
		st.Identity = rep.Re[0].Map["name"]
	}

	// Hitung tunnel L2TP aktif = interface L2TP server yang running
	// Setiap interface <l2tp-...> running berarti satu router tenant sedang konek.
	if rep, err := client.Run("/interface/l2tp-server/print"); err == nil {
		st.TotalTunnel = len(rep.Re)
		for _, re := range rep.Re {
			if re.Map["running"] == "true" {
				st.ActiveTunnel++
			}
		}
	}
	return st
}

// FindL2TPClientIP mencari IP VPN (remote-address) router tenant yang sedang
// konek ke CHR lewat L2TP, berdasarkan nama secret/user. Dipakai saat onboard
// untuk menyimpan host router yang benar (10.99.x.x) alih-alih RemoteAddr
// loopback bila fetch lewat tunnel.
// name bisa berupa username secret L2TP ATAU nama interface <l2tp-...>.
// Mengembalikan "" bila tidak ketemu / CHR tidak aktif / koneksi gagal.
func FindL2TPClientIP(host string, port int, username, password string, useTLS bool, name string) string {
	if host == "" || name == "" {
		return ""
	}
	addr := domain.RouterAddr(host, port)

	var client *routeros.Client
	var err error
	if useTLS {
		client, err = routeros.DialTLS(addr, username, password, &tls.Config{
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
		})
	} else {
		client, err = routeros.Dial(addr, username, password)
	}
	if err != nil {
		return ""
	}
	defer client.Close()

	// 1) Cari di secret aktif: /ppp/active -> user + address (remote IP client)
	if rep, err := client.Run("/ppp/active/print"); err == nil {
		for _, re := range rep.Re {
			if re.Map["name"] == name || re.Map["user"] == name {
				if ip := re.Map["address"]; ip != "" {
					return ip
				}
			}
		}
	}

	// 2) Fallback: interface <l2tp-name> -> remote-address
	if rep, err := client.Run("/interface/l2tp-server/print"); err == nil {
		for _, re := range rep.Re {
			ifName := re.Map["name"] // contoh: <l2tp-onobill-api>
			if ifName == name || ifName == "<l2tp-"+name+">" {
				if ip := re.Map["remote-address"]; ip != "" {
					return ip
				}
			}
		}
	}
	return ""
}
