// Package notify mengirim notifikasi ke pelanggan/admin via WA, Telegram, Email.
// Membaca konfigurasi per-tenant dari NotifSetting.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"onobill/internal/domain"
)

// PlatformTenantID adalah TenantID khusus (0) untuk NotifSetting milik superadmin/platform.
// Engine WA/Email ini dipakai untuk semua notifikasi platform: tenant baru, tagihan langganan,
// broadcast pengumuman ke semua tenant — terpisah dari notifikasi milik tiap tenant.
const PlatformTenantID uint = 0

type Service struct {
	hc *http.Client
}

func NewService() *Service {
	return &Service{hc: &http.Client{Timeout: 15 * time.Second}}
}

// ---- WhatsApp (Fonnte-compatible) ----
// POST endpoint, header Authorization: token, form: target, message
func (svc *Service) SendWA(st *domain.NotifSetting, target, message string) error {
	if !st.WAEnabled || st.WAToken == "" {
		return fmt.Errorf("whatsapp not enabled")
	}
	endpoint := st.WAEndpoint
	if endpoint == "" {
		endpoint = "https://api.fonnte.com/send"
	}
	form := url.Values{}
	form.Set("target", target)
	form.Set("message", message)
	req, err := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", st.WAToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := svc.hc.Do(req)
	if err != nil {
		return fmt.Errorf("wa send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("wa send status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// ---- Telegram Bot ----
func (svc *Service) SendTelegram(st *domain.NotifSetting, chatID, message string) error {
	if !st.TGEnabled || st.TGBotToken == "" {
		return fmt.Errorf("telegram not enabled")
	}
	if chatID == "" {
		chatID = st.TGChatID
	}
	if chatID == "" {
		return fmt.Errorf("no telegram chat id")
	}
	api := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", st.TGBotToken)
	body, _ := json.Marshal(map[string]any{
		"chat_id":    chatID,
		"text":       message,
		"parse_mode": "HTML",
	})
	resp, err := svc.hc.Post(api, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("tg send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("tg send status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// ---- Email (SMTP) ----
// SendEmail mengirim email via SMTP milik platform/tenant (Gmail, SES, Mailgun, dst).
func (svc *Service) SendEmail(st *domain.NotifSetting, to, subject, body string) error {
	if !st.SMTPEnabled || st.SMTPHost == "" {
		return fmt.Errorf("smtp not enabled")
	}
	if to == "" {
		return fmt.Errorf("no recipient")
	}
	port := st.SMTPPort
	if port == 0 {
		port = 587
	}
	from := st.SMTPFrom
	if from == "" {
		from = st.SMTPUser
	}
	addr := fmt.Sprintf("%s:%d", st.SMTPHost, port)

	// Bangun pesan RFC 822
	var msg strings.Builder
	msg.WriteString("From: " + from + "\r\n")
	msg.WriteString("To: " + to + "\r\n")
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	auth := smtp.PlainAuth("", st.SMTPUser, st.SMTPPass, st.SMTPHost)
	if err := smtp.SendMail(addr, auth, from, []string{to}, []byte(msg.String())); err != nil {
		return fmt.Errorf("smtp send: %w", err)
	}
	return nil
}

// ---- Helper: format pesan umum ----

func InvoiceMessage(inv *domain.Invoice, customerName, tenantName, payURL string) string {
	return fmt.Sprintf(
		"🧾 *Tagihan %s*\n\nHalo %s,\nInvoice: %s\nJumlah: Rp %.0f\nJatuh tempo: %s\n\nBayar di sini:\n%s\n\nTerima kasih 🙏",
		tenantName, customerName, inv.Number, inv.Total, inv.DueAt.Format("02 Jan 2006"), payURL)
}

func PaymentMessage(inv *domain.Invoice, customerName, tenantName string) string {
	return fmt.Sprintf(
		"✅ *Pembayaran Diterima*\n\nHalo %s,\nInvoice %s sebesar Rp %.0f telah LUNAS.\nLayanan Anda aktif. Terima kasih! 🙏\n\n- %s",
		customerName, inv.Number, inv.Total, tenantName)
}

func ReminderMessage(inv *domain.Invoice, customerName, tenantName, payURL string, daysLeft int) string {
	return fmt.Sprintf(
		"⏰ *Pengingat Tagihan*\n\nHalo %s,\nInvoice %s (Rp %.0f) jatuh tempo dalam %d hari (%s).\nBayar: %s\n\n- %s",
		customerName, inv.Number, inv.Total, daysLeft, inv.DueAt.Format("02 Jan 2006"), payURL, tenantName)
}

func IsolirMessage(customerName, tenantName string) string {
	return fmt.Sprintf(
		"⚠️ *Layanan Diisolir*\n\nHalo %s,\nLayanan internet Anda dinonaktifkan sementara karena tagihan belum dibayar.\nSegera lunasi untuk mengaktifkan kembali.\n\n- %s",
		customerName, tenantName)
}
