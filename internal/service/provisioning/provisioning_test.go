package provisioning

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"onobill/internal/service/ipam"
)

func newIPAM(t *testing.T) *ipam.Service {
	db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	s := ipam.NewService(db)
	s.AutoMigrate()
	return s
}

func TestBuildPlanDirectV7(t *testing.T) {
	p, err := BuildPlan(RouterInput{
		TenantID: 1, Name: "Kantor", ConnectionType: "direct", ROSVersion: "v7",
		HotspotName: "WIFI-KANTOR", HotspotDNS: "wifi.kantor",
	}, newIPAM(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	if p.APIUsername != "onobill-api" || p.APIPassword == "" {
		t.Fatal("kredensial API harus ada")
	}
	if p.HotspotCIDR == "" || p.HotspotGateway != "10.10.0.1" {
		t.Fatalf("hotspot salah: %s gw=%s", p.HotspotCIDR, p.HotspotGateway)
	}
	if p.TunnelIP != "" {
		t.Fatal("direct tidak boleh ada tunnel IP")
	}
	if !strings.Contains(p.Script, "WIFI-KANTOR") || !strings.Contains(p.Script, "wifi.kantor") {
		t.Fatal("script harus memuat nama & DNS hotspot")
	}
	if !strings.Contains(p.Script, "ISOLIR") {
		t.Fatal("script harus siapkan ISOLIR")
	}
	if strings.Contains(p.Script, "l2tp-client") {
		t.Fatal("direct tidak boleh pakai l2tp-client")
	}
}

func TestBuildPlanL2TPV6(t *testing.T) {
	p, err := BuildPlan(RouterInput{
		TenantID: 1, Name: "Cabang", ConnectionType: "l2tp", ROSVersion: "v6",
		HotspotName: "WIFI-CABANG", HotspotDNS: "wifi.cabang",
	}, newIPAM(t), 2)
	if err != nil {
		t.Fatal(err)
	}
	if p.TunnelIP == "" || !strings.HasSuffix(p.TunnelIP, "/32") {
		t.Fatalf("l2tp harus dapat tunnel /32, got %s", p.TunnelIP)
	}
	if !strings.Contains(p.Script, "l2tp-client") {
		t.Fatal("l2tp harus ada l2tp-client di script")
	}
}

func TestBuildPlanNoBentrokAntarRouter(t *testing.T) {
	ip := newIPAM(t)
	p1, _ := BuildPlan(RouterInput{TenantID: 1, Name: "A", ConnectionType: "direct", ROSVersion: "v7"}, ip, 1)
	p2, _ := BuildPlan(RouterInput{TenantID: 1, Name: "B", ConnectionType: "direct", ROSVersion: "v7"}, ip, 2)
	if p1.HotspotCIDR == p2.HotspotCIDR {
		t.Fatalf("subnet hotspot bentrok: %s == %s", p1.HotspotCIDR, p2.HotspotCIDR)
	}
}

func TestScriptContainsCredentials(t *testing.T) {
	p, _ := BuildPlan(RouterInput{TenantID: 1, Name: "X", ConnectionType: "direct", ROSVersion: "v7"}, newIPAM(t), 1)
	if !strings.Contains(p.Script, p.APIPassword) || !strings.Contains(p.Script, p.APIUsername) {
		t.Fatal("script harus memuat kredensial API")
	}
}
