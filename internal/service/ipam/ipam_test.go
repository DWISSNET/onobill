package ipam

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return db
}

func TestAllocateSequentialUnique(t *testing.T) {
	s := NewService(newTestDB(t))
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Allocate("hotspot", 1, 1)
	b, _ := s.Allocate("hotspot", 1, 2)
	c, _ := s.Allocate("hotspot", 2, 3)
	if a == b || b == c || a == c {
		t.Fatalf("bentrok! %s %s %s", a, b, c)
	}
	if a != "10.10.0.0/24" || b != "10.10.1.0/24" || c != "10.10.2.0/24" {
		t.Fatalf("urutan salah: %s %s %s", a, b, c)
	}
}

func TestAllocateTunnelHost(t *testing.T) {
	s := NewService(newTestDB(t))
	s.AutoMigrate()
	ip, err := s.Allocate("tunnel", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if ip != "10.200.0.0/32" {
		t.Fatalf("expected /32 host, got %s", ip)
	}
}

func TestNoDuplicateAfterRelease(t *testing.T) {
	s := NewService(newTestDB(t))
	s.AutoMigrate()
	a, _ := s.Allocate("pppoe", 1, 1)
	s.Release(a)
	b, _ := s.Allocate("pppoe", 1, 2)
	if b != a {
		t.Fatalf("setelah release harus reuse subnet terkecil, got %s want %s", b, a)
	}
}

func TestPoolExhausted(t *testing.T) {
	db := newTestDB(t)
	s := NewService(db)
	s.AutoMigrate()
	db.Create(&Pool{Name: "tiny", CIDR: "192.168.99.0/30", PrefixLen: 31})
	if _, err := s.Allocate("tiny", 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Allocate("tiny", 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Allocate("tiny", 0, 0); err != ErrPoolExhausted {
		t.Fatalf("expected ErrPoolExhausted, got %v", err)
	}
}

func TestUsage(t *testing.T) {
	s := NewService(newTestDB(t))
	s.AutoMigrate()
	s.Allocate("hotspot", 1, 1)
	used, total, err := s.Usage("hotspot")
	if err != nil {
		t.Fatal(err)
	}
	if used != 1 || total != 256 {
		t.Fatalf("usage: used=%d total=%d", used, total)
	}
}

func TestGatewayIP(t *testing.T) {
	if g := GatewayIP("10.10.5.0/24"); g != "10.10.5.1" {
		t.Fatalf("gateway salah: %s", g)
	}
}
