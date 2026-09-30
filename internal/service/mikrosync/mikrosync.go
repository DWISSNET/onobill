// Package mikrosync adalah engine auto-sync yang mendorong (push) data billing
// ONOBILL ke router MikroTik via RouterOS API:
//
//   - PPPoE profile   <- domain.Package  (type=pppoe)   -> /ppp/profile
//   - PPPoE secret    <- domain.Customer (service_type=pppoe) -> /ppp/secret
//   - Hotspot profile <- domain.Package  (type=hotspot) -> /ip/hotspot/user/profile
//   - Hotspot user    <- domain.Customer (service_type=hotspot) -> /ip/hotspot/user
//   - Voucher         <- domain.Voucher  -> /ip/hotspot/user (profile sesuai paket)
//
// Engine berjalan sebagai background worker dengan antrean tugas, retry
// ber-backoff, dan lock per-router agar tidak ada dua sinkronisasi ke router
// yang sama secara bersamaan.
package mikrosync

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/go-routeros/routeros/v3"
	"onobill/internal/domain"
	"onobill/internal/repository"
)

// ---------------------------------------------------------------------------
// Konstanta & tipe publik
// ---------------------------------------------------------------------------

// TaskType menentukan lingkup sinkronisasi.
type TaskType string

const (
	TaskFullSync        TaskType = "full_sync"        // semua entitas ke satu router
	TaskPPPoEProfile    TaskType = "pppoe_profile"    // paket pppoe -> /ppp/profile
	TaskPPPoESecret     TaskType = "pppoe_secret"     // customer pppoe -> /ppp/secret
	TaskHotspotProfile  TaskType = "hotspot_profile"  // paket hotspot -> /ip/hotspot/user/profile
	TaskHotspotUser     TaskType = "hotspot_user"     // customer hotspot -> /ip/hotspot/user
	TaskVoucher         TaskType = "voucher"          // voucher -> /ip/hotspot/user
	TaskRemovePPPoE     TaskType = "remove_pppoe"     // hapus secret (customer terminated/dihapus)
	TaskRemoveHotspot   TaskType = "remove_hotspot"   // hapus user hotspot
	TaskDisableCustomer TaskType = "disable_customer" // disable secret/user (isolir manual via sync)
)

// Task adalah satu unit kerja sinkronisasi.
// RefID menunjuk ke ID entitas sumber (Package/Customer/Voucher) dan hanya
// dipakai oleh task bertipe granular; TaskFullSync mengabaikannya.
type Task struct {
	TenantID uint
	RouterID uint
	Type     TaskType
	RefID    uint
	// Username dipakai task remove_* saat entitas sudah terhapus dari DB.
	Username string
}

// Result merangkum hasil satu siklus sinkronisasi.
type Result struct {
	RouterID uint
	Pushed   int // entri dibuat/diupdate di router
	Removed  int // entri dihapus dari router
	Skipped  int // entri dilewati (sudah sinkron / tidak relevan)
	Failed   int // entri gagal diproses
	Errors   []string
	Started  time.Time
	Duration time.Duration
}

func (r *Result) fail(format string, args ...any) {
	r.Failed++
	r.Errors = append(r.Errors, fmt.Sprintf(format, args...))
}

var (
	ErrRouterNotFound = errors.New("mikrosync: router tidak ditemukan")
	ErrRouterInactive = errors.New("mikrosync: router tidak aktif")
)

// ---------------------------------------------------------------------------
// RouterOS client abstraction (agar mudah di-mock saat testing)
// ---------------------------------------------------------------------------

// rosClient adalah subset API routeros.Client yang dipakai engine.
type rosClient interface {
	Run(sentence ...string) (*routeros.Reply, error)
	RunContext(ctx context.Context, sentence ...string) (*routeros.Reply, error)
	Close() error
}

// dialer membuka koneksi RouterOS API ke router tertentu.
type dialer func(ctx context.Context, r *domain.Router) (rosClient, error)

// ---------------------------------------------------------------------------
// Engine
// ---------------------------------------------------------------------------

