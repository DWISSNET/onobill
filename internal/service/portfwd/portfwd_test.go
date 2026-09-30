package portfwd

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(db, 30000, 30005) // range kecil untuk test
	if err := s.AutoMigrate(); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAllocateUniquePorts(t *testing.T) {
	s := newSvc(t)
	f1, err := s.Allocate(1, 1, "10.200.0.2", 8291, "winbox")
	if err != nil {
		t.Fatal(err)
	}
	f2, err := s.Allocate(1, 2, "10.200.0.3", 8291, "winbox")
	if err != nil {
		t.Fatal(err)
	}
	if f1.PublicPort == f2.PublicPort {
		t.Fatalf("port bentrok: %d == %d", f1.PublicPort, f2.PublicPort)
	}
	if f1.PublicPort != 30000 || f2.PublicPort != 30001 {
		t.Fatalf("alokasi urut salah: %d %d", f1.PublicPort, f2.PublicPort)
	}
}

func TestRangeExhausted(t *testing.T) {
	s := newSvc(t)
	// range 30000-30005 = 6 port
	for i := 0; i < 6; i++ {
		if _, err := s.Allocate(1, uint(i+1), "10.200.0.2", 8291, "winbox"); err != nil {
			t.Fatalf("alokasi ke-%d gagal: %v", i, err)
		}
	}
	if _, err := s.Allocate(1, 99, "10.200.0.9", 8291, "winbox"); err != ErrRangeExhausted {
		t.Fatalf("expected ErrRangeExhausted, got %v", err)
	}
}

func TestDeleteFreesPort(t *testing.T) {
	s := newSvc(t)
	f, _ := s.Allocate(1, 1, "10.200.0.2", 8291, "winbox")
	if err := s.Delete(f.ID); err != nil {
		t.Fatal(err)
	}
	f2, _ := s.Allocate(1, 2, "10.200.0.3", 8291, "winbox")
	if f2.PublicPort != f.PublicPort {
		t.Fatalf("port yang dibebaskan harus dipakai ulang: %d != %d", f2.PublicPort, f.PublicPort)
	}
}

func TestWinboxAddress(t *testing.T) {
	f := &Forward{PublicPort: 30001}
	if got := WinboxAddress("203.0.113.5", f); got != "203.0.113.5:30001" {
		t.Fatalf("winbox address salah: %s", got)
	}
}
