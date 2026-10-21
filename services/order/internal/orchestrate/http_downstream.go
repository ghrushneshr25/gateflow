package orchestrate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ghrushneshrathod/gateflow/libs/go/testhooks"
)

// HTTPDoer matches net/http.Client.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// HTTPDownstream implements Downstream over REST.
type HTTPDownstream struct {
	HTTP          HTTPDoer
	AdmissionBase string
	InventoryBase string
	PaymentBase   string
	// ServiceToken is used for privileged S2S calls (compensation). Per-request WithServiceToken overrides.
	ServiceToken string
}

func (h *HTTPDownstream) BindEntitlement(ctx context.Context, entitlementID, buyerID, checkoutOp, corr string) error {
	body, _ := json.Marshal(map[string]string{
		"buyer_id": buyerID, "checkout_operation_id": checkoutOp,
	})
	_, code, err := h.post(ctx, h.AdmissionBase+"/v1/entitlements/"+entitlementID+"/bind", body, corr)
	if err != nil {
		if isTransport(err) {
			return ErrAmbiguous
		}
		return err
	}
	if code >= 500 {
		return ErrAmbiguous
	}
	if code >= 400 {
		return ErrDefinitive{Code: "ENTITLEMENT", Msg: fmt.Sprintf("bind status %d", code)}
	}
	return nil
}

func (h *HTTPDownstream) ConsumeEntitlement(ctx context.Context, entitlementID, checkoutOp, corr string) error {
	body, _ := json.Marshal(map[string]string{"checkout_operation_id": checkoutOp})
	_, code, err := h.post(ctx, h.AdmissionBase+"/v1/entitlements/"+entitlementID+"/consume", body, corr)
	if err != nil {
		if isTransport(err) {
			return ErrAmbiguous
		}
		return err
	}
	if code >= 500 {
		return ErrAmbiguous
	}
	if code >= 400 {
		return ErrDefinitive{Code: "ENTITLEMENT_CONSUME", Msg: fmt.Sprintf("consume status %d", code)}
	}
	return nil
}

func (h *HTTPDownstream) Reserve(ctx context.Context, operationID, poolID, buyerID, orderID string, qty, leaseSeconds int, corr string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"operation_id": operationID, "pool_id": poolID, "quantity": qty,
		"buyer_id": buyerID, "order_id": orderID, "lease_seconds": leaseSeconds,
	})
	out, code, err := h.post(ctx, h.InventoryBase+"/v1/reservations", body, corr)
	if err != nil {
		if isTransport(err) {
			return "", ErrAmbiguous
		}
		return "", err
	}
	if code == 409 {
		// may be capacity or conflict — try lookup
		if id, _, lerr := h.GetReservationByOperation(ctx, operationID, corr); lerr == nil {
			return id, nil
		}
		return "", ErrDefinitive{Code: "INV_CAPACITY", Msg: "reservation rejected"}
	}
	if code >= 500 {
		return "", ErrAmbiguous
	}
	if code >= 400 {
		return "", ErrDefinitive{Code: "INV_RESERVE", Msg: fmt.Sprintf("reserve status %d", code)}
	}
	id, _ := out["id"].(string)
	if id == "" {
		return "", ErrAmbiguous
	}
	return id, nil
}

func (h *HTTPDownstream) GetReservationByOperation(ctx context.Context, operationID, corr string) (string, string, error) {
	out, code, err := h.get(ctx, h.InventoryBase+"/v1/reservations/by-operation/"+operationID, corr)
	if err != nil {
		if isTransport(err) {
			return "", "", ErrAmbiguous
		}
		return "", "", err
	}
	if code == 404 {
		return "", "", ErrNotFound
	}
	if code >= 500 {
		return "", "", ErrAmbiguous
	}
	if code >= 400 {
		return "", "", fmt.Errorf("reservation lookup %d", code)
	}
	id, _ := out["id"].(string)
	st, _ := out["status"].(string)
	return id, st, nil
}

func (h *HTTPDownstream) ConfirmReservation(ctx context.Context, reservationID, corr string) (string, error) {
	out, code, err := h.post(ctx, h.InventoryBase+"/v1/reservations/"+reservationID+"/confirm", []byte("{}"), corr)
	if err != nil {
		if isTransport(err) {
			return "", ErrAmbiguous
		}
		return "", err
	}
	if code == 409 {
		st, _ := out["status"].(string)
		if st == "" {
			// error body may not include status
			return "EXPIRED", ErrDefinitive{Code: "INV_EXPIRED", Msg: "confirm rejected"}
		}
		return st, ErrDefinitive{Code: "INV_EXPIRED", Msg: "confirm rejected"}
	}
	if code >= 500 {
		return "", ErrAmbiguous
	}
	if code >= 400 {
		return "", fmt.Errorf("confirm status %d", code)
	}
	st, _ := out["status"].(string)
	return st, nil
}

