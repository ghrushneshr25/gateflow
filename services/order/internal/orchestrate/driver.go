package orchestrate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/testhooks"
	"github.com/ghrushneshrathod/gateflow/services/order/internal/store"
)

// Terminal statuses.
const (
	StatusCreated                = "CREATED"
	StatusEntitlementBound       = "ENTITLEMENT_BOUND"
	StatusReservationHeld        = "RESERVATION_HELD"
	StatusPaymentPending         = "PAYMENT_PENDING"
	StatusPaymentSucceeded       = "PAYMENT_SUCCEEDED"
	StatusReconciliationRequired = "RECONCILIATION_REQUIRED"
	StatusConfirmed              = "CONFIRMED"
	StatusFailed                 = "FAILED"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrInvalidState = errors.New("invalid state for transition")
)

// Downstream ports used by the recovery driver (HTTP or test doubles).
type Downstream interface {
	BindEntitlement(ctx context.Context, entitlementID, buyerID, checkoutOp, corr string) error
	ConsumeEntitlement(ctx context.Context, entitlementID, checkoutOp, corr string) error
	Reserve(ctx context.Context, operationID, poolID, buyerID, orderID string, qty int, leaseSeconds int, corr string) (reservationID string, err error)
	GetReservationByOperation(ctx context.Context, operationID, corr string) (id, status string, err error)
	ConfirmReservation(ctx context.Context, reservationID, corr string) (status string, err error)
	CreatePayment(ctx context.Context, operationID, orderID string, amountCents int64, currency, scenario, corr string) (paymentID, status string, err error)
	GetPaymentByOperation(ctx context.Context, operationID, corr string) (paymentID, status string, err error)
	// EnsureCompensation creates-or-gets compensation for payment_operation_id and optionally executes once.
	EnsureCompensation(ctx context.Context, paymentOperationID, orderID, corr string, execute bool) (compensationOperationID, status string, err error)
}

// OrderRepo is the persistence surface needed by Advance.
type OrderRepo interface {
	Get(ctx context.Context, id string) (store.Order, error)
	Update(ctx context.Context, o store.Order) error
	Confirm(ctx context.Context, orderID, eventID, corr string, payload any, now time.Time) (store.Order, error)
	Fail(ctx context.Context, orderID string, now time.Time) error
}

// ErrAmbiguous means the caller could not determine downstream outcome.
var ErrAmbiguous = errors.New("ambiguous downstream outcome")

// ErrDefinitive is a known business failure (capacity, payment failed, etc.).
type ErrDefinitive struct {
	Code string
	Msg  string
}

func (e ErrDefinitive) Error() string { return e.Msg }

// Driver advances a single order one or more safe steps within one call.
type Driver struct {
	Store      OrderRepo
	Downstream Downstream
	// PaymentScenario is forwarded on first payment create (tests inject failure/success).
	PaymentScenario string
	LeaseSeconds    int
	// PaymentDeadline is how long payment may remain unknown (default 3m). Does not extend reservation lease.
	PaymentDeadline time.Duration
	// Now optional clock for tests.
	Now func() time.Time
}

func (d *Driver) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

func confirmationEventID(orderID string) string {
	return "evt_order_confirmed_" + orderID
}

// Advance loads nothing; operates on the provided order snapshot and reloads after writes.
func (d *Driver) Advance(ctx context.Context, orderID, corr string) (store.Order, error) {
	o, err := d.Store.Get(ctx, orderID)
	if err != nil {
		return o, err
	}
	return d.advanceOrder(ctx, o, corr)
}

