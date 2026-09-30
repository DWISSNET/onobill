// Implementasi gateway KlikQRIS (QRIS dinamis).
// API KlikQRIS: signature HMAC/MD5 sederhana; sesuaikan dengan dokumentasi akun CEO.
package payment

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type KlikQRIS struct {
	MerchantID  string
	APIKey      string
	BaseURL     string
	CallbackURL string
	ReturnURL   string
	hc          *http.Client
}

func NewKlikQRIS(merchantID, apiKey, baseURL, callbackURL, returnURL string) *KlikQRIS {
	if baseURL == "" {
		baseURL = "https://api.klikqris.com"
	}
	return &KlikQRIS{merchantID, apiKey, baseURL, callbackURL, returnURL, &http.Client{Timeout: 20 * time.Second}}
}

func (k *KlikQRIS) Name() string { return "klikqris" }

// signature: md5(merchantID + orderID + amount + apiKey)
func (k *KlikQRIS) sign(orderID string, amount int64) string {
	sum := md5.Sum([]byte(fmt.Sprintf("%s%s%d%s", k.MerchantID, orderID, amount, k.APIKey)))
	return hex.EncodeToString(sum[:])
}

func (k *KlikQRIS) CreateTransaction(req TxRequest) (*TxResult, error) {
	body := map[string]any{
		"merchant_id":  k.MerchantID,
		"order_id":     req.OrderID,
		"amount":       req.Amount,
		"product":      req.ProductDetail,
		"customer":     req.CustomerName,
		"email":        req.Email,
		"phone":        req.Phone,
		"callback_url": k.CallbackURL,
		"return_url":   k.ReturnURL,
		"signature":    k.sign(req.OrderID, req.Amount),
	}
	b, _ := json.Marshal(body)
	resp, err := k.hc.Post(k.BaseURL+"/v1/qris/create", "application/json", strings.NewReader(string(b)))
	if err != nil {
		return nil, fmt.Errorf("klikqris create: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("klikqris parse: %w (body: %s)", err, string(raw))
	}
	if ok, _ := out["success"].(bool); !ok {
		return nil, fmt.Errorf("klikqris error: %v", out["message"])
	}
	data, _ := out["data"].(map[string]any)
	if data == nil {
		data = out
	}
	return &TxResult{
		PaymentURL: strOf(data, "payment_url"),
		Reference:  strOf(data, "reference"),
		QRString:   strOf(data, "qr_string"),
		Raw:        data,
	}, nil
}

// Callback KlikQRIS signature: md5(merchantID + orderID + amount + apiKey)
func (k *KlikQRIS) ParseCallback(body []byte, headers map[string]string) (*CallbackPayload, error) {
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("klikqris callback parse: %w", err)
	}
	orderID := strOf(p, "order_id")
	amountStr := strOf(p, "amount")
	sig := strOf(p, "signature")
	amt, _ := strconv.ParseInt(amountStr, 10, 64)
	expect := k.sign(orderID, amt)
	if !strings.EqualFold(expect, sig) {
		return nil, ErrInvalidSignature
	}
	status := strings.ToLower(strOf(p, "status"))
	paid := status == "paid" || status == "success" || status == "settlement" || status == "00"
	return &CallbackPayload{
		OrderID:   orderID,
		Reference: strOf(p, "reference"),
		Amount:    amt,
		Paid:      paid,
		Raw:       p,
	}, nil
}
