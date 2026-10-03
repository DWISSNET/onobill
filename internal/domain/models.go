package domain

import (
	"net"
	"strconv"
	"strings"
	"time"
)

// RouterAddr membangun "host:port" yang valid untuk koneksi API MikroTik.
// Aturan:
//   - pakai VPNIP bila terisi (router dijangkau lewat VPN), else Host
//   - bila host sudah mengandung port (mis "1.2.3.4:8291"), pakai apa adanya
//   - bila belum, gabungkan dengan Port (default 8728 bila 0)
func RouterAddr(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host // sudah ada port eksplisit
	}
	if port == 0 {
		port = 8728
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// APIAddr mengembalikan alamat dial router: prioritas VPNIP bila ada.
func (r *Router) APIAddr() string {
	host := r.Host
	if strings.TrimSpace(r.VPNIP) != "" {
		host = r.VPNIP
	}
	return RouterAddr(host, r.Port)
}

// ============ TENANT & USER ============

type Tenant struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:255;not null" json:"name"`
	Slug      string    `gorm:"size:100;uniqueIndex" json:"slug"`
	Status    string    `gorm:"size:20;default:'active'" json:"status"` // active | grace | suspended
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Subscription langganan tenant ke platform ONOBILL (sumber uang superadmin).
type Subscription struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	TenantID   uint       `gorm:"index;not null" json:"tenant_id"`
	Plan       string     `gorm:"size:50;default:'basic'" json:"plan"`    // basic | pro | enterprise
	Amount     float64    `json:"amount"`                                 // biaya langganan per periode
	Status     string     `gorm:"size:20;default:'active'" json:"status"` // active | grace | suspended
	CurrentEnd time.Time  `json:"current_end"`                            // akhir periode berjalan
	GraceUntil *time.Time `json:"grace_until,omitempty"`                  // batas grace period (H+7)
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type User struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	TenantID     uint      `gorm:"index;not null" json:"tenant_id"`
	Email        string    `gorm:"size:255;uniqueIndex;not null" json:"email"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	Name         string    `gorm:"size:255" json:"name"`
	Phone        string    `gorm:"size:32" json:"phone"`                // nomor WA admin (untuk notifikasi & broadcast platform)
	Role         string    `gorm:"size:50;default:'admin'" json:"role"` // superadmin, admin, operator
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Tenant       *Tenant   `gorm:"foreignKey:TenantID" json:"tenant,omitempty"`
}

// ============ ROUTER ============

type Router struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	TenantID  uint       `gorm:"index;not null" json:"tenant_id"`
	Name      string     `gorm:"size:255;not null" json:"name"`
	Host      string     `gorm:"size:255;not null" json:"host"`
	Port      int        `gorm:"default:8728" json:"port"` // API port
	Username  string     `gorm:"size:255" json:"username"`
	Password  string     `gorm:"size:255" json:"password"` // encrypted at rest ideally
	VPNIP     string     `gorm:"size:50" json:"vpn_ip"`    // WireGuard IP if connected via VPN
	IsActive  bool       `gorm:"default:true" json:"is_active"`
	LastSeen  *time.Time `json:"last_seen"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// ============ PACKAGE / PROFILE ============

type Package struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	TenantID    uint      `gorm:"index;not null" json:"tenant_id"`
	Name        string    `gorm:"size:255;not null" json:"name"`
	Type        string    `gorm:"size:50;not null" json:"type"` // pppoe, hotspot
	Price       float64   `gorm:"not null" json:"price"`
	SpeedDown   int       `json:"speed_down"`                 // Mbps
	SpeedUp     int       `json:"speed_up"`                   // Mbps
	Duration    int       `gorm:"default:30" json:"duration"` // days
	Description string    `gorm:"type:text" json:"description"`
	IsActive    bool      `gorm:"default:true" json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ============ CUSTOMER ============

type Customer struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	TenantID    uint       `gorm:"index;not null" json:"tenant_id"`
	RouterID    *uint      `gorm:"index" json:"router_id"`
	PackageID   *uint      `gorm:"index" json:"package_id"`
	Code        string     `gorm:"size:50;index" json:"code"` // unique customer code
	Name        string     `gorm:"size:255;not null" json:"name"`
	Email       string     `gorm:"size:255" json:"email"`
	Phone       string     `gorm:"size:50" json:"phone"`
	Address     string     `gorm:"type:text" json:"address"`
	Username    string     `gorm:"size:255;index" json:"username"`              // PPPoE/Hotspot username
	Password    string     `gorm:"size:255" json:"password"`                    // PPPoE password
	ServiceType string     `gorm:"size:50;default:'pppoe'" json:"service_type"` // pppoe, hotspot
	Status      string     `gorm:"size:50;default:'active'" json:"status"`      // active, isolated, suspended, terminated
	DueDate     *time.Time `json:"due_date"`                                    // billing due date
	JoinedAt    time.Time  `json:"joined_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Router      *Router    `gorm:"foreignKey:RouterID" json:"router,omitempty"`
	Package     *Package   `gorm:"foreignKey:PackageID" json:"package,omitempty"`
}

