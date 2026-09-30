package portal

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newSvc(t *testing.T) *Service {
	db, _ := gorm.Open(sqlite.Open(t.TempDir()+"/p.db"), &gorm.Config{})
	s := NewService(db)
	s.AutoMigrate()
	return s
}

func TestRegisterLoginValidate(t *testing.T) {
	s := newSvc(t)
	if err := s.Register(1, 100, "pelanggan1", "rahasia"); err != nil {
		t.Fatal(err)
	}
	sess, err := s.Login("pelanggan1", "rahasia")
	if err != nil {
		t.Fatal(err)
	}
	custID, tenantID, err := s.ValidateSession(sess.Token)
	if err != nil {
		t.Fatal(err)
	}
	if custID != 100 || tenantID != 1 {
		t.Fatalf("sesi salah: cust=%d tenant=%d", custID, tenantID)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	s := newSvc(t)
	s.Register(1, 100, "pelanggan1", "rahasia")
	if _, err := s.Login("pelanggan1", "salah"); err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogout(t *testing.T) {
	s := newSvc(t)
	s.Register(1, 100, "pel1", "pass")
	sess, _ := s.Login("pel1", "pass")
	if err := s.Logout(sess.Token); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ValidateSession(sess.Token); err != ErrInvalidCredentials {
		t.Fatalf("setelah logout harus invalid, got %v", err)
	}
}
