package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"onobill/internal/domain"
	"time"

	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) DB() *gorm.DB { return r.db }

// ============ TENANT ============

func (r *Repository) CreateTenant(t *domain.Tenant) error {
	return r.db.Create(t).Error
}

// onboardToken sementara disimpan di Tenant.Slug prefix? Tidak — pakai tabel terpisah ringan.
// Untuk kesederhanaan, simpan token onboarding di memori-DB via kolom terpisah di bawah.
func (r *Repository) EnsureOnboardToken(tenantID uint) string {
	var t domain.Tenant
	if err := r.db.First(&t, tenantID).Error; err != nil {
		return ""
	}
	// Token deterministik per tenant: gunakan kolom Slug + ID sebagai seed sederhana.
	// Untuk produksi: tabel onboarding_tokens terpisah. Di sini cukup unik & stabil.
	token := tenantOnboardToken(t.ID, t.Slug)
	return token
}

func (r *Repository) TenantByOnboardToken(token string) (uint, bool) {
	var tenants []domain.Tenant
	if err := r.db.Find(&tenants).Error; err != nil {
		return 0, false
	}
	for _, t := range tenants {
		if tenantOnboardToken(t.ID, t.Slug) == token {
			return t.ID, true
		}
	}
	return 0, false
}

func (r *Repository) GetTenantByID(id uint) (*domain.Tenant, error) {
	var t domain.Tenant
	if err := r.db.First(&t, id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *Repository) ListTenants() ([]domain.Tenant, error) {
	var list []domain.Tenant
	err := r.db.Order("created_at DESC").Find(&list).Error
	return list, err
}

// SearchTenants mencari tenant berdasarkan keyword (nama/slug) + filter status, dengan pagination.
// Aman untuk ribuan tenant: tidak memuat semua baris sekaligus.
func (r *Repository) SearchTenants(keyword, status string, page, perPage int) ([]domain.Tenant, int64, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 200 {
		perPage = 50
	}
	q := r.db.Model(&domain.Tenant{})
	if keyword != "" {
		like := "%" + keyword + "%"
		q = q.Where("name LIKE ? OR slug LIKE ?", like, like)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []domain.Tenant
	offset := (page - 1) * perPage
	err := q.Order("created_at DESC").Limit(perPage).Offset(offset).Find(&list).Error
	return list, total, err
}

// CountTenantsByStatus menghitung jumlah tenant per status (untuk badge filter).
func (r *Repository) CountTenantsByStatus() (map[string]int64, error) {
	rows := []struct {
		Status string
		N      int64
	}{}
	if err := r.db.Model(&domain.Tenant{}).Select("status, COUNT(*) as n").Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, row := range rows {
		out[row.Status] = row.N
	}
	return out, nil
}

func (r *Repository) UpdateTenant(t *domain.Tenant) error {
	return r.db.Save(t).Error
}

func (r *Repository) DeleteTenant(id uint) error {
	return r.db.Delete(&domain.Tenant{}, id).Error
}

// ============ USER ============

func (r *Repository) CreateUser(u *domain.User) error {
	return r.db.Create(u).Error
}

func (r *Repository) GetUserByEmail(email string) (*domain.User, error) {
	var u domain.User
	if err := r.db.Where("email = ?", email).First(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *Repository) GetUserByID(id uint) (*domain.User, error) {
	var u domain.User
	if err := r.db.Preload("Tenant").First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *Repository) ListUsers(tenantID uint) ([]domain.User, error) {
	var list []domain.User
	err := r.db.Where("tenant_id = ?", tenantID).Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *Repository) UpdateUser(u *domain.User) error {
	return r.db.Save(u).Error
}

func (r *Repository) DeleteUser(id uint) error {
	return r.db.Delete(&domain.User{}, id).Error
}

// ============ ROUTER ============

func (r *Repository) CreateRouter(rt *domain.Router) error {
	return r.db.Create(rt).Error
}

func (r *Repository) GetRouterByID(tenantID, id uint) (*domain.Router, error) {
	var rt domain.Router
	if err := r.db.Where("tenant_id = ? AND id = ?", tenantID, id).First(&rt).Error; err != nil {
		return nil, err
	}
	return &rt, nil
}

func (r *Repository) ListRouters(tenantID uint) ([]domain.Router, error) {
	var list []domain.Router
	err := r.db.Where("tenant_id = ?", tenantID).Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *Repository) UpdateRouter(rt *domain.Router) error {
	return r.db.Save(rt).Error
}

func (r *Repository) DeleteRouter(tenantID, id uint) error {
	return r.db.Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&domain.Router{}).Error
}

// ============ PACKAGE ============

func (r *Repository) CreatePackage(p *domain.Package) error {
	return r.db.Create(p).Error
}

func (r *Repository) GetPackageByID(tenantID, id uint) (*domain.Package, error) {
	var p domain.Package
	if err := r.db.Where("tenant_id = ? AND id = ?", tenantID, id).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) ListPackages(tenantID uint) ([]domain.Package, error) {
	var list []domain.Package
	err := r.db.Where("tenant_id = ?", tenantID).Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *Repository) UpdatePackage(p *domain.Package) error {
	return r.db.Save(p).Error
}

func (r *Repository) DeletePackage(tenantID, id uint) error {
	return r.db.Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&domain.Package{}).Error
}

// ============ CUSTOMER ============

func (r *Repository) CreateCustomer(c *domain.Customer) error {
	return r.db.Create(c).Error
}

func (r *Repository) GetCustomerByID(tenantID, id uint) (*domain.Customer, error) {
	var c domain.Customer
	if err := r.db.Preload("Router").Preload("Package").
		Where("tenant_id = ? AND id = ?", tenantID, id).First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) ListCustomers(tenantID uint, status string) ([]domain.Customer, error) {
	var list []domain.Customer
	q := r.db.Preload("Router").Preload("Package").Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *Repository) SearchCustomers(tenantID uint, keyword string) ([]domain.Customer, error) {
	var list []domain.Customer
	kw := "%" + keyword + "%"
	err := r.db.Preload("Router").Preload("Package").
		Where("tenant_id = ? AND (name LIKE ? OR code LIKE ? OR username LIKE ? OR phone LIKE ?)",
			tenantID, kw, kw, kw, kw).
		Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *Repository) UpdateCustomer(c *domain.Customer) error {
	return r.db.Save(c).Error
}

func (r *Repository) DeleteCustomer(tenantID, id uint) error {
	return r.db.Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&domain.Customer{}).Error
}