// Engine adalah background worker auto-sync.
type Engine struct {
	repo *repository.Repository

	// DialTimeout batas waktu koneksi API ke router.
	DialTimeout time.Duration
	// OpTimeout batas waktu per-perintah RouterOS.
	OpTimeout time.Duration
	// UseTLS memakai API-SSL (8729); default false (plain 8728).
	UseTLS bool
	// MaxRetries jumlah percobaan ulang task yang gagal dial.
	MaxRetries int
	// RetryBaseDelay dasar backoff eksponensial antar retry.
	RetryBaseDelay time.Duration
	// QueueSize kapasitas buffer antrean task.
	QueueSize int
	// Logger tujuan log; default log.Default().
	Logger *log.Logger

	dial dialer

	queues chan queuedTask

	mu      sync.Mutex
	locks   map[uint]*sync.Mutex // lock per router
	started bool

	wg     sync.WaitGroup
	cancel context.CancelFunc
}

type queuedTask struct {
	task    Task
	attempt int
}

// NewEngine membuat engine sync baru dengan default yang masuk akal.
func NewEngine(repo *repository.Repository) *Engine {
	e := &Engine{
		repo:           repo,
		DialTimeout:    10 * time.Second,
		OpTimeout:      15 * time.Second,
		UseTLS:         false,
		MaxRetries:     3,
		RetryBaseDelay: 5 * time.Second,
		QueueSize:      256,
		Logger:         log.Default(),
		locks:          make(map[uint]*sync.Mutex),
	}
	e.dial = e.dialRouterOS
	return e
}

// SetDialer mengganti fungsi dial (untuk unit test).
func (e *Engine) SetDialer(d dialer) { e.dial = d }

// ---------------------------------------------------------------------------
// Lifecycle worker
// ---------------------------------------------------------------------------

// Start menjalankan worker goroutine. Aman dipanggil sekali; panggilan
// berikutnya diabaikan.
func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.started {
		e.mu.Unlock()
		return
	}
	e.started = true
	if e.QueueSize <= 0 {
		e.QueueSize = 256
	}
	e.queues = make(chan queuedTask, e.QueueSize)
	ctx, e.cancel = context.WithCancel(ctx)
	e.mu.Unlock()

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.loop(ctx)
	}()
	e.Logger.Printf("[mikrosync] engine started")
}

// Stop menghentikan worker dan menunggu task yang sedang berjalan selesai.
func (e *Engine) Stop() {
	e.mu.Lock()
	if !e.started {
		e.mu.Unlock()
		return
	}
	cancel := e.cancel
	e.started = false
	e.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	e.wg.Wait()
	e.Logger.Printf("[mikrosync] engine stopped")
}

// Enqueue memasukkan task ke antrean. Mengembalikan false jika antrean penuh
// (non-blocking) — caller dapat memutuskan untuk drop atau mencoba lagi.
func (e *Engine) Enqueue(t Task) bool {
	e.mu.Lock()
	q := e.queues
	e.mu.Unlock()
	if q == nil {
		return false
	}
	select {
	case q <- queuedTask{task: t}:
		return true
	default:
		e.Logger.Printf("[mikrosync] queue full, dropping task type=%s router=%d", t.Type, t.RouterID)
		return false
	}
}

