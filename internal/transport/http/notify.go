package http

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"onobill/internal/domain"
	"onobill/internal/service/notify"
)

// ============ NOTIFIKASI PLATFORM (SUPERADMIN) ============
// Engine WA gateway + Email milik platform (superadmin), tersimpan di NotifSetting TenantID=0.
// Dipakai untuk: notifikasi tenant baru, tagihan langganan, dan BROADCAST ke semua tenant.

// NotifyPage menampilkan pengaturan WA gateway + Email + form broadcast.
func (h *Handler) NotifyPage(w http.ResponseWriter, r *http.Request) {
	st, _ := h.Repo.GetNotifSetting(notify.PlatformTenantID)
	if st == nil {
		st = &domain.NotifSetting{TenantID: notify.PlatformTenantID}
	}
	admins, _ := h.Repo.ListTenantAdmins()
	render(w, "notify", map[string]interface{}{
		"Title":       "Notifikasi Platform - ONOBILL",
		"Page":        "notify",
		"St":          st,
		"TenantCount": len(admins),
		"Success":     r.URL.Query().Get("success"),
		"Error":       r.URL.Query().Get("error"),
	})
}

// NotifySaveSettings menyimpan konfigurasi WA gateway + SMTP milik platform.
func (h *Handler) NotifySaveSettings(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	port, _ := strconv.Atoi(r.FormValue("smtp_port"))
	if port == 0 {
		port = 587
	}
	st := &domain.NotifSetting{
		TenantID:    notify.PlatformTenantID,
		WAEnabled:   r.FormValue("wa_enabled") == "on",
		WAProvider:  "fonnte",
		WAEndpoint:  r.FormValue("wa_endpoint"),
		WAToken:     r.FormValue("wa_token"),
		SMTPEnabled: r.FormValue("smtp_enabled") == "on",
		SMTPHost:    r.FormValue("smtp_host"),
		SMTPPort:    port,
		SMTPUser:    r.FormValue("smtp_user"),
		SMTPPass:    r.FormValue("smtp_pass"),
		SMTPFrom:    r.FormValue("smtp_from"),
	}
	if st.WAEndpoint == "" {
		st.WAEndpoint = "https://api.fonnte.com/send"
	}
	if err := h.Repo.UpsertNotifSetting(st); err != nil {
		http.Redirect(w, r, "/superadmin/notify?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/superadmin/notify?success=settings", http.StatusSeeOther)
}

// NotifyTestWA mengirim WA percobaan ke nomor yang diisi.
func (h *Handler) NotifyTestWA(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	st, err := h.getPlatformNotif()
	if err != nil {
		http.Redirect(w, r, "/superadmin/notify?error=atur+WA+gateway+dulu", http.StatusSeeOther)
		return
	}
	target := r.FormValue("target")
	if err := notify.NewService().SendWA(st, target, "✅ Tes WA gateway ONOBILL berhasil!"); err != nil {
		http.Redirect(w, r, "/superadmin/notify?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/superadmin/notify?success=test_wa", http.StatusSeeOther)
}

// NotifyTestEmail mengirim email percobaan.
func (h *Handler) NotifyTestEmail(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	st, err := h.getPlatformNotif()
	if err != nil {
		http.Redirect(w, r, "/superadmin/notify?error=atur+SMTP+dulu", http.StatusSeeOther)
		return
	}
	to := r.FormValue("target")
	if err := notify.NewService().SendEmail(st, to, "Tes Email ONOBILL", "Halo! Ini email percobaan dari platform ONOBILL.\n\nJika Anda menerima ini, SMTP sudah benar."); err != nil {
		http.Redirect(w, r, "/superadmin/notify?error="+err.Error(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/superadmin/notify?success=test_email", http.StatusSeeOther)
}

// NotifyBroadcast mengirim pesan ke SEMUA admin tenant (WA + Email).
func (h *Handler) NotifyBroadcast(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	message := r.FormValue("message")
	subject := r.FormValue("subject")
	if subject == "" {
		subject = "Pengumuman ONOBILL"
	}
	if message == "" {
		http.Redirect(w, r, "/superadmin/notify?error=pesan+kosong", http.StatusSeeOther)
		return
	}
	st, err := h.getPlatformNotif()
	if err != nil {
		http.Redirect(w, r, "/superadmin/notify?error=atur+gateway+dulu", http.StatusSeeOther)
		return
	}
	admins, _ := h.Repo.ListTenantAdmins()
	svc := notify.NewService()
	waOK, waFail, emailOK, emailFail := 0, 0, 0, 0
	sendWA := r.FormValue("via_wa") == "on"
	sendEmail := r.FormValue("via_email") == "on"

	for _, a := range admins {
		if sendWA && a.Phone != "" {
			if err := svc.SendWA(st, a.Phone, message); err == nil {
				waOK++
			} else {
				waFail++
			}
			time.Sleep(300 * time.Millisecond) // jeda agar tidak kena rate-limit WA gateway
		}
		if sendEmail && a.Email != "" {
			if err := svc.SendEmail(st, a.Email, subject, message); err == nil {
				emailOK++
			} else {
				emailFail++
			}
		}
	}
	msg := fmt.Sprintf("broadcast:+WA %d ok/%d gagal,+Email %d ok/%d gagal", waOK, waFail, emailOK, emailFail)
	http.Redirect(w, r, "/superadmin/notify?success="+msg, http.StatusSeeOther)
}

// getPlatformNotif mengambil NotifSetting platform (TenantID=0).
func (h *Handler) getPlatformNotif() (*domain.NotifSetting, error) {
	st, err := h.Repo.GetNotifSetting(notify.PlatformTenantID)
	if err != nil || st == nil {
		return nil, fmt.Errorf("gateway belum diatur")
	}
	return st, nil
}

// NotifyNewTenant dipanggil saat ada tenant baru mendaftar — kirim WA + Email sambutan.
func (h *Handler) NotifyNewTenant(tenantName, adminName, adminEmail, adminPhone, loginURL string) {
	st, err := h.getPlatformNotif()
	if err != nil {
		return // gateway belum diatur -> lewati diam-diam
	}
	svc := notify.NewService()
	waMsg := fmt.Sprintf("🎉 *Selamat datang di ONOBILL, %s!*\n\nTenant *%s* berhasil dibuat.\nSilakan login: %s\nEmail: %s\n\nTim ONOBILL", adminName, tenantName, loginURL, adminEmail)
	if adminPhone != "" {
		_ = svc.SendWA(st, adminPhone, waMsg)
	}
	if adminEmail != "" {
		emailBody := fmt.Sprintf("Halo %s,\n\nSelamat datang di ONOBILL!\n\nTenant \"%s\" berhasil dibuat.\nSilakan login di: %s\nEmail: %s\n\nSalam,\nTim ONOBILL", adminName, tenantName, loginURL, adminEmail)
		_ = svc.SendEmail(st, adminEmail, "Selamat Datang di ONOBILL", emailBody)
	}
}
