package http

import (
	"net/http"

	"onobill/internal/domain"
	"onobill/internal/service/notify"
	"onobill/internal/service/payment"
)

// ============ PAYMENT GATEWAY (SUPERADMIN) ============
// Konfigurasi Duitku & KlikQRIS milik platform (superadmin), tersimpan di DB
// (PaymentGatewaySetting TenantID=0) sehingga bisa diubah tanpa restart.
// Perubahan langsung memperbarui registry payment secara live.

// PaymentGatewayPage menampilkan form konfigurasi Duitku & KlikQRIS.
func (h *Handler) PaymentGatewayPage(w http.ResponseWriter, r *http.Request) {
	st, _ := h.Repo.GetPaymentGatewaySetting(notify.PlatformTenantID)
	if st == nil {
		st = &domain.PaymentGatewaySetting{TenantID: notify.PlatformTenantID, DuitkuSandbox: true}
	}
	render(w, "payment", map[string]interface{}{
		"Title":   "Payment Gateway - ONOBILL",
		"Page":    "payment",
		"St":      st,
		"BaseURL": h.BaseURL,
		"Success": r.URL.Query().Get("success"),
		"Error":   r.URL.Query().Get("error"),
	})
}

// PaymentGatewaySave menyimpan konfigurasi & memperbarui registry live.
// POST /superadmin/payment
func (h *Handler) PaymentGatewaySave(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	st := &domain.PaymentGatewaySetting{
		TenantID:         notify.PlatformTenantID,
		DuitkuEnabled:    r.FormValue("duitku_enabled") == "on",
		DuitkuMerchant:   r.FormValue("duitku_merchant"),
		DuitkuAPIKey:     r.FormValue("duitku_api_key"),
		DuitkuSandbox:    r.FormValue("duitku_sandbox") == "on",
		KlikQRISEnabled:  r.FormValue("klikqris_enabled") == "on",
		KlikQRISMerchant: r.FormValue("klikqris_merchant"),
		KlikQRISAPIKey:   r.FormValue("klikqris_api_key"),
	}

	// Pertahankan API key lama bila field dikosongkan (agar tidak tertimpa kosong).
	if old, _ := h.Repo.GetPaymentGatewaySetting(notify.PlatformTenantID); old != nil {
		if st.DuitkuAPIKey == "" {
			st.DuitkuAPIKey = old.DuitkuAPIKey
		}
		if st.KlikQRISAPIKey == "" {
			st.KlikQRISAPIKey = old.KlikQRISAPIKey
		}
	}

	if err := h.Repo.UpsertPaymentGatewaySetting(st); err != nil {
		http.Redirect(w, r, "/superadmin/payment?error=gagal+menyimpan", http.StatusSeeOther)
		return
	}

	// Perbarui registry payment secara live (tanpa restart).
	h.applyPaymentSettings(st)

	http.Redirect(w, r, "/superadmin/payment?success=tersimpan", http.StatusSeeOther)
}

// applyPaymentSettings meregistrasi / menghapus gateway di registry sesuai konfigurasi.
func (h *Handler) applyPaymentSettings(st *domain.PaymentGatewaySetting) {
	if h.Payment == nil {
		return
	}
	callback := h.BaseURL + "/webhooks/payment/duitku"
	returnURL := h.BaseURL + "/billing"

	// Duitku
	if st.DuitkuEnabled && st.DuitkuMerchant != "" && st.DuitkuAPIKey != "" {
		h.Payment.Upsert(payment.NewDuitku(st.DuitkuMerchant, st.DuitkuAPIKey, st.DuitkuBaseURL(), callback, returnURL))
	} else {
		h.Payment.Unregister("duitku")
	}

	// KlikQRIS
	if st.KlikQRISEnabled && st.KlikQRISMerchant != "" && st.KlikQRISAPIKey != "" {
		h.Payment.Upsert(payment.NewKlikQRIS(st.KlikQRISMerchant, st.KlikQRISAPIKey, "", h.BaseURL+"/webhooks/payment/klikqris", returnURL))
	} else {
		h.Payment.Unregister("klikqris")
	}
}

// LoadPaymentGatewayFromDB memuat konfigurasi gateway dari DB ke registry saat boot.
// Dipanggil sekali di main agar pengaturan yang tersimpan tetap aktif setelah restart.
func (h *Handler) LoadPaymentGatewayFromDB() {
	st, _ := h.Repo.GetPaymentGatewaySetting(notify.PlatformTenantID)
	if st != nil {
		h.applyPaymentSettings(st)
	}
}