// loop mengonsumsi antrean dan mengeksekusi task dengan retry.
func (e *Engine) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case qt := <-e.queues:
			res, err := e.execute(ctx, qt.task)
			if err != nil && qt.attempt < e.MaxRetries {
				// Backoff eksponensial: base * 2^attempt.
				delay := e.RetryBaseDelay << qt.attempt
				e.Logger.Printf("[mikrosync] task type=%s router=%d gagal (attempt %d): %v — retry dalam %s",
					qt.task.Type, qt.task.RouterID, qt.attempt+1, err, delay)
				qt.attempt++
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
				select {
				case e.queues <- qt:
				case <-ctx.Done():
					return
				default:
					e.Logger.Printf("[mikrosync] queue full saat retry, task dropped: type=%s router=%d",
						qt.task.Type, qt.task.RouterID)
				}
				continue
			}
			if err != nil {
				e.Logger.Printf("[mikrosync] task type=%s router=%d gagal permanen: %v",
					qt.task.Type, qt.task.RouterID, err)
			} else if res != nil {
				e.Logger.Printf("[mikrosync] sync router=%d selesai dalam %s: pushed=%d removed=%d skipped=%d failed=%d",
					res.RouterID, res.Duration.Round(time.Millisecond), res.Pushed, res.Removed, res.Skipped, res.Failed)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Eksekusi task
// ---------------------------------------------------------------------------

// execute menjalankan satu task sinkronisasi (diproteksi lock per-router).
func (e *Engine) execute(ctx context.Context, t Task) (*Result, error) {
	router, err := e.repo.GetRouterByID(t.TenantID, t.RouterID)
	if err != nil {
		return nil, fmt.Errorf("get router %d: %w", t.RouterID, err)
	}
	if router == nil {
		return nil, ErrRouterNotFound
	}
	if !router.IsActive {
		return nil, ErrRouterInactive
	}

	// Satu sync per router dalam satu waktu.
	lock := e.routerLock(router.ID)
	lock.Lock()
	defer lock.Unlock()

	dialCtx, cancel := context.WithTimeout(ctx, e.DialTimeout)
	client, err := e.dial(dialCtx, router)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("dial router %s:%d: %w", router.Host, router.Port, err)
	}
	defer client.Close()

	res := &Result{RouterID: router.ID, Started: time.Now()}
	defer func() { res.Duration = time.Since(res.Started) }()

	switch t.Type {
	case TaskFullSync:
		e.syncPackages(ctx, client, t.TenantID, router, res)
		e.syncCustomers(ctx, client, t.TenantID, router, res)
		e.syncVouchers(ctx, client, t.TenantID, router, res)
	case TaskPPPoEProfile, TaskHotspotProfile:
		pkg, err := e.repo.GetPackageByID(t.TenantID, t.RefID)
		if err != nil || pkg == nil {
			res.fail("package %d tidak ditemukan: %v", t.RefID, err)
			return res, nil
		}
		if err := e.pushPackageProfile(ctx, client, pkg); err != nil {
			res.fail("profile %s: %v", pkg.Name, err)
		} else {
			res.Pushed++
		}
	case TaskPPPoESecret, TaskHotspotUser, TaskDisableCustomer:
		cust, err := e.repo.GetCustomerByID(t.TenantID, t.RefID)
		if err != nil || cust == nil {
			res.fail("customer %d tidak ditemukan: %v", t.RefID, err)
			return res, nil
		}
		if t.Type == TaskDisableCustomer {
			err = e.setCustomerDisabled(ctx, client, cust, true)
		} else {
			err = e.pushCustomer(ctx, client, cust)
		}
		if err != nil {
			res.fail("customer %s: %v", cust.Username, err)
		} else {
			res.Pushed++
		}
	case TaskVoucher:
		v, err := e.repo.GetVoucher(t.TenantID, t.RefID)
		if err != nil || v == nil {
			res.fail("voucher %d tidak ditemukan: %v", t.RefID, err)
			return res, nil
		}
		if err := e.pushVoucher(ctx, client, v); err != nil {
			res.fail("voucher %s: %v", v.Code, err)
		} else {
			res.Pushed++
		}
	case TaskRemovePPPoE:
		if err := e.removeByName(ctx, client, "/ppp/secret", t.Username); err != nil {
			res.fail("remove pppoe secret %s: %v", t.Username, err)
		} else {
			res.Removed++
		}
	case TaskRemoveHotspot:
		if err := e.removeByName(ctx, client, "/ip/hotspot/user", t.Username); err != nil {
			res.fail("remove hotspot user %s: %v", t.Username, err)
		} else {
			res.Removed++
		}
	default:
		return nil, fmt.Errorf("unknown task type %q", t.Type)
	}
	return res, nil
}

func (e *Engine) routerLock(routerID uint) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	l, ok := e.locks[routerID]
	if !ok {
		l = &sync.Mutex{}
		e.locks[routerID] = l
	}
	return l
}

// ---------------------------------------------------------------------------
// Full sync: packages -> profiles
// ---------------------------------------------------------------------------

