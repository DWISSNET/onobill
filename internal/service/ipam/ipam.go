// Package ipam — IP Address Management ONOBILL.
// Auto-alokasi subnet anti-bentrok per tenant/router.
// Pengaman terakhir anti-bentrok: UNIQUE constraint di tabel ipam_allocations.
package ipam

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"gorm.io/gorm"
)

var (
	ErrPoolNotFound  = errors.New("ipam pool tidak ditemukan")
	ErrPoolExhausted = errors.New("ipam pool habis")
)

// Pool master per keperluan (tunnel/hotspot/pppoe/acs).
type Pool struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"column:name;size:50;uniqueIndex"`
	CIDR      string `gorm:"column:cidr;size:50"`
	PrefixLen int    `gorm:"column:prefix_len"` // ukuran subnet per alokasi
}

// Allocation subnet yang sudah dialokasikan.
type Allocation struct {
	ID       uint   `gorm:"primaryKey"`
	PoolID   uint   `gorm:"column:pool_id;index;uniqueIndex:idx_pool_cidr"`
	TenantID uint   `gorm:"column:tenant_id;index"`
	RouterID uint   `gorm:"column:router_id;index"`
	CIDR     string `gorm:"column:cidr;size:50;uniqueIndex:idx_pool_cidr"`
}

// Service IPAM.
type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// AutoMigrate membuat tabel + seed master pool default.
func (s *Service) AutoMigrate() error {
	if err := s.db.AutoMigrate(&Pool{}, &Allocation{}); err != nil {
		return err
	}
	defaults := []Pool{
		{Name: "tunnel", CIDR: "10.200.0.0/16", PrefixLen: 32},
		{Name: "hotspot", CIDR: "10.10.0.0/16", PrefixLen: 24},
		{Name: "pppoe", CIDR: "10.20.0.0/16", PrefixLen: 24},
		{Name: "acs", CIDR: "10.30.0.0/16", PrefixLen: 24},
	}
	for _, p := range defaults {
		var cnt int64
		s.db.Model(&Pool{}).Where("name=?", p.Name).Count(&cnt)
		if cnt == 0 {
			if err := s.db.Create(&p).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// Allocate mengambil subnet kosong pertama dari pool. Anti-bentrok via UNIQUE(pool_id,cidr).
func (s *Service) Allocate(poolName string, tenantID, routerID uint) (string, error) {
	var pool Pool
	if err := s.db.Where("name=?", poolName).First(&pool).Error; err != nil {
		return "", ErrPoolNotFound
	}
	used := map[string]bool{}
	var allocs []Allocation
	s.db.Where("pool_id=?", pool.ID).Find(&allocs)
	for _, a := range allocs {
		used[a.CIDR] = true
	}
	_, base, err := net.ParseCIDR(pool.CIDR)
	if err != nil {
		return "", fmt.Errorf("pool cidr invalid: %w", err)
	}
	for _, cidr := range subnets(base, pool.PrefixLen) {
		if used[cidr] {
			continue
		}
		a := Allocation{PoolID: pool.ID, TenantID: tenantID, RouterID: routerID, CIDR: cidr}
		if err := s.db.Create(&a).Error; err != nil {
			// race/bentrok -> coba kandidat berikutnya
			used[cidr] = true
			continue
		}
		return cidr, nil
	}
	return "", ErrPoolExhausted
}

// Release membebaskan alokasi.
func (s *Service) Release(cidr string) error {
	return s.db.Where("cidr=?", cidr).Delete(&Allocation{}).Error
}

// ForRouter mengembalikan semua alokasi milik sebuah router.
func (s *Service) ForRouter(routerID uint) ([]Allocation, error) {
	var out []Allocation
	err := s.db.Where("router_id=?", routerID).Find(&out).Error
	return out, err
}

// Usage statistik pemakaian pool.
func (s *Service) Usage(poolName string) (used int, total int, err error) {
	var pool Pool
	if err := s.db.Where("name=?", poolName).First(&pool).Error; err != nil {
		return 0, 0, ErrPoolNotFound
	}
	var cnt int64
	s.db.Model(&Allocation{}).Where("pool_id=?", pool.ID).Count(&cnt)
	_, base, _ := net.ParseCIDR(pool.CIDR)
	total = len(subnets(base, pool.PrefixLen))
	return int(cnt), total, nil
}

// subnets menghasilkan semua subnet dari base dengan prefix target (urut deterministic).
func subnets(base *net.IPNet, prefixLen int) []string {
	var out []string
	ones, _ := base.Mask.Size()
	// jumlah subnet = 2^(prefixLen-ones)
	count := 1 << uint(prefixLen-ones)
	size := 1 << uint(32-prefixLen) // untuk IPv4 /prefixLen
	ip := base.IP.Mask(base.Mask).To4()
	if ip == nil {
		return out
	}
	baseInt := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	for i := 0; i < count; i++ {
		cur := baseInt + uint32(i)*uint32(size)
		out = append(out, fmt.Sprintf("%d.%d.%d.%d/%d",
			byte(cur>>24), byte(cur>>16), byte(cur>>8), byte(cur), prefixLen))
	}
	return out
}

// GatewayIP mengambil IP host pertama dari sebuah subnet (untuk gateway hotspot).
func GatewayIP(cidr string) string {
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return ""
	}
	ip := n.IP.Mask(n.Mask).To4()
	if ip == nil {
		return ""
	}
	gw := make(net.IP, 4)
	copy(gw, ip)
	gw[3]++
	return strings.TrimSpace(gw.String())
}