// ============ BILLING ============

type Invoice struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	TenantID    uint       `gorm:"index;not null" json:"tenant_id"`
	CustomerID  uint       `gorm:"index;not null" json:"customer_id"`
	Number      string     `gorm:"size:100;uniqueIndex" json:"number"` // INV-20250920-0001
	Amount      float64    `gorm:"not null" json:"amount"`
	Tax         float64    `gorm:"default:0" json:"tax"`
	Total       float64    `gorm:"not null" json:"total"`
	Status      string     `gorm:"size:50;default:'unpaid'" json:"status"` // unpaid, paid, overdue, cancelled
	IssuedAt    time.Time  `json:"issued_at"`
	DueAt       time.Time  `json:"due_at"`
	PaidAt      *time.Time `json:"paid_at"`
	PeriodStart *time.Time `json:"period_start"`
	PeriodEnd   *time.Time `json:"period_end"`
	Notes       string     `gorm:"type:text" json:"notes"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Customer    *Customer  `gorm:"foreignKey:CustomerID" json:"customer,omitempty"`
	Payments    []Payment  `gorm:"foreignKey:InvoiceID" json:"payments,omitempty"`
}

type Payment struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TenantID  uint      `gorm:"index;not null" json:"tenant_id"`
	InvoiceID uint      `gorm:"index;not null" json:"invoice_id"`
	Amount    float64   `gorm:"not null" json:"amount"`
	Method    string    `gorm:"size:50" json:"method"`     // cash, transfer, gateway
	Reference string    `gorm:"size:255" json:"reference"` // transaction ref
	PaidAt    time.Time `json:"paid_at"`
	CreatedAt time.Time `json:"created_at"`
	Invoice   *Invoice  `gorm:"foreignKey:InvoiceID" json:"invoice,omitempty"`
}

// ============ HOTSPOT VOUCHER ============

type Voucher struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	TenantID  uint       `gorm:"index;not null" json:"tenant_id"`
	RouterID  *uint      `gorm:"index" json:"router_id"`
	PackageID *uint      `gorm:"index" json:"package_id"`
	Code      string     `gorm:"size:50;uniqueIndex" json:"code"`
	Password  string     `gorm:"size:50" json:"password"`
	Profile   string     `gorm:"size:100" json:"profile"`
	Status    string     `gorm:"size:50;default:'unused'" json:"status"` // unused, active, expired, disabled
	UsedAt    *time.Time `json:"used_at"`
	ExpiredAt *time.Time `json:"expired_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// ============ VPN PEER ============