// syncPackages mendorong semua paket aktif tenant sebagai PPPoE/Hotspot profile.
func (e *Engine) syncPackages(ctx context.Context, client rosClient, tenantID uint, router *domain.Router, res *Result) {
	pkgs, err := e.repo.ListPackages(tenantID)
	if err != nil {
		res.fail("list packages: %v", err)
		return
	}
	for i := range pkgs {
		pkg := &pkgs[i]
		if !pkg.IsActive {
			res.Skipped++
			continue
		}
		if err := e.pushPackageProfile(ctx, client, pkg); err != nil {
			res.fail("profile %s: %v", pkg.Name, err)
			continue
		}
		res.Pushed++
	}
}

// pushPackageProfile membuat/meng-update satu profile di router sesuai tipe paket.
func (e *Engine) pushPackageProfile(ctx context.Context, client rosClient, pkg *domain.Package) error {
	rateLimit := formatRateLimit(pkg.SpeedUp, pkg.SpeedDown)
	comment := fmt.Sprintf("onobill:pkg:%d", pkg.ID)
	if pkg.Description != "" {
		comment += " " + truncate(pkg.Description, 180)
	}

	switch strings.ToLower(pkg.Type) {
	case "pppoe":
		args := []string{
			"=name=" + pkg.Name,
			"=comment=" + comment,
		}
		if rateLimit != "" {
			args = append(args, "=rate-limit="+rateLimit)
		}
		return e.upsert(ctx, client, "/ppp/profile", "name", pkg.Name, args)
	case "hotspot":
		args := []string{
			"=name=" + pkg.Name,
			"=comment=" + comment,
		}
		if rateLimit != "" {
			args = append(args, "=rate-limit="+rateLimit)
		}
		return e.upsert(ctx, client, "/ip/hotspot/user/profile", "name", pkg.Name, args)
	default:
		return fmt.Errorf("tipe paket %q tidak dikenal", pkg.Type)
	}
}

// ---------------------------------------------------------------------------
// Full sync: customers -> secrets / hotspot users
// ---------------------------------------------------------------------------

// syncCustomers mendorong semua customer yang terikat ke router ini.
func (e *Engine) syncCustomers(ctx context.Context, client rosClient, tenantID uint, router *domain.Router, res *Result) {
	customers, err := e.repo.ListCustomers(tenantID, "")
	if err != nil {
		res.fail("list customers: %v", err)
		return
	}
	for i := range customers {
		cust := &customers[i]
		if cust.RouterID == nil || *cust.RouterID != router.ID {
			continue
		}
		if cust.Username == "" {
			res.Skipped++
			continue
		}
		switch cust.Status {
		case "terminated":
			// Customer berhenti berlangganan: hapus dari router.
			if err := e.removeCustomer(ctx, client, cust); err != nil {
				res.fail("remove %s: %v", cust.Username, err)
			} else {
				res.Removed++
			}
			continue
		case "isolated", "suspended":
			if err := e.setCustomerDisabled(ctx, client, cust, true); err != nil {
				res.fail("disable %s: %v", cust.Username, err)
			} else {
				res.Pushed++
			}
			continue
		}
		if err := e.pushCustomer(ctx, client, cust); err != nil {
			res.fail("customer %s: %v", cust.Username, err)
			continue
		}
		res.Pushed++
	}
}

// pushCustomer membuat/meng-update PPPoE secret atau hotspot user di router.
func (e *Engine) pushCustomer(ctx context.Context, client rosClient, cust *domain.Customer) error {
	profile := e.customerProfileName(cust)
	comment := fmt.Sprintf("onobill:cust:%d %s", cust.ID, truncate(cust.Name, 160))
	disabled := "no"
	if cust.Status == "isolated" || cust.Status == "suspended" {
		disabled = "yes"
	}

	if strings.ToLower(cust.ServiceType) == "hotspot" {
		args := []string{
			"=name=" + cust.Username,
			"=password=" + cust.Password,
			"=disabled=" + disabled,
			"=comment=" + comment,
		}
		if profile != "" {
			args = append(args, "=profile="+profile)
		}
		return e.upsert(ctx, client, "/ip/hotspot/user", "name", cust.Username, args)
	}

	// default: PPPoE secret
	args := []string{
		"=name=" + cust.Username,
		"=password=" + cust.Password,
		"=service=pppoe",
		"=disabled=" + disabled,
		"=comment=" + comment,
	}
	if profile != "" {
		args = append(args, "=profile="+profile)
	}
	return e.upsert(ctx, client, "/ppp/secret", "name", cust.Username, args)
}