func (r *Repository) CountCustomers(tenantID uint) (int64, error) {
	var n int64
	err := r.db.Model(&domain.Customer{}).Where("tenant_id = ?", tenantID).Count(&n).Error
	return n, err
}

func (r *Repository) CountCustomersByStatus(tenantID uint, status string) (int64, error) {
	var n int64
	err := r.db.Model(&domain.Customer{}).Where("tenant_id = ? AND status = ?", tenantID, status).Count(&n).Error
	return n, err
}

// ============ INVOICE ============

func (r *Repository) CreateInvoice(inv *domain.Invoice) error {
	return r.db.Create(inv).Error
}

func (r *Repository) GetInvoiceByID(tenantID, id uint) (*domain.Invoice, error) {
	var inv domain.Invoice
	if err := r.db.Preload("Customer").Preload("Payments").
		Where("tenant_id = ? AND id = ?", tenantID, id).First(&inv).Error; err != nil {
		return nil, err
	}
	return &inv, nil
}

func (r *Repository) GetInvoiceByNumber(number string) (*domain.Invoice, error) {
	var inv domain.Invoice
	if err := r.db.Preload("Customer").Where("number = ?", number).First(&inv).Error; err != nil {
		return nil, err
	}
	return &inv, nil
}

func (r *Repository) ListInvoices(tenantID uint, status string) ([]domain.Invoice, error) {
	var list []domain.Invoice
	q := r.db.Preload("Customer").Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Order("created_at DESC").Find(&list).Error
	return list, err
}

