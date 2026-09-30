package vpn

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"onobill/internal/domain"
	"onobill/internal/repository"
)

type Service struct {
	repo *repository.Repository
}

func NewService(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

var ErrNotFound = errors.New("vpn peer not found")

// CreatePeer generates a WireGuard peer config
func (s *Service) CreatePeer(tenantID uint, routerID *uint, name, endpoint string) (*domain.VPNPeer, error) {
	// Generate WireGuard keypair (simplified - real impl uses curve25519)
	privKey, pubKey := generateKeyPair()

	// Allocate IP
	count, _ := s.countPeers(tenantID)
	allowedIP := fmt.Sprintf("10.250.0.%d/32", 10+count)

	peer := &domain.VPNPeer{
		TenantID:   tenantID,
		RouterID:   routerID,
		Name:       name,
		PublicKey:  pubKey,
		PrivateKey: privKey,
		AllowedIP:  allowedIP,
		Endpoint:   endpoint,
		Status:     "active",
	}
	if err := s.repo.CreateVPNPeer(peer); err != nil {
		return nil, err
	}
	return peer, nil
}

func (s *Service) countPeers(tenantID uint) (int, error) {
	peers, err := s.repo.ListVPNPeers(tenantID)
	return len(peers), err
}

func (s *Service) Get(tenantID, id uint) (*domain.VPNPeer, error) {
	p, err := s.repo.GetVPNPeerByID(tenantID, id)
	if err != nil {
		if repository.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p, nil
}

func (s *Service) List(tenantID uint) ([]domain.VPNPeer, error) {
	return s.repo.ListVPNPeers(tenantID)
}

func (s *Service) Delete(tenantID, id uint) error {
	if _, err := s.Get(tenantID, id); err != nil {
		return err
	}
	return s.repo.DeleteVPNPeer(tenantID, id)
}

// GenerateMikrotikScript returns RouterOS script to configure WireGuard
func (s *Service) GenerateMikrotikScript(peer *domain.VPNPeer, serverPubKey, serverEndpoint string) string {
	return fmt.Sprintf(`# WireGuard config for %s
/interface wireguard add name=wg-onobill private-key="%s" listen-port=13231
/interface wireguard peers add interface=wg-onobill public-key="%s" endpoint-address=%s endpoint-port=51820 allowed-address=%s persistent-keepalive=25s
/ip address add address=%s interface=wg-onobill
`,
		peer.Name,
		peer.PrivateKey,
		serverPubKey,
		serverEndpoint,
		peer.AllowedIP,
		peer.AllowedIP,
	)
}

// generateKeyPair creates random keys (placeholder - real WireGuard uses curve25519)
func generateKeyPair() (priv, pub string) {
	p := make([]byte, 32)
	rand.Read(p)
	priv = base64.StdEncoding.EncodeToString(p)
	// In real implementation, derive pub from priv using curve25519
	// For now, generate another random one
	q := make([]byte, 32)
	rand.Read(q)
	pub = base64.StdEncoding.EncodeToString(q)
	return priv, pub
}
