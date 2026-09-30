// Implementasi gateway Duitku (QRIS, VA, e-wallet, dsb).
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

type Duitku struct {
	MerchantCode string
	APIKey       string
	BaseURL      string
	CallbackURL  string
	ReturnURL    string
	hc           *http.Client
}

func NewDuitku(merchantCode, apiKey, baseURL, callbackURL, returnURL string) *Duitku {
	if baseURL == "" {
		baseURL = "https://sandbox.duitku.com/webapi/api/merchant"
	}
	return &Duitku{merchantCode, apiKey, baseURL, callbackURL, returnURL, &http.Client{Timeout: 20 * time.Second}}
}

func (d *Duitku) Name() string { return "duitku" }

// signature inquiry: md5(merchantCode + orderID + amount + apiKey)
func (d *Duitku) signInquiry(orderID string, amount int64) string {
	sum := md5.Sum([]byte(fmt.Sprintf("%s%s%d%s", d.MerchantCode, orderID, amount, d.APIKey)))
	return hex.EncodeToString(sum[:])
}

func (d *Duitku) CreateTransaction(req TxRequest) (*TxResult, error) {
	method := req.Method
	if method == "" {
		method = "SQ" // QRIS
	}
	body := map[string]any{
		"merchantCode":    d.MerchantCode,
		"paymentAmount":   req.Amount,
		"paymentMethod":   method,
		"merchantOrderId": req.OrderID,
		"productDetails":  req.ProductDetail,
		"email":           req.Email,
		"phoneNumber":     req.Phone,
		"customerVaName":  req.CustomerName,
		"callbackUrl":     d.CallbackURL,
		"returnUrl":       d.ReturnURL,
		"signature":       d.signInquiry(req.OrderID, req.Amount),
		"expiryPeriod":    60,
	}
	b, _ := json.Marshal(body)
	resp, err := d.hc.Post(d.BaseURL+"/v2/inquiry", "application/json", strings.NewReader(string(b)))
	if err != nil {
		return nil, fmt.Errorf("duitku inquiry: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("duitku parse: %w (body: %s)", err, string(raw))
	}
	if sc, _ := out["statusCode"].(string); sc != "" && sc != "00" {
		return nil, fmt.Errorf("duitku error %s: %v", sc, out["statusMessage"])
	}
	return &TxResult{
		PaymentURL: strOf(out, "paymentUrl"),
		Reference:  strOf(out, "reference"),
		QRString:   strOf(out, "qrString"),
		Raw:        out,
	}, nil
}

// Callback Duitku signature: md5(merchantCode + amount + merchantOrderId + apiKey)
func (d *Duitku) ParseCallback(body []byte, headers map[string]string) (*CallbackPayload, error) {
	var p map[string]any
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("duitku callback parse: %w", err)
	}
	orderID := strOf(p, "merchantOrderId")
	amountStr := strOf(p, "amount")
	sig := strOf(p, "signature")
	expect := md5.Sum([]byte(d.MerchantCode + amountStr + orderID + d.APIKey))
	if !strings.EqualFold(hex.EncodeToString(expect[:]), sig) {
		return nil, ErrInvalidSignature
	}
	amt, _ := strconv.ParseInt(amountStr, 10, 64)
	return &CallbackPayload{
		OrderID:   orderID,
		Reference: strOf(p, "reference"),
		Amount:    amt,
		Paid:      strOf(p, "resultCode") == "00",
		Raw:       p,
	}, nil
}

func strOf(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}