type VPNPeer struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	TenantID      uint       `gorm:"index;not null" json:"tenant_id"`
	RouterID      *uint      `gorm:"index" json:"router_id"`
	Name          string     `gorm:"size:255" json:"name"`
	PublicKey     string     `gorm:"size:255" json:"public_key"`
	PrivateKey    string     `gorm:"size:255" json:"private_key"` // only stored for generated peers
	AllowedIP     string     `gorm:"size:100" json:"allowed_ip"`
	Endpoint      string     `gorm:"size:255" json:"endpoint"`
	Status        string     `gorm:"size:50;default:'active'" json:"status"`
	LastHandshake *time.Time `json:"last_handshake"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	Router        *Router    `gorm:"foreignKey:RouterID" json:"router,omitempty"`
}

// ============ CHR (HUB KOMUNIKASI) ============

// CHRServer = pusat jalur komunikasi L2TP antara ONOBILL dan router tenant.
// Semua router tenant "menelepon" (dial L2TP) ke CHR; ONOBILL menghubungi router lewat tunnel ini.
type CHRServer struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:255;not null" json:"name"` // label, mis: "CHR-Jakarta-1"
	Host      string    `gorm:"size:255;not null" json:"host"` // IP/domain CHR
	APIPort   int       `gorm:"default:8728" json:"api_port"`
	Username  string    `gorm:"size:255" json:"username"`
	Password  string    `gorm:"size:255" json:"password"`
	L2TPRange string    `gorm:"size:64" json:"l2tp_range"` // pool IP tunnel, mis: 10.10.0.0/24
	Secret    string    `gorm:"size:255" json:"secret"`    // L2TP/IPsec secret
	IsActive  bool      `gorm:"default:true" json:"is_active"`
	Note      string    `gorm:"size:255" json:"note"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ============ AUDIT LOG ============

type AuditLog struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	TenantID   uint      `gorm:"index" json:"tenant_id"`
	UserID     *uint     `gorm:"index" json:"user_id"`
	Action     string    `gorm:"size:100;index" json:"action"` // create, update, delete, login, etc
	Resource   string    `gorm:"size:100" json:"resource"`     // customer, invoice, router, etc
	ResourceID uint      `json:"resource_id"`
	Detail     string    `gorm:"type:text" json:"detail"` // JSON detail
	IP         string    `gorm:"size:50" json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
}

// ============ NOTIFICATIONS ============