func (h *HTTPDownstream) CreatePayment(ctx context.Context, operationID, orderID string, amountCents int64, currency, scenario, corr string) (string, string, error) {
	body, _ := json.Marshal(map[string]any{
		"operation_id": operationID, "order_id": orderID,
		"amount_cents": amountCents, "currency": currency, "scenario": scenario,
	})
	out, code, err := h.post(ctx, h.PaymentBase+"/v1/payments", body, corr)
	if err != nil {
		if isTransport(err) {
			return "", "", ErrAmbiguous
		}
		return "", "", err
	}
	if code >= 500 {
		return "", "", ErrAmbiguous
	}
	if code >= 400 && code != 409 {
		return "", "", fmt.Errorf("payment status %d", code)
	}
	// 409 conflict or success — prefer lookup
	if code == 409 {
		return h.GetPaymentByOperation(ctx, operationID, corr)
	}
	id, _ := out["id"].(string)
	st, _ := out["status"].(string)
	// P4.5: payment durable success observed by Order but not yet persisted by Order.
	if st == "SUCCEEDED" {
		testhooks.SignalReached("after_payment_success")
		testhooks.MaybeCrash("after_payment_success")
	}
	return id, st, nil
}

func (h *HTTPDownstream) GetPaymentByOperation(ctx context.Context, operationID, corr string) (string, string, error) {
	out, code, err := h.get(ctx, h.PaymentBase+"/v1/payments/by-operation/"+operationID, corr)
	if err != nil {
		if isTransport(err) {
			return "", "", ErrAmbiguous
		}
		return "", "", err
	}
	if code == 404 {
		return "", "", ErrNotFound
	}
	if code >= 500 {
		return "", "", ErrAmbiguous
	}
	id, _ := out["id"].(string)
	st, _ := out["status"].(string)
	return id, st, nil
}

func (h *HTTPDownstream) EnsureCompensation(ctx context.Context, paymentOperationID, orderID, corr string, execute bool) (string, string, error) {
	body, _ := json.Marshal(map[string]any{
		"payment_operation_id": paymentOperationID,
		"order_id":             orderID,
		"execute":              execute,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.PaymentBase+"/v1/compensations", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-ID", corr)
	token := h.ServiceToken
	if tok := ctx.Value(ctxServiceToken{}); tok != nil {
		if s, ok := tok.(string); ok && s != "" {
			token = s
		}
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := h.HTTP.Do(req)
	if err != nil {
		if isTransport(err) {
			return "", "", ErrAmbiguous
		}
		return "", "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		if resp.StatusCode >= 500 {
			return "", "", ErrAmbiguous
		}
		return "", "", fmt.Errorf("compensation status %d: %s", resp.StatusCode, string(b))
	}
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	op, _ := out["operation_id"].(string)
	st, _ := out["status"].(string)
	return op, st, nil
}

type ctxServiceToken struct{}

// WithServiceToken attaches a bearer token for S2S privileged calls.
func WithServiceToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, ctxServiceToken{}, token)
}

var ErrNotFound = fmt.Errorf("not found")

func (h *HTTPDownstream) bearer(ctx context.Context) string {
	token := h.ServiceToken
	if tok := ctx.Value(ctxServiceToken{}); tok != nil {
		if s, ok := tok.(string); ok && s != "" {
			token = s
		}
	}
	return token
}

func (h *HTTPDownstream) post(ctx context.Context, url string, body []byte, corr string) (map[string]any, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Correlation-ID", corr)
	if tok := h.bearer(ctx); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := h.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = map[string]any{}
	}
	return out, resp.StatusCode, nil
}

func (h *HTTPDownstream) get(ctx context.Context, url, corr string) (map[string]any, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-Correlation-ID", corr)
	if tok := h.bearer(ctx); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := h.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	if out == nil {
		out = map[string]any{}
	}
	return out, resp.StatusCode, nil
}

func isTransport(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "Timeout") ||
		strings.Contains(s, "EOF") ||
		strings.Contains(s, "refused")
}