// ListInvoicesByCustomer mengambil semua invoice milik satu pelanggan (untuk portal).
func (r *Repository) ListInvoicesByCustomer(tenantID, customerID uint) ([]domain.Invoice, error) {
	var list []domain.Invoice
	err := r.db.Where("tenant_id = ? AND customer_id = ?", tenantID, customerID).
		Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *Repository) UpdateInvoice(inv *domain.Invoice) error {
	return r.db.Save(inv).Error
}

func (r *Repository) DeleteInvoice(tenantID, id uint) error {
	return r.db.Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&domain.Invoice{}).Error
}

func (r *Repository) SumRevenue(tenantID uint) (float64, error) {
	var total float64
	err := r.db.Model(&domain.Invoice{}).
		Where("tenant_id = ? AND status = ?", tenantID, "paid").
		Select("COALESCE(SUM(total), 0)").Scan(&total).Error
	return total, err
}

func (r *Repository) CountInvoices(tenantID uint, status string) (int64, error) {
	var n int64
	q := r.db.Model(&domain.Invoice{}).Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Count(&n).Error
	return n, err
}

// CountOpenInvoicesByCustomer menghitung invoice terbuka (unpaid/overdue) milik pelanggan.
// Dipakai worker recurring agar tidak membuat invoice ganda.
func (r *Repository) CountOpenInvoicesByCustomer(tenantID, customerID uint) (int64, error) {
	var n int64
	err := r.db.Model(&domain.Invoice{}).
		Where("tenant_id = ? AND customer_id = ? AND status IN ?", tenantID, customerID, []string{"unpaid", "overdue"}).
		Count(&n).Error
	return n, err
}

// ============ PAYMENT ============

func (r *Repository) CreatePayment(p *domain.Payment) error {
	return r.db.Create(p).Error
}

func (r *Repository) ListPaymentsByInvoice(tenantID, invoiceID uint) ([]domain.Payment, error) {
	var list []domain.Payment
	err := r.db.Where("tenant_id = ? AND invoice_id = ?", tenantID, invoiceID).
		Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *Repository) ListPayments(tenantID uint, limit int) ([]domain.Payment, error) {
	var list []domain.Payment
	q := r.db.Preload("Invoice").Preload("Invoice.Customer").
		Where("tenant_id = ?", tenantID).Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&list).Error
	return list, err
}

// ============ VOUCHER ============

func (r *Repository) CreateVoucher(v *domain.Voucher) error {
	return r.db.Create(v).Error
}

func (r *Repository) GetVoucherByCode(code string) (*domain.Voucher, error) {
	var v domain.Voucher
	if err := r.db.Where("code = ?", code).First(&v).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *Repository) ListVouchers(tenantID uint, status string) ([]domain.Voucher, error) {
	var list []domain.Voucher
	q := r.db.Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	err := q.Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *Repository) UpdateVoucher(v *domain.Voucher) error {
	return r.db.Save(v).Error
}

func (r *Repository) DeleteVoucher(tenantID, id uint) error {
	return r.db.Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&domain.Voucher{}).Error
}

// ============ VPN PEER ============

func (r *Repository) CreateVPNPeer(p *domain.VPNPeer) error {
	return r.db.Create(p).Error
}

func (r *Repository) GetVPNPeerByID(tenantID, id uint) (*domain.VPNPeer, error) {
	var p domain.VPNPeer
	if err := r.db.Preload("Router").Where("tenant_id = ? AND id = ?", tenantID, id).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) ListVPNPeers(tenantID uint) ([]domain.VPNPeer, error) {
	var list []domain.VPNPeer
	err := r.db.Preload("Router").Where("tenant_id = ?", tenantID).
		Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *Repository) UpdateVPNPeer(p *domain.VPNPeer) error {
	return r.db.Save(p).Error
}