func (d *Driver) advanceOrder(ctx context.Context, o store.Order, corr string) (store.Order, error) {
	if o.Status == StatusConfirmed || o.Status == StatusFailed {
		return o, nil
	}
	lease := d.LeaseSeconds
	if lease <= 0 {
		lease = 300
	}
	scenario := d.PaymentScenario
	if scenario == "" {
		scenario = "success"
	}

	// Loop a bounded number of steps so one request can finish the happy path.
	for step := 0; step < 8; step++ {
		switch o.Status {
		case StatusCreated:
			if err := d.Downstream.BindEntitlement(ctx, o.EntitlementID, o.BuyerID, o.CheckoutOperationID, corr); err != nil {
				if errors.Is(err, ErrAmbiguous) {
					return d.mark(ctx, o, StatusReconciliationRequired)
				}
				_ = d.Store.Fail(ctx, o.ID, time.Now().UTC())
				o.Status = StatusFailed
				return o, err
			}
			o.Status = StatusEntitlementBound
			o.UpdatedAt = time.Now().UTC()
			if err := d.Store.Update(ctx, o); err != nil {
				return o, err
			}

		case StatusEntitlementBound:
			resID, err := d.ensureReservation(ctx, o, lease, corr)
			if err != nil {
				if errors.Is(err, ErrAmbiguous) {
					return d.mark(ctx, o, StatusReconciliationRequired)
				}
				var def ErrDefinitive
				if errors.As(err, &def) {
					_ = d.Store.Fail(ctx, o.ID, time.Now().UTC())
					o.Status = StatusFailed
					return o, err
				}
				return d.mark(ctx, o, StatusReconciliationRequired)
			}
			o.ReservationID = &resID
			o.Status = StatusReservationHeld
			o.UpdatedAt = time.Now().UTC()
			if err := d.Store.Update(ctx, o); err != nil {
				return o, err
			}

		case StatusReservationHeld:
			// Consume is best-effort but errors must be visible if definitive.
			if err := d.Downstream.ConsumeEntitlement(ctx, o.EntitlementID, o.CheckoutOperationID, corr); err != nil {
				if !errors.Is(err, ErrAmbiguous) {
					// Definitive consume failure: keep reservation/hold; require reconcile.
					return d.mark(ctx, o, StatusReconciliationRequired)
				}
				// Ambiguous consume: continue; bind already holds checkout exclusivity.
			}
			payID, payStatus, err := d.ensurePayment(ctx, o, scenario, corr)
			if err != nil {
				if errors.Is(err, ErrAmbiguous) {
					return d.mark(ctx, o, StatusPaymentPending)
				}
				var def ErrDefinitive
				if errors.As(err, &def) && def.Code == "PAYMENT_FAILED" {
					_ = d.Store.Fail(ctx, o.ID, time.Now().UTC())
					o.Status = StatusFailed
					return o, err
				}
				return d.mark(ctx, o, StatusPaymentPending)
			}
			o.PaymentID = &payID
			if payStatus == "FAILED" {
				_ = d.Store.Fail(ctx, o.ID, time.Now().UTC())
				o.Status = StatusFailed
				o.UpdatedAt = time.Now().UTC()
				_ = d.Store.Update(ctx, o)
				return o, nil
			}
			if payStatus != "SUCCEEDED" {
				// Set deadline on first pending observation.
				if o.PaymentDeadlineAt == nil {
					dl := d.PaymentDeadline
					if dl <= 0 {
						dl = 3 * time.Minute
					}
					t := d.now().Add(dl)
					o.PaymentDeadlineAt = &t
				}
				return d.mark(ctx, o, StatusPaymentPending)
			}
			o.Status = StatusPaymentSucceeded
			o.UpdatedAt = d.now()
			if err := d.Store.Update(ctx, o); err != nil {
				return o, err
			}

		case StatusPaymentPending:
			// Enforce outcome deadline without extending reservation lease.
			deadline := d.PaymentDeadline
			if deadline <= 0 {
				deadline = 3 * time.Minute
			}
			if o.PaymentDeadlineAt != nil && d.now().After(*o.PaymentDeadlineAt) {
				// Past deadline still unknown → escalate to reconciliation (do not Fail).
				return d.mark(ctx, o, StatusReconciliationRequired)
			}
			payID, payStatus, err := d.Downstream.GetPaymentByOperation(ctx, o.PaymentOperationID, corr)
			if err != nil {
				if errors.Is(err, ErrAmbiguous) {
					return o, nil // still unknown
				}
				// not found yet — still pending
				return o, nil
			}
			o.PaymentID = &payID
			if payStatus == "FAILED" {
				_ = d.Store.Fail(ctx, o.ID, d.now())
				o.Status = StatusFailed
				return o, nil
			}
			if payStatus == "UNKNOWN" || payStatus == "PENDING" {
				return o, nil
			}
			if payStatus != "SUCCEEDED" {
				return o, nil
			}
			o.Status = StatusPaymentSucceeded
			o.UpdatedAt = d.now()
			if err := d.Store.Update(ctx, o); err != nil {
				return o, err
			}

		case StatusPaymentSucceeded, StatusReconciliationRequired:
			if err := d.finalizeInventoryAndConfirm(ctx, &o, corr); err != nil {
				if errors.Is(err, ErrAmbiguous) {
					return d.mark(ctx, o, StatusReconciliationRequired)
				}
				var def ErrDefinitive
				if errors.As(err, &def) && def.Code == "INV_EXPIRED" {
					// Late payment / expired seat: never confirm; ensure compensation once.
					compOp, _, cerr := d.Downstream.EnsureCompensation(ctx, o.PaymentOperationID, o.ID, corr, true)
					if cerr == nil && compOp != "" {
						o.CompensationOperationID = &compOp
					}
					// Crash window: compensation durable at payment; order row may not yet record comp op / RECON status.
					testhooks.MaybeCrashOn("after_compensation_created", o.PaymentOperationID)
					o.Status = StatusReconciliationRequired
					o.UpdatedAt = d.now()
					_ = d.Store.Update(ctx, o)
					return o, nil
				}
				return d.mark(ctx, o, StatusReconciliationRequired)
			}
			return o, nil

		default:
			return o, fmt.Errorf("%w: %s", ErrInvalidState, o.Status)
		}
	}
	return o, nil
}