// setCustomerDisabled men-disable/enable entri customer di router.
func (e *Engine) setCustomerDisabled(ctx context.Context, client rosClient, cust *domain.Customer, disabled bool) error {
	menu := "/ppp/secret"
	if strings.ToLower(cust.ServiceType) == "hotspot" {
		menu = "/ip/hotspot/user"
	}
	id, err := e.findID(ctx, client, menu, "name", cust.Username)
	if err != nil {
		return err
	}
	if id == "" {
		// Belum ada di router: push sekalian (status disabled sudah di-set).
		if disabled {
			return e.pushCustomer(ctx, client, cust)
		}
		return nil
	}
	flag := "no"
	if disabled {
		flag = "yes"
	}
	_, err = e.run(ctx, client, menu+"/set", "=.id="+id, "=disabled="+flag)
	return err
}

// removeCustomer menghapus entri customer dari router.
func (e *Engine) removeCustomer(ctx context.Context, client rosClient, cust *domain.Customer) error {
	menu := "/ppp/secret"
	if strings.ToLower(cust.ServiceType) == "hotspot" {
		menu = "/ip/hotspot/user"
	}
	return e.removeByName(ctx, client, menu, cust.Username)
}

// customerProfileName mengambil nama profile dari paket customer (bila ada).
func (e *Engine) customerProfileName(cust *domain.Customer) string {
	if cust.Package != nil && cust.Package.Name != "" {
		return cust.Package.Name
	}
	if cust.PackageID != nil {
		pkg, err := e.repo.GetPackageByID(cust.TenantID, *cust.PackageID)
		if err == nil && pkg != nil {
			return pkg.Name
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Full sync: vouchers -> hotspot users
// ---------------------------------------------------------------------------

// syncVouchers mendorong voucher aktif/belum terpakai sebagai hotspot user.
func (e *Engine) syncVouchers(ctx context.Context, client rosClient, tenantID uint, router *domain.Router, res *Result) {
	vouchers, err := e.repo.ListVouchers(tenantID, "")
	if err != nil {
		res.fail("list vouchers: %v", err)
		return
	}
	for i := range vouchers {
		v := &vouchers[i]
		if v.RouterID == nil || *v.RouterID != router.ID {
			continue
		}
		switch v.Status {
		case "unused", "active":
			if err := e.pushVoucher(ctx, client, v); err != nil {
				res.fail("voucher %s: %v", v.Code, err)
				continue
			}
			res.Pushed++
		case "expired", "disabled":
			if err := e.removeByName(ctx, client, "/ip/hotspot/user", v.Code); err != nil {
				res.fail("remove voucher %s: %v", v.Code, err)
			} else {
				res.Removed++
			}
		default:
			res.Skipped++
		}
	}
}

// pushVoucher membuat/meng-update voucher sebagai hotspot user di router.
// Username = kode voucher; password mengikuti field Password bila diisi.
func (e *Engine) pushVoucher(ctx context.Context, client rosClient, v *domain.Voucher) error {
	password := v.Password
	if password == "" {
		password = v.Code // mode voucher umum: username = password = kode
	}
	args := []string{
		"=name=" + v.Code,
		"=password=" + password,
		"=disabled=no",
		"=comment=" + fmt.Sprintf("onobill:voucher:%d", v.ID),
	}
	profile := v.Profile
	if profile == "" && v.PackageID != nil {
		if pkg, err := e.repo.GetPackageByID(v.TenantID, *v.PackageID); err == nil && pkg != nil {
			profile = pkg.Name
		}
	}
	if profile != "" {
		args = append(args, "=profile="+profile)
	}
	if v.ExpiredAt != nil {
		if d := time.Until(*v.ExpiredAt); d > 0 {
			args = append(args, "=limit-uptime="+formatRouterOSDuration(d))
		}
	}
	return e.upsert(ctx, client, "/ip/hotspot/user", "name", v.Code, args)
}

// ---------------------------------------------------------------------------
// RouterOS helpers
// ---------------------------------------------------------------------------

// run mengeksekusi perintah RouterOS dengan OpTimeout.
func (e *Engine) run(ctx context.Context, client rosClient, sentence ...string) (*routeros.Reply, error) {
	opCtx, cancel := context.WithTimeout(ctx, e.OpTimeout)
	defer cancel()
	return client.RunContext(opCtx, sentence...)
}

// findID mencari .id entri berdasarkan satu properti (exact match).
// Mengembalikan "" (tanpa error) bila tidak ditemukan.
func (e *Engine) findID(ctx context.Context, client rosClient, menu, prop, value string) (string, error) {
	reply, err := e.run(ctx, client, menu+"/print", "?"+prop+"="+value, "=.proplist=.id")
	if err != nil {
		return "", fmt.Errorf("%s print: %w", menu, err)
	}
	if len(reply.Re) == 0 {
		return "", nil
	}
	return reply.Re[0].Map[".id"], nil
}

// upsert membuat entri baru atau meng-update entri yang sudah ada
// (dikenali lewat properti unik, biasanya "name"). Idempotent.
func (e *Engine) upsert(ctx context.Context, client rosClient, menu, keyProp, keyValue string, args []string) error {
	id, err := e.findID(ctx, client, menu, keyProp, keyValue)
	if err != nil {
		return err
	}
	if id == "" {
		if _, err := e.run(ctx, client, append([]string{menu + "/add"}, args...)...); err != nil {
			return fmt.Errorf("%s add %s=%s: %w", menu, keyProp, keyValue, err)
		}
		return nil
	}
	setArgs := append([]string{menu + "/set", "=.id=" + id}, args[1:]...) // skip =name= pada set
	if _, err := e.run(ctx, client, setArgs...); err != nil {
		return fmt.Errorf("%s set %s=%s: %w", menu, keyProp, keyValue, err)
	}
	return nil
}

// removeByName menghapus entri berdasarkan nama; no-op bila tidak ada.
func (e *Engine) removeByName(ctx context.Context, client rosClient, menu, name string) error {
	if name == "" {
		return nil
	}
	id, err := e.findID(ctx, client, menu, "name", name)
	if err != nil {
		return err
	}
	if id == "" {
		return nil // sudah tidak ada — idempotent
	}
	if _, err := e.run(ctx, client, menu+"/remove", "=.id="+id); err != nil {
		return fmt.Errorf("%s remove %s: %w", menu, name, err)
	}
	return nil
}

// dialRouterOS membuka koneksi RouterOS API (plain atau TLS).
func (e *Engine) dialRouterOS(ctx context.Context, r *domain.Router) (rosClient, error) {
	addr := r.APIAddr()
	if e.UseTLS {
		return routeros.DialTLSContext(ctx, addr, r.Username, r.Password, &tls.Config{
			InsecureSkipVerify: true, // RouterOS default self-signed
		})
	}
	return routeros.DialContext(ctx, addr, r.Username, r.Password)
}

// ---------------------------------------------------------------------------
// Util
// ---------------------------------------------------------------------------

// formatRateLimit mengubah speed (Mbps) menjadi format RouterOS "upM/downM".
// Mengembalikan "" bila keduanya nol (unlimited -> tidak diset).
func formatRateLimit(upMbps, downMbps int) string {
	if upMbps <= 0 && downMbps <= 0 {
		return ""
	}
	return fmt.Sprintf("%dM/%dM", upMbps, downMbps)
}

// formatRouterOSDuration memformat time.Duration ke format RouterOS (mis: "1d2h30m").
func formatRouterOSDuration(d time.Duration) string {
	if d < time.Minute {
		return "1m"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	var sb strings.Builder
	if days > 0 {
		fmt.Fprintf(&sb, "%dd", days)
	}
	if hours > 0 {
		fmt.Fprintf(&sb, "%dh", hours)
	}
	if mins > 0 || sb.Len() == 0 {
		fmt.Fprintf(&sb, "%dm", mins)
	}
	return sb.String()
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max]
}
