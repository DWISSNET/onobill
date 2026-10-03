package http

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"onobill/internal/domain"
	"onobill/internal/service/payment"
	"onobill/internal/service/wallet"
)

const (
	minTopUpAmount int64 = 1000
	maxTopUpAmount int64 = 10000000
)

func (h *Handler) APIWallet(w http.ResponseWriter, r *http.Request) {
	if h.Wallet == nil {
		writeError(w, http.StatusServiceUnavailable, "wallet service belum aktif")
		return
	}
	wlt, err := h.Wallet.EnsureWallet(getTenantID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wlt)
}

func (h *Handler) APIWalletLedger(w http.ResponseWriter, r *http.Request) {
	if h.Wallet == nil {
		writeError(w, http.StatusServiceUnavailable, "wallet service belum aktif")
		return
	}
	rows, err := h.Wallet.Ledger(getTenantID(r), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (h *Handler) APICreateWalletTopUp(w http.ResponseWriter, r *http.Request) {
	if h.Wallet == nil || h.Payment == nil || h.Repo == nil {
		writeError(w, http.StatusServiceUnavailable, "payment/wallet service belum aktif")
		return
	}
	var req struct {
		Amount int64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "body JSON tidak valid")
		return
	}
	if req.Amount < minTopUpAmount || req.Amount > maxTopUpAmount {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("nominal harus antara %d dan %d Rupiah", minTopUpAmount, maxTopUpAmount))
		return
	}
	gw, ok := h.Payment.Get("duitku")
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "gateway Duitku belum dikonfigurasi")
		return
	}
	tenantID := getTenantID(r)
	if tenantID == 0 {
		writeError(w, http.StatusUnauthorized, "tenant tidak valid")
		return
	}
	if _, err := h.Wallet.EnsureWallet(tenantID); err != nil {
		writeError(w, http.StatusInternalServerError, "gagal menyiapkan wallet")
		return
	}
	orderID, err := newTopUpOrderID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "gagal membuat order")
		return
	}
	topup := &domain.TopUp{TenantID: tenantID, OrderID: orderID, Gateway: "duitku", Amount: req.Amount, Status: "pending", ExpiresAt: time.Now().Add(60 * time.Minute)}
	if err := h.Repo.DB().Create(topup).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "gagal menyimpan top-up")
		return
	}
	res, err := gw.CreateTransaction(payment.TxRequest{OrderID: orderID, Amount: req.Amount, Method: "SQ", ProductDetail: "Top up saldo MikhmonCloud " + orderID, CustomerName: r.Header.Get("X-User-Email")})
	if err != nil {
		h.Repo.DB().Model(topup).Updates(map[string]any{"status": "failed"})
		writeError(w, http.StatusBadGateway, "gagal membuat pembayaran Duitku")
		return
	}
	if err := h.Repo.DB().Model(topup).Updates(map[string]any{"payment_url": res.PaymentURL, "qr_string": res.QRString, "gateway_reference": res.Reference}).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "gagal menyimpan detail pembayaran")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"order_id": orderID, "amount": req.Amount, "status": "pending", "payment_url": res.PaymentURL, "qr_string": res.QRString, "reference": res.Reference, "expires_at": topup.ExpiresAt})
}

func (h *Handler) applyTopUpCallback(cb *payment.CallbackPayload) (string, error) {
	var topup domain.TopUp
	if err := h.Repo.DB().Where("order_id = ? AND gateway = ?", cb.OrderID, "duitku").First(&topup).Error; err != nil {
		return "", err
	}
	if cb.Amount != topup.Amount {
		return "", fmt.Errorf("amount mismatch: callback=%d expected=%d", cb.Amount, topup.Amount)
	}
	if topup.Status == "paid" {
		return "already_paid", nil
	}
	if topup.Status != "pending" {
		return "ignored", nil
	}
	if !topup.ExpiresAt.IsZero() && time.Now().After(topup.ExpiresAt) {
		h.Repo.DB().Model(&topup).Update("status", "expired")
		return "expired", nil
	}
	if err := h.Wallet.CreditTopUp(topup.TenantID, topup.OrderID, topup.Amount); err != nil && !errors.Is(err, wallet.ErrDuplicateReference) {
		return "", err
	}
	paidAt := time.Now()
	if err := h.Repo.DB().Model(&topup).Updates(map[string]any{"status": "paid", "gateway_reference": cb.Reference, "paid_at": paidAt}).Error; err != nil {
		return "", err
	}
	return "paid", nil
}

func newTopUpOrderID() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "MC-TU-" + time.Now().UTC().Format("20060102150405") + "-" + strings.ToUpper(hex.EncodeToString(b)), nil
}