func (r *Repository) DeleteVPNPeer(tenantID, id uint) error {
	return r.db.Where("tenant_id = ? AND id = ?", tenantID, id).Delete(&domain.VPNPeer{}).Error
}

// ============ AUDIT LOG ============

func (r *Repository) CreateAuditLog(log *domain.AuditLog) error {
	return r.db.Create(log).Error
}

func (r *Repository) ListAuditLogs(tenantID uint, limit int) ([]domain.AuditLog, error) {
	var list []domain.AuditLog
	q := r.db.Where("tenant_id = ?", tenantID).Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&list).Error
	return list, err
}

// ============ JOB ============

func (r *Repository) CreateJob(j *domain.Job) error {
	return r.db.Create(j).Error
}

func (r *Repository) GetPendingJobs(limit int) ([]domain.Job, error) {
	var list []domain.Job
	err := r.db.Where("status = ?", "pending").Order("run_at ASC").Limit(limit).Find(&list).Error
	return list, err
}

func (r *Repository) UpdateJob(j *domain.Job) error {
	return r.db.Save(j).Error
}

// ============ COMMON ============

// ============ NOTIFICATIONS ============

// GetNotifSetting returns the tenant's notification settings, or (nil, nil) if not configured yet.
func (r *Repository) GetNotifSetting(tenantID uint) (*domain.NotifSetting, error) {
	var s domain.NotifSetting
	if err := r.db.Where("tenant_id = ?", tenantID).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// UpsertNotifSetting creates or updates the tenant's notification settings.
func (r *Repository) UpsertNotifSetting(s *domain.NotifSetting) error {
	var existing domain.NotifSetting
	err := r.db.Where("tenant_id = ?", s.TenantID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.Create(s).Error
	}
	if err != nil {
		return err
	}
	s.ID = existing.ID
	s.CreatedAt = existing.CreatedAt
	return r.db.Save(s).Error
}

// CreateNotifLog records a notification attempt.
func (r *Repository) CreateNotifLog(l *domain.NotifLog) error {
	return r.db.Create(l).Error
}

// GetPaymentGatewaySetting mengambil konfigurasi payment gateway tenant.
// Mengembalikan (nil, nil) bila belum ada.
func (r *Repository) GetPaymentGatewaySetting(tenantID uint) (*domain.PaymentGatewaySetting, error) {
	var s domain.PaymentGatewaySetting
	if err := r.db.Where("tenant_id = ?", tenantID).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// UpsertPaymentGatewaySetting membuat atau memperbarui konfigurasi gateway tenant.
func (r *Repository) UpsertPaymentGatewaySetting(s *domain.PaymentGatewaySetting) error {
	var existing domain.PaymentGatewaySetting
	err := r.db.Where("tenant_id = ?", s.TenantID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.Create(s).Error
	}
	if err != nil {
		return err
	}
	s.ID = existing.ID
	s.CreatedAt = existing.CreatedAt
	return r.db.Save(s).Error
}

// ListNotifLogs returns the most recent notification logs for a tenant.
func (r *Repository) ListNotifLogs(tenantID uint, limit int) ([]domain.NotifLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var logs []domain.NotifLog
	err := r.db.Where("tenant_id = ?", tenantID).
		Order("id DESC").Limit(limit).Find(&logs).Error
	return logs, err
}

// HasNotifLog checks whether a log entry already exists (dedup for scheduled reminders).
func (r *Repository) HasNotifLog(tenantID uint, customerID uint, event, channel string, since time.Time) (bool, error) {
	var count int64
	err := r.db.Model(&domain.NotifLog{}).
		Where("tenant_id = ? AND customer_id = ? AND event = ? AND channel = ? AND created_at >= ?",
			tenantID, customerID, event, channel, since).
		Count(&count).Error
	return count > 0, err
}

// ListDueSoonInvoices returns unpaid invoices due exactly `days` days from today (per tenant, 0 = all tenants).
func (r *Repository) ListDueSoonInvoices(tenantID uint, days int) ([]domain.Invoice, error) {
	target := time.Now().AddDate(0, 0, days)
	start := time.Date(target.Year(), target.Month(), target.Day(), 0, 0, 0, 0, target.Location())
	end := start.Add(24 * time.Hour)
	q := r.db.Where("status = ? AND due_date >= ? AND due_date < ?", "unpaid", start, end)
	if tenantID > 0 {
		q = q.Where("tenant_id = ?", tenantID)
	}
	var invoices []domain.Invoice
	err := q.Find(&invoices).Error
	return invoices, err
}

// ListOverdueInvoices returns unpaid invoices past their due date.
func (r *Repository) ListOverdueInvoices(tenantID uint) ([]domain.Invoice, error) {
	q := r.db.Where("status = ? AND due_date < ?", "unpaid", time.Now())
	if tenantID > 0 {
		q = q.Where("tenant_id = ?", tenantID)
	}
	var invoices []domain.Invoice
	err := q.Find(&invoices).Error
	return invoices, err
}

var ErrNotFound = errors.New("record not found")

func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

// tenantOnboardToken membuat token onboarding stabil per tenant (sha256, 24 hex chars).
// Catatan: token ini rahasia-per-tenant; untuk produksi, simpan di tabel terpisah + rotasi.
func tenantOnboardToken(tenantID uint, slug string) string {
	sum := sha256.Sum256([]byte("onobill-onboard:" + slug + ":" + itoa(tenantID)))
	return hex.EncodeToString(sum[:])[:24]
}

func itoa(u uint) string {
	if u == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for u > 0 {
		i--
		b[i] = byte('0' + u%10)
		u /= 10
	}
	return string(b[i:])
}

// ============ CHR (HUB KOMUNIKASI) ============

func (r *Repository) ListCHR() ([]domain.CHRServer, error) {
	var list []domain.CHRServer
	err := r.db.Order("created_at ASC").Find(&list).Error
	return list, err
}

func (r *Repository) CreateCHR(c *domain.CHRServer) error {
	return r.db.Create(c).Error
}

func (r *Repository) GetCHRByID(id uint) (*domain.CHRServer, error) {
	var c domain.CHRServer
	if err := r.db.First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) UpdateCHR(c *domain.CHRServer) error {
	return r.db.Save(c).Error
}

func (r *Repository) DeleteCHR(id uint) error {
	return r.db.Delete(&domain.CHRServer{}, id).Error
}

// ActiveCHR mengembalikan CHR aktif pertama (dipakai provisioning untuk script L2TP).
func (r *Repository) ActiveCHR() (*domain.CHRServer, error) {
	var c domain.CHRServer
	if err := r.db.Where("is_active=?", true).Order("created_at ASC").First(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// ListTenantAdmins mengembalikan satu user admin (role admin/superadmin) per tenant —
// dipakai untuk broadcast platform & notifikasi tenant baru.
func (r *Repository) ListTenantAdmins() ([]domain.User, error) {
	var users []domain.User
	err := r.db.Where("tenant_id > 0 AND role IN ?", []string{"admin", "superadmin"}).
		Group("tenant_id").
		Find(&users).Error
	return users, err
}

// ListUsersByTenant mengembalikan semua user dalam satu tenant.
func (r *Repository) ListUsersByTenant(tenantID uint) ([]domain.User, error) {
	var users []domain.User
	err := r.db.Where("tenant_id = ?", tenantID).Find(&users).Error
	return users, err
}

func (r *Repository) GetVoucher(tenantID, id uint) (*domain.Voucher, error) {
	var v domain.Voucher
	if err := r.db.Where("tenant_id = ? AND id = ?", tenantID, id).First(&v).Error; err != nil {
		return nil, err
	}
	return &v, nil
}