// NotifSetting holds per-tenant notification channel configuration.
// Exactly one row per tenant (TenantID unique).
type NotifSetting struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	TenantID uint `gorm:"uniqueIndex;not null" json:"tenant_id"`

	// Master switches
	WAEnabled   bool `gorm:"default:false" json:"wa_enabled"`
	TGEnabled   bool `gorm:"default:false" json:"tg_enabled"`
	SMTPEnabled bool `gorm:"default:false" json:"smtp_enabled"`

	// WhatsApp gateway (Fonnte-compatible: POST {target,message} + Authorization header)
	WAProvider string `gorm:"size:50;default:'fonnte'" json:"wa_provider"` // fonnte, generic
	WAEndpoint string `gorm:"size:255" json:"wa_endpoint"`                 // default https://api.fonnte.com/send
	WAToken    string `gorm:"size:255" json:"wa_token"`

	// Telegram bot
	TGBotToken string `gorm:"size:255" json:"tg_bot_token"`
	TGChatID   string `gorm:"size:100" json:"tg_chat_id"` // fallback chat id (e.g. admin group)

	// SMTP email
	SMTPHost string `gorm:"size:255" json:"smtp_host"`
	SMTPPort int    `gorm:"default:587" json:"smtp_port"`
	SMTPUser string `gorm:"size:255" json:"smtp_user"`
	SMTPPass string `gorm:"size:255" json:"smtp_pass"`
	SMTPFrom string `gorm:"size:255" json:"smtp_from"`

	// Event toggles
	NotifyNewInvoice   bool `gorm:"default:true" json:"notify_new_invoice"` // invoice created
	NotifyPayment      bool `gorm:"default:true" json:"notify_payment"`     // payment received
	NotifyReminder     bool `gorm:"default:true" json:"notify_reminder"`    // H-3 before due
	NotifyOverdue      bool `gorm:"default:true" json:"notify_overdue"`     // past due date
	ReminderDaysBefore int  `gorm:"default:3" json:"reminder_days_before"`  // days before due date

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PaymentGatewaySetting menyimpan konfigurasi payment gateway per-tenant
// (tenant 0 = platform/superadmin, dipakai untuk pembayaran langganan).
// Mendukung Duitku & KlikQRIS; bisa diatur dari UI Superadmin tanpa restart.
type PaymentGatewaySetting struct {
	ID       uint `gorm:"primaryKey" json:"id"`
	TenantID uint `gorm:"uniqueIndex;not null" json:"tenant_id"`

	// Duitku
	DuitkuEnabled  bool   `gorm:"default:false" json:"duitku_enabled"`
	DuitkuMerchant string `gorm:"size:100" json:"duitku_merchant"` // merchant code
	DuitkuAPIKey   string `gorm:"size:255" json:"duitku_api_key"`
	DuitkuSandbox  bool   `gorm:"default:true" json:"duitku_sandbox"` // true=sandbox, false=production

	// KlikQRIS
	KlikQRISEnabled  bool   `gorm:"default:false" json:"klikqris_enabled"`
	KlikQRISMerchant string `gorm:"size:100" json:"klikqris_merchant"`
	KlikQRISAPIKey   string `gorm:"size:255" json:"klikqris_api_key"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DuitkuBaseURL mengembalikan base URL API Duitku sesuai mode.
func (p *PaymentGatewaySetting) DuitkuBaseURL() string {
	if p.DuitkuSandbox {
		return "https://sandbox.duitku.com/webapi/api/merchant"
	}
	return "https://passport.duitku.com/webapi/api/merchant"
}

// NotifLog records every notification attempt (success or failure).
type NotifLog struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	TenantID   uint       `gorm:"index;not null" json:"tenant_id"`
	CustomerID *uint      `gorm:"index" json:"customer_id"`
	Channel    string     `gorm:"size:20;index" json:"channel"` // whatsapp, telegram, email
	Event      string     `gorm:"size:50;index" json:"event"`   // new_invoice, payment, reminder, overdue, test
	Target     string     `gorm:"size:255" json:"target"`       // phone / chat_id / email
	Message    string     `gorm:"type:text" json:"message"`
	Status     string     `gorm:"size:20;index" json:"status"` // sent, failed, skipped
	Error      string     `gorm:"type:text" json:"error"`
	SentAt     *time.Time `json:"sent_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// ============ PLATFORM WALLET / TOP-UP ============

// Wallet menyimpan saldo prabayar tenant untuk layanan platform.
// Semua nominal disimpan sebagai integer Rupiah, bukan float.
type Wallet struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	TenantID  uint      `gorm:"uniqueIndex;not null" json:"tenant_id"`
	Balance   int64     `gorm:"not null;default:0" json:"balance"`
	Currency  string    `gorm:"size:3;not null;default:'IDR'" json:"currency"`
	Status    string    `gorm:"size:20;not null;default:'active'" json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WalletLedger adalah append-only audit trail perubahan saldo.
type WalletLedger struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	TenantID      uint      `gorm:"index;uniqueIndex:ux_wallet_ledger_reference;not null" json:"tenant_id"`
	WalletID      uint      `gorm:"index;not null" json:"wallet_id"`
	ReferenceType string    `gorm:"size:40;uniqueIndex:ux_wallet_ledger_reference;not null" json:"reference_type"`
	ReferenceID   string    `gorm:"size:120;uniqueIndex:ux_wallet_ledger_reference;not null" json:"reference_id"`
	EntryType     string    `gorm:"size:20;uniqueIndex:ux_wallet_ledger_reference;not null" json:"entry_type"` // credit|debit|reversal
	Amount        int64     `gorm:"not null" json:"amount"`
	BalanceBefore int64     `gorm:"not null" json:"balance_before"`
	BalanceAfter  int64     `gorm:"not null" json:"balance_after"`
	Description   string    `gorm:"size:255" json:"description"`
	CreatedAt     time.Time `json:"created_at"`
}

// TopUp mewakili satu order pembayaran wallet ke Duitku.
type TopUp struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	TenantID         uint       `gorm:"index;not null" json:"tenant_id"`
	OrderID          string     `gorm:"size:100;uniqueIndex;not null" json:"order_id"`
	Gateway          string     `gorm:"size:40;not null;default:'duitku'" json:"gateway"`
	GatewayReference string     `gorm:"size:150;index" json:"gateway_reference"`
	Amount           int64      `gorm:"not null" json:"amount"`
	Status           string     `gorm:"size:20;not null;default:'pending'" json:"status"` // pending|paid|expired|failed
	PaymentURL       string     `gorm:"type:text" json:"payment_url"`
	QRString         string     `gorm:"type:text" json:"qr_string"`
	ExpiresAt        time.Time  `json:"expires_at"`
	PaidAt           *time.Time `json:"paid_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// ============ JOB QUEUE ============

type Job struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	TenantID  uint       `gorm:"index" json:"tenant_id"`
	Type      string     `gorm:"size:100;index" json:"type"`              // sync_router, isolate_customer, send_invoice, etc
	Payload   string     `gorm:"type:text" json:"payload"`                // JSON
	Status    string     `gorm:"size:50;default:'pending'" json:"status"` // pending, running, done, failed
	Error     string     `gorm:"type:text" json:"error"`
	Retries   int        `gorm:"default:0" json:"retries"`
	RunAt     time.Time  `json:"run_at"`
	DoneAt    *time.Time `json:"done_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
