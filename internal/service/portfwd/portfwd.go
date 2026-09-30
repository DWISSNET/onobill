// Package portfwd mengelola port-forwarding di CHR untuk remote router tenant.
// Setiap router tenant dapat port publik unik di CHR yang di-NAT ke Winbox router itu.
// Range port dialokasikan otomatis & unik (anti-bentrok) via UNIQUE(public_port).
package portfwd

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	routeros "github.com/go-routeros/routeros/v3"
	"gorm.io/gorm"

	"onobill/internal/domain"
)

var (
	ErrRangeExhausted = errors.New("range port habis")
	ErrNotFound       = errors.New("port-forward tidak ditemukan")
)

// Forward satu pemetaan port publik CHR -> router tenant.
type Forward struct {
	ID         uint   `gorm:"primaryKey"`
	RouterID   uint   `gorm:"column:router_id;index"`
	TenantID   uint   `gorm:"column:tenant_id;index"`
	PublicPort int    `gorm:"column:public_port;uniqueIndex"`
	DestIP     string `gorm:"column:dest_ip;size:50"`
	DestPort   int    `gorm:"column:dest_port"`
	Protocol   string `gorm:"column:protocol;size:10;default:'tcp'"`
	Purpose    string `gorm:"column:purpose;size:30;default:'winbox'"`
}

// Service port-forward.
type Service struct {
	db        *gorm.DB
	rangeFrom int
	rangeTo   int
}

func NewService(db *gorm.DB, rangeFrom, rangeTo int) *Service {
	if rangeFrom == 0 {
		rangeFrom = 30000
	}
	if rangeTo == 0 {
		rangeTo = 40000
	}
	return &Service{db: db, rangeFrom: rangeFrom, rangeTo: rangeTo}
}

func (s *Service) AutoMigrate() error {
	return s.db.AutoMigrate(&Forward{})
}

// Allocate membuat port-forward baru untuk router. PublicPort dialokasikan otomatis & unik.
func (s *Service) Allocate(tenantID, routerID uint, destIP string, destPort int, purpose string) (*Forward, error) {
	if purpose == "" {
		purpose = "winbox"
	}
	if destPort == 0 {
		destPort = 8291 // Winbox default
	}
	// Kumpulkan port yang sudah dipakai
	used := map[int]bool{}
	var fwds []Forward
	s.db.Find(&fwds)
	for _, f := range fwds {
		used[f.PublicPort] = true
	}
	for p := s.rangeFrom; p <= s.rangeTo; p++ {
		if used[p] {
			continue
		}
		f := Forward{
			RouterID:   routerID,
			TenantID:   tenantID,
			PublicPort: p,
			DestIP:     destIP,
			DestPort:   destPort,
			Protocol:   "tcp",
			Purpose:    purpose,
		}
		if err := s.db.Create(&f).Error; err != nil {
			used[p] = true // race -> coba port berikutnya
			continue
		}
		return &f, nil
	}
	return nil, ErrRangeExhausted
}

// ListByRouter semua port-forward milik router.
func (s *Service) ListByRouter(routerID uint) ([]Forward, error) {
	var out []Forward
	err := s.db.Where("router_id=?", routerID).Find(&out).Error
	return out, err
}

