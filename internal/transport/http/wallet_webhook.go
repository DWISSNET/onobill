package http

import (
	"io"
	"log"
	"net/http"

	"onobill/internal/service/payment"
)

// WalletWebhook memproses callback Duitku untuk order top-up wallet.
// Signature dan status sudah dinormalisasi oleh adapter gateway.
func (h *Handler) WalletWebhook(w http.ResponseWriter, r *http.Request) {
	gatewayName := r.PathValue("gateway")
	gw, ok := h.Payment.Get(gatewayName)
	if !ok || h.Wallet == nil || h.Repo == nil {
		writeError(w, http.StatusBadRequest, "gateway wallet tidak tersedia")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "body tidak valid")
		return
	}
	headers := make(map[string]string, len(r.Header))
	for k := range r.Header {
		headers[k] = r.Header.Get(k)
	}
	cb, err := gw.ParseCallback(body, headers)
	if err != nil {
		log.Printf("[wallet webhook] %s invalid: %v", gatewayName, err)
		writeError(w, http.StatusForbidden, "invalid signature")
		return
	}
	if cb.OrderID == "" || cb.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "callback tidak lengkap")
		return
	}
	if !cb.Paid {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	status, err := h.applyTopUpCallback(cb)
	if err != nil {
		log.Printf("[wallet webhook] order %s: %v", cb.OrderID, err)
		writeError(w, http.StatusInternalServerError, "failed to apply top-up")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

var _ payment.CallbackPayload
