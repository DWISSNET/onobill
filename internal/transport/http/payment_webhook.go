package http

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"onobill/internal/domain"
	"onobill/internal/service/notify"
	"onobill/internal/service/payment"
)

// ============ PAYMENT CHECKOUT & WEBHOOK ============

// APICreatePaymentLink membuat link/QR pembayaran untuk sebuah invoice.
// POST /api/v1/invoices/{id}/pay  {gateway: "duitku"|"klikqris"}
func (h *Handler) APICreatePaymentLink(w http.ResponseWriter, r *http.Request) {
	tenantID := getTenantID(r)
	id, _ := parseID(r, "id")
	if id == 0 {
		writeError(w, http.StatusBadRequest, "invalid invoice id")
		return
	}
	var body struct {
		Gateway string `json:"gateway"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Gateway == "" {
		body.Gateway = "duitku"
	}
	inv, err := h.Billing.GetInvoice(tenantID, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "invoice not found")
		return
	}
	gw, ok := h.Payment.Get(body.Gateway)
	if !ok {
		writeError(w, http.StatusBadRequest, "gateway not configured: "+body.Gateway)
		return
	}
	res, err := gw.CreateTransaction(payment.TxRequest{
		OrderID:       inv.Number,
		Amount:        int64(inv.Total),
		ProductDetail: "Tagihan " + inv.Number,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "gateway error: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"invoice":     inv.Number,
		"gateway":     body.Gateway,
		"payment_url": res.PaymentURL,
		"qr_string":   res.QRString,
		"reference":   res.Reference,
	})
}

// PaymentWebhook menerima callback dari gateway dan menandai invoice lunas.
// POST /webhooks/payment/{gateway}  (tanpa auth, divalidasi signature)
func (h *Handler) PaymentWebhook(w http.ResponseWriter, r *http.Request) {
	gatewayName := r.PathValue("gateway")
	gw, ok := h.Payment.Get(gatewayName)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown gateway")
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	headers := map[string]string{}
	for k := range r.Header {
		headers[k] = r.Header.Get(k)
	}
	cb, err := gw.ParseCallback(body, headers)
	if err != nil {
		log.Printf("[webhook] %s invalid: %v", gatewayName, err)
		writeError(w, http.StatusForbidden, "invalid signature")
		return
	}
	// Cari invoice berdasarkan orderID (= invoice number, unik global)
	inv, err := h.Repo.GetInvoiceByNumber(cb.OrderID)
	if err != nil {
		log.Printf("[webhook] invoice not found for order %s", cb.OrderID)
		writeError(w, http.StatusNotFound, "invoice not found")
		return
	}
	if !cb.Paid {
		log.Printf("[webhook] %s order %s not paid yet", gatewayName, cb.OrderID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	// Idempotent: jika sudah lunas, jangan proses ulang
	if inv.Status == "paid" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already_paid"})
		return
	}
	// Tandai lunas via billing service (trigger auto-unisolate via OnCustomerReactivate)
	if _, err := h.Billing.PayInvoice(inv.TenantID, inv.ID, float64(cb.Amount), gatewayName, cb.Reference); err != nil {
		log.Printf("[webhook] pay invoice %s: %v", cb.OrderID, err)
		writeError(w, http.StatusInternalServerError, "failed to mark paid")
		return
	}
	// Notifikasi WA/Telegram (best-effort)
	h.notifyPayment(inv)
	writeJSON(w, http.StatusOK, map[string]string{"status": "paid"})
}

// notifyPayment kirim notif lunas ke pelanggan (best-effort, tidak ganggu respons webhook).
func (h *Handler) notifyPayment(inv *domain.Invoice) {
	st, err := h.Repo.GetNotifSetting(inv.TenantID)
	if err != nil || st == nil || !st.NotifyPayment {
		return
	}
	cust, err := h.Customer.Get(inv.TenantID, inv.CustomerID)
	if err != nil {
		return
	}
	tenant, err := h.Tenant.Get(inv.TenantID)
	tenantName := "ONOBILL"
	if err == nil {
		tenantName = tenant.Name
	}
	msg := notify.PaymentMessage(inv, cust.Name, tenantName)
	if st.WAEnabled && cust.Phone != "" {
		if err := h.Notify.SendWA(st, cust.Phone, msg); err != nil {
			log.Printf("[notify] WA to %s: %v", cust.Phone, err)
		}
	}
	if st.TGEnabled && st.TGChatID != "" {
		if err := h.Notify.SendTelegram(st, st.TGChatID, msg); err != nil {
			log.Printf("[notify] TG: %v", err)
		}
	}
}