func (d *Driver) mark(ctx context.Context, o store.Order, status string) (store.Order, error) {
	o.Status = status
	o.UpdatedAt = time.Now().UTC()
	if err := d.Store.Update(ctx, o); err != nil {
		return o, err
	}
	return o, nil
}

func (d *Driver) ensureReservation(ctx context.Context, o store.Order, lease int, corr string) (string, error) {
	if o.ReservationID != nil && *o.ReservationID != "" {
		id, status, err := d.Downstream.GetReservationByOperation(ctx, o.InventoryOperationID, corr)
		if err == nil {
			if status == "EXPIRED" || status == "RELEASED" {
				return "", ErrDefinitive{Code: "INV_EXPIRED", Msg: "reservation not active"}
			}
			return id, nil
		}
	}
	id, status, err := d.Downstream.GetReservationByOperation(ctx, o.InventoryOperationID, corr)
	if err == nil {
		return id, nil
	}
	_ = status
	resID, err := d.Downstream.Reserve(ctx, o.InventoryOperationID, o.PoolID, o.BuyerID, o.ID, o.Quantity, lease, corr)
	if err != nil {
		return "", err
	}
	return resID, nil
}

func (d *Driver) ensurePayment(ctx context.Context, o store.Order, scenario, corr string) (string, string, error) {
	if id, st, err := d.Downstream.GetPaymentByOperation(ctx, o.PaymentOperationID, corr); err == nil {
		return id, st, nil
	}
	return d.Downstream.CreatePayment(ctx, o.PaymentOperationID, o.ID, o.AmountCents, o.Currency, scenario, corr)
}

func (d *Driver) finalizeInventoryAndConfirm(ctx context.Context, o *store.Order, corr string) error {
	// Resolve reservation id
	resID := ""
	if o.ReservationID != nil {
		resID = *o.ReservationID
	}
	if resID == "" {
		id, status, err := d.Downstream.GetReservationByOperation(ctx, o.InventoryOperationID, corr)
		if err != nil {
			return err
		}
		resID = id
		o.ReservationID = &resID
		_ = status
		o.UpdatedAt = time.Now().UTC()
		_ = d.Store.Update(ctx, *o)
	}

	id, status, err := d.Downstream.GetReservationByOperation(ctx, o.InventoryOperationID, corr)
	if err == nil {
		resID = id
		if status == "CONFIRMED" {
			confirmed, err := d.Store.Confirm(ctx, o.ID, confirmationEventID(o.ID), corr, map[string]any{
				"order_id": o.ID, "buyer_id": o.BuyerID, "quantity": o.Quantity, "session_id": o.SessionID,
			}, time.Now().UTC())
			if err != nil {
				return err
			}
			*o = confirmed
			return nil
		}
		if status == "EXPIRED" || status == "RELEASED" {
			return ErrDefinitive{Code: "INV_EXPIRED", Msg: "reservation expired or released"}
		}
	}

	st, err := d.Downstream.ConfirmReservation(ctx, resID, corr)
	if err != nil {
		// Try authoritative lookup
		_, status2, err2 := d.Downstream.GetReservationByOperation(ctx, o.InventoryOperationID, corr)
		if err2 == nil && status2 == "CONFIRMED" {
			st = "CONFIRMED"
		} else if err2 == nil && (status2 == "EXPIRED" || status2 == "RELEASED") {
			return ErrDefinitive{Code: "INV_EXPIRED", Msg: "reservation expired or released"}
		} else {
			return err
		}
	}
	if st != "CONFIRMED" {
		if st == "EXPIRED" {
			return ErrDefinitive{Code: "INV_EXPIRED", Msg: "reservation expired"}
		}
		return ErrAmbiguous
	}
	confirmed, err := d.Store.Confirm(ctx, o.ID, confirmationEventID(o.ID), corr, map[string]any{
		"order_id": o.ID, "buyer_id": o.BuyerID, "quantity": o.Quantity, "session_id": o.SessionID,
	}, time.Now().UTC())
	if err != nil {
		return err
	}
	*o = confirmed
	return nil
}
