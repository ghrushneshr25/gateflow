package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

// Mutually exclusive outcome classes for a single checkout attempt.
// Counts must reconcile: sum(classes) == attempts.
const (
	ClassBizConfirmed       = "biz_confirmed"          // HTTP 2xx, status CONFIRMED
	ClassBizFailedStatus    = "biz_failed_status"      // HTTP 2xx, status FAILED (sold-out path etc.)
	ClassBizOtherStatus     = "biz_other_status"       // HTTP 2xx, other business status
	ClassExpectedReject     = "expected_capacity_budget" // HTTP 409/429 or known capacity/budget codes
	ClassAuthFailure        = "auth_failure"           // HTTP 401/403
	ClassHTTP4xx            = "http_4xx_other"         // other 4xx
	ClassHTTP5xx            = "http_5xx"               // 5xx
	ClassClientTimeout      = "client_timeout"
	ClassClientCancel       = "client_cancel"
	ClassTransportError     = "transport_error"
	ClassUnclassified       = "unclassified"
)

// classOrder is the stable reporting order.
var classOrder = []string{
	ClassBizConfirmed,
	ClassBizFailedStatus,
	ClassBizOtherStatus,
	ClassExpectedReject,
	ClassAuthFailure,
	ClassHTTP4xx,
	ClassHTTP5xx,
	ClassClientTimeout,
	ClassClientCancel,
	ClassTransportError,
	ClassUnclassified,
}

type outcomeCounters struct {
	// attempts is total classifyCheckout invocations.
	attempts atomic.Int64
	byClass  [11]atomic.Int64
}

func (c *outcomeCounters) idx(class string) int {
	for i, name := range classOrder {
		if name == class {
			return i
		}
	}
	return len(classOrder) - 1 // unclassified
}

func (c *outcomeCounters) add(class string) {
	c.attempts.Add(1)
	c.byClass[c.idx(class)].Add(1)
}

func (c *outcomeCounters) snapshot() map[string]int64 {
	out := make(map[string]int64, len(classOrder)+1)
	out["attempts"] = c.attempts.Load()
	var sum int64
	for i, name := range classOrder {
		v := c.byClass[i].Load()
		out[name] = v
		sum += v
	}
	out["sum_classes"] = sum
	return out
}

// classifyCheckout maps HTTP status, body, and transport error to one class.
func classifyCheckout(code int, body map[string]any, err error) string {
	if err != nil {
		return classifyTransportErr(err)
	}
	if code <= 0 {
		return ClassUnclassified
	}
	switch {
	case code == 401 || code == 403:
		return ClassAuthFailure
	case code == 409 || code == 429:
		return ClassExpectedReject
	case code >= 500 && code <= 599:
		return ClassHTTP5xx
	case code >= 400 && code <= 499:
		if isExpectedCapacityBudgetBody(body) {
			return ClassExpectedReject
		}
		return ClassHTTP4xx
	case code >= 200 && code <= 299:
		st := digString(body, "status")
		switch strings.ToUpper(st) {
		case "CONFIRMED":
			return ClassBizConfirmed
		case "FAILED":
			return ClassBizFailedStatus
		case "":
			// 2xx without status — try error envelope capacity codes
			if isExpectedCapacityBudgetBody(body) {
				return ClassExpectedReject
			}
			return ClassUnclassified
		default:
			return ClassBizOtherStatus
		}
	default:
		return ClassUnclassified
	}
}

func classifyTransportErr(err error) string {
	if err == nil {
		return ClassUnclassified
	}
	if errors.Is(err, context.Canceled) {
		return ClassClientCancel
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ClassClientTimeout
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return ClassClientTimeout
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") {
		return ClassClientTimeout
	}
	if strings.Contains(msg, "canceled") || strings.Contains(msg, "cancelled") {
		return ClassClientCancel
	}
	return ClassTransportError
}

func isExpectedCapacityBudgetBody(body map[string]any) bool {
	if body == nil {
		return false
	}
	// error.code nested or flat
	code := strings.ToUpper(digString(body, "error", "code"))
	if code == "" {
		code = strings.ToUpper(digString(body, "code"))
	}
	switch code {
	case "INV_INSUFFICIENT_CAPACITY", "INV_SOLD_OUT", "ORD_CAPACITY", "ORD_SOLD_OUT",
		"ADM_BUDGET_EXCEEDED", "ADM_SESSION_BUDGET", "ADM_GLOBAL_BUDGET",
		"ORD_PURCHASE_LIMIT", "ORD_LIMIT_EXCEEDED":
		return true
	}
	msg := strings.ToLower(digString(body, "error", "message"))
	if msg == "" {
		msg = strings.ToLower(digString(body, "message"))
	}
	if strings.Contains(msg, "sold out") || strings.Contains(msg, "insufficient capacity") ||
		strings.Contains(msg, "budget") || strings.Contains(msg, "purchase limit") {
		return true
	}
	return false
}

// unexpected5xxSLO evaluates provisional "unexpected 5xx < threshold" gate.
// Returns pass=false when any outcomes remain unclassified (cannot claim SLO).
func unexpected5xxSLO(counts map[string]int64, threshold float64) (pass bool, rate float64, reason string) {
	if threshold <= 0 {
		threshold = 0.01
	}
	attempts := counts["attempts"]
	if attempts <= 0 {
		return false, 0, "no attempts"
	}
	if counts[ClassUnclassified] > 0 {
		return false, 0, fmt.Sprintf("unclassified=%d; cannot claim 5xx SLO", counts[ClassUnclassified])
	}
	if counts["sum_classes"] != attempts {
		return false, 0, fmt.Sprintf("class sum %d != attempts %d", counts["sum_classes"], attempts)
	}
	n5 := float64(counts[ClassHTTP5xx])
	rate = n5 / float64(attempts)
	if rate >= threshold {
		return false, rate, fmt.Sprintf("http_5xx rate %.4f >= %.4f", rate, threshold)
	}
	return true, rate, fmt.Sprintf("http_5xx rate %.4f < %.4f", rate, threshold)
}

// legacyErrorsFromClass maps class → whether it counted as prior harness "errors" (transport or HTTP>=400).
func legacyHTTPErr(class string) bool {
	switch class {
	case ClassBizConfirmed, ClassBizFailedStatus, ClassBizOtherStatus:
		return false
	default:
		return true
	}
}

// sleep helper kept for tests that need deterministic timing (unused in prod path).
var _ = time.Second