// Delete menghapus port-forward (membebaskan port).
func (s *Service) Delete(id uint) error {
	res := s.db.Delete(&Forward{}, id)
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

// WinboxAddress string "publicIP:port" untuk remote Winbox.
func WinboxAddress(publicIP string, f *Forward) string {
	return fmt.Sprintf("%s:%d", publicIP, f.PublicPort)
}

// ============ PUSH NAT RULE KE CHR ============
// Saat port-forward ditambah, ONOBILL otomatis membuat dst-nat rule di CHR:
//   ipCHR:PublicPort  -->  DestIP:DestPort (router/service tenant di belakang NAT)
// plus cek agar PublicPort tidak dobel/bentrok dengan rule yang sudah ada di CHR.

// IsPortUsedOnCHR mengecek apakah PublicPort sudah dipakai oleh dst-nat rule lain di CHR.
// Mencegah dobel: kalau port sudah ada di firewall CHR, kembalikan true.
func IsPortUsedOnCHR(chrHost string, chrAPIPort int, chrUser, chrPass string, publicPort int) (bool, error) {
	c, err := dialCHR(chrHost, chrAPIPort, chrUser, chrPass)
	if err != nil {
		return false, err
	}
	defer c.Close()
	rep, err := c.Run("/ip/firewall/nat/print", "?dst-port="+strconv.Itoa(publicPort), "?chain=dstnat")
	if err != nil {
		return false, err
	}
	return len(rep.Re) > 0, nil
}

// PushNATRule membuat dst-nat rule di CHR: ipCHR:publicPort -> destIP:destPort.
// comment dipakai sebagai penanda (onobill-pf-<id>) supaya mudah dihapus lagi.
// Mengembalikan error bila CHR tidak bisa dihubungi atau port sudah bentrok.
func PushNATRule(chrHost string, chrAPIPort int, chrUser, chrPass string, f *Forward) error {
	c, err := dialCHR(chrHost, chrAPIPort, chrUser, chrPass)
	if err != nil {
		return fmt.Errorf("CHR tidak terhubung: %w", err)
	}
	defer c.Close()

	comment := fmt.Sprintf("onobill-pf-%d", f.ID)
	// Idempotent: kalau rule dengan comment ini sudah ada, anggap sukses.
	if rep, err := c.Run("/ip/firewall/nat/print", "?comment="+comment); err == nil && len(rep.Re) > 0 {
		return nil
	}

	// Cek dobel port: jangan buat rule kalau dst-port sudah dipakai rule lain (bukan milik kita).
	if rep, err := c.Run("/ip/firewall/nat/print", "?dst-port="+strconv.Itoa(f.PublicPort), "?chain=dstnat"); err == nil {
		for _, re := range rep.Re {
			if re.Map["comment"] != comment {
				return fmt.Errorf("port %d sudah dipakai rule lain di CHR", f.PublicPort)
			}
		}
	}

	_, err = c.Run("/ip/firewall/nat/add",
		"=chain=dstnat",
		"=protocol=tcp",
		"=dst-port="+strconv.Itoa(f.PublicPort),
		"=action=dst-nat",
		"=to-addresses="+f.DestIP,
		"=to-ports="+strconv.Itoa(f.DestPort),
		"=comment="+comment,
	)
	if err != nil {
		return fmt.Errorf("gagal membuat NAT rule: %w", err)
	}
	return nil
}

// RemoveNATRule menghapus dst-nat rule di CHR berdasarkan comment penanda.
func RemoveNATRule(chrHost string, chrAPIPort int, chrUser, chrPass string, forwardID uint) error {
	c, err := dialCHR(chrHost, chrAPIPort, chrUser, chrPass)
	if err != nil {
		return fmt.Errorf("CHR tidak terhubung: %w", err)
	}
	defer c.Close()
	comment := fmt.Sprintf("onobill-pf-%d", forwardID)
	rep, err := c.Run("/ip/firewall/nat/print", "?comment="+comment)
	if err != nil {
		return err
	}
	for _, re := range rep.Re {
		if id, ok := re.Map[".id"]; ok {
			if _, err := c.Run("/ip/firewall/nat/remove", "=.id="+id); err != nil {
				return err
			}
		}
	}
	return nil
}

// dialCHR membuka koneksi API ke CHR.
func dialCHR(host string, port int, user, pass string) (*routeros.Client, error) {
	addr := domain.RouterAddr(host, port)
	d := &net.Dialer{Timeout: 6 * time.Second}
	_ = d
	return routeros.Dial(addr, user, pass)
}
