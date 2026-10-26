package main

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestClassifyCheckout_Business2xx(t *testing.T) {
	cases := []struct {
		code int
		body map[string]any
		want string
	}{
		{200, map[string]any{"status": "CONFIRMED"}, ClassBizConfirmed},
		{200, map[string]any{"status": "FAILED"}, ClassBizFailedStatus},
		{200, map[string]any{"status": "PAYMENT_PENDING"}, ClassBizOtherStatus},
		{201, map[string]any{"status": "confirmed"}, ClassBizConfirmed},
	}
	for _, tc := range cases {
		got := classifyCheckout(tc.code, tc.body, nil)
		if got != tc.want {
			t.Fatalf("code=%d body=%v got %s want %s", tc.code, tc.body, got, tc.want)
		}
	}
}

func TestClassifyCheckout_HTTPClasses(t *testing.T) {
	if classifyCheckout(401, nil, nil) != ClassAuthFailure {
		t.Fatal("401")
	}
	if classifyCheckout(403, nil, nil) != ClassAuthFailure {
		t.Fatal("403")
	}
	if classifyCheckout(409, nil, nil) != ClassExpectedReject {
		t.Fatal("409")
	}
	if classifyCheckout(429, nil, nil) != ClassExpectedReject {
		t.Fatal("429")
	}
	if classifyCheckout(400, map[string]any{"error": map[string]any{"code": "ORD_SOLD_OUT"}}, nil) != ClassExpectedReject {
		t.Fatal("400 sold out")
	}
	if classifyCheckout(400, map[string]any{"error": map[string]any{"code": "ORD_BAD_REQUEST"}}, nil) != ClassHTTP4xx {
		t.Fatal("400 other")
	}
	if classifyCheckout(503, nil, nil) != ClassHTTP5xx {
		t.Fatal("503")
	}
	if classifyCheckout(0, nil, nil) != ClassUnclassified {
		t.Fatal("zero code")
	}
}

func TestClassifyCheckout_Transport(t *testing.T) {
	if classifyCheckout(0, nil, context.DeadlineExceeded) != ClassClientTimeout {
		t.Fatal("deadline")
	}
	if classifyCheckout(0, nil, context.Canceled) != ClassClientCancel {
		t.Fatal("cancel")
	}
	if classifyCheckout(0, nil, errors.New("connection refused")) != ClassTransportError {
		t.Fatal("refused")
	}
	// net.Error timeout
	te := timeoutErr{}
	if classifyCheckout(0, nil, te) != ClassClientTimeout {
		t.Fatal("net timeout")
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

func TestOutcomeCounters_Reconcile(t *testing.T) {
	var c outcomeCounters
	c.add(ClassBizConfirmed)
	c.add(ClassHTTP5xx)
	c.add(ClassClientTimeout)
	snap := c.snapshot()
	if snap["attempts"] != 3 || snap["sum_classes"] != 3 {
		t.Fatalf("%v", snap)
	}
}

func TestUnexpected5xxSLO_BlocksUnclassified(t *testing.T) {
	counts := map[string]int64{
		"attempts":         10,
		"sum_classes":      10,
		ClassHTTP5xx:       0,
		ClassUnclassified:  1,
		ClassBizConfirmed:  9,
	}
	pass, _, reason := unexpected5xxSLO(counts, 0.01)
	if pass {
		t.Fatalf("should fail: %s", reason)
	}
	if reason == "" {
		t.Fatal("empty reason")
	}
}

func TestUnexpected5xxSLO_PassAndFail(t *testing.T) {
	ok := map[string]int64{
		"attempts":        100,
		"sum_classes":     100,
		ClassHTTP5xx:      0,
		ClassUnclassified: 0,
		ClassBizConfirmed: 100,
	}
	pass, rate, _ := unexpected5xxSLO(ok, 0.01)
	if !pass || rate != 0 {
		t.Fatalf("pass=%v rate=%v", pass, rate)
	}
	bad := map[string]int64{
		"attempts":        100,
		"sum_classes":     100,
		ClassHTTP5xx:      5,
		ClassUnclassified: 0,
		ClassBizConfirmed: 95,
	}
	pass, rate, _ = unexpected5xxSLO(bad, 0.01)
	if pass || rate < 0.04 {
		t.Fatalf("expected fail rate=%v", rate)
	}
}

func TestClassify_DoesNotTreatTransportAs5xx(t *testing.T) {
	if classifyCheckout(0, nil, errors.New("dial tcp: connection reset")) == ClassHTTP5xx {
		t.Fatal("transport must not be 5xx")
	}
	_ = time.Second // keep import used if trimmed
}
