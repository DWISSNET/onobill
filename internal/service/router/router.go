package router

import (
	"errors"
	"net"
	"onobill/internal/domain"
	"onobill/internal/repository"
	"time"
)

type Service struct {
	repo *repository.Repository
}

func NewService(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

var ErrNotFound = errors.New("router not found")

func (s *Service) Create(tenantID uint, name, host, username, password string, port int) (*domain.Router, error) {
	if port == 0 {
		port = 8728
	}
	r := &domain.Router{
		TenantID: tenantID,
		Name:     name,
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
		IsActive: true,
	}
	if err := s.repo.CreateRouter(r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Service) Get(tenantID, id uint) (*domain.Router, error) {
	r, err := s.repo.GetRouterByID(tenantID, id)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return r, nil
}

func (s *Service) List(tenantID uint) ([]domain.Router, error) {
	return s.repo.ListRouters(tenantID)
}

func (s *Service) Update(tenantID, id uint, name, host, username, password string, port int, isActive bool) (*domain.Router, error) {
	r, err := s.Get(tenantID, id)
	if err != nil {
		return nil, err
	}
	r.Name = name
	r.Host = host
	r.Username = username
	if password != "" {
		r.Password = password
	}
	r.Port = port
	r.IsActive = isActive
	if err := s.repo.UpdateRouter(r); err != nil {
		return nil, err
	}
	return r, nil
}

func (s *Service) Delete(tenantID, id uint) error {
	if _, err := s.Get(tenantID, id); err != nil {
		return err
	}
	return s.repo.DeleteRouter(tenantID, id)
}

// CheckIn dipanggil router setelah menjalankan script onboarding (via /tool fetch).
// Router "menelepon balik" ONOBILL dengan token rahasia -> otomatis tersimpan.
// Ini menutup celah alur: sebelumnya router dibuat di form tapi tidak pernah tersimpan.
func (s *Service) CheckIn(tenantID uint, name, host, username, password string, port int, vpnIP string) (*domain.Router, bool, error) {
	// Cari router yang sudah ada berdasarkan nama (idempotent: script bisa di-paste ulang)
	routers, err := s.repo.ListRouters(tenantID)
	if err == nil {
		for i := range routers {
			if routers[i].Name == name {
				r := routers[i]
				r.Host = host
				r.Username = username
				if password != "" {
					r.Password = password
				}
				if port > 0 {
					r.Port = port
				}
				if vpnIP != "" {
					r.VPNIP = vpnIP
				}
				now := time.Now()
				r.LastSeen = &now
				r.IsActive = true
				s.repo.UpdateRouter(&r)
				return &r, false, nil // existing
			}
		}
	}
	// Belum ada -> buat baru
	if port == 0 {
		port = 8728
	}
	r := &domain.Router{
		TenantID: tenantID,
		Name:     name,
		Host:     host,
		Port:     port,
		Username: username,
		Password: password,
		VPNIP:    vpnIP,
		IsActive: true,
	}
	now := time.Now()
	r.LastSeen = &now
	if err := s.repo.CreateRouter(r); err != nil {
		return nil, false, err
	}
	return r, true, nil // created
}

// TestConnection checks TCP connectivity to router API port
func (s *Service) TestConnection(host string, port int) bool {
	timeout := 5 * time.Second
	conn, err := net.DialTimeout("tcp", domain.RouterAddr(host, port), timeout)
	if err != nil {
		return false
	}
	defer conn.Close()
	return true
}

// MarkSeen updates LastSeen timestamp
func (s *Service) MarkSeen(tenantID, id uint) error {
	r, err := s.Get(tenantID, id)
	if err != nil {
		return err
	}
	now := time.Now()
	r.LastSeen = &now
	return s.repo.UpdateRouter(r)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [12]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
