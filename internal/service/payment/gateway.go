// Package payment menangani MULTI payment gateway (Duitku, KlikQRIS).
// Setiap gateway mengimplementasikan interface Gateway, sehingga mudah ditambah.
// Pelanggan bayar via QRIS -> webhook -> invoice otomatis lunas -> pelanggan auto-aktif.
package payment

import (
	"errors"
)

// TxRequest parameter pembuatan transaksi.
type TxRequest struct {
	OrderID       string
	Amount        int64  // Rupiah (integer)
	Method        string // "SQ"/"QRIS" untuk QRIS; kosong = default gateway
	ProductDetail string
	Email         string
	Phone         string
	CustomerName  string
}

// TxResult hasil pembuatan transaksi.
type TxResult struct {
	PaymentURL string
	Reference  string
	QRString   string // string QRIS bila ada (untuk ditampilkan sebagai QR)
	Raw        map[string]any
}

// CallbackPayload data webhook yang dinormalisasi lintas-gateway.
type CallbackPayload struct {
	OrderID   string
	Reference string
	Amount    int64
	Paid      bool
	Raw       map[string]any
}

// Gateway kontrak sebuah payment gateway.
type Gateway interface {
	Name() string
	// CreateTransaction membuat transaksi & mengembalikan URL/QR pembayaran.
	CreateTransaction(req TxRequest) (*TxResult, error)
	// ParseCallback memvalidasi & menormalkan webhook. Mengembalikan error bila signature tidak valid.
	ParseCallback(body []byte, headers map[string]string) (*CallbackPayload, error)
}

var ErrInvalidSignature = errors.New("invalid payment callback signature")

// Registry menampung semua gateway aktif, dipilih per-invoice.
type Registry struct {
	gw map[string]Gateway
}

func NewRegistry() *Registry { return &Registry{gw: map[string]Gateway{}} }

func (r *Registry) Register(g Gateway) {
	if g != nil {
		r.gw[g.Name()] = g
	}
}

// Unregister menghapus gateway dari registry (mis. saat dinonaktifkan).
func (r *Registry) Unregister(name string) {
	delete(r.gw, name)
}

// Upsert menimpa/menambah gateway (register baru dengan konfigurasi terbaru).
func (r *Registry) Upsert(g Gateway) {
	r.Register(g)
}

func (r *Registry) Get(name string) (Gateway, bool) {
	g, ok := r.gw[name]
	return g, ok
}

func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.gw))
	for n := range r.gw {
		out = append(out, n)
	}
	return out
}
