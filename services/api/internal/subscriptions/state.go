package subscriptions

import "slices"

// Status is a subscription's status.
type Status string

const (
	PendingPayment  Status = "pending_payment"
	Active          Status = "active"
	PauseScheduled  Status = "pause_scheduled"
	Paused          Status = "paused"
	PastDue         Status = "past_due"
	CancelScheduled Status = "cancel_scheduled"
	Cancelled       Status = "cancelled"
	Expired         Status = "expired"
)

// transitions is every legal status change. Anything else is refused.
//
//   - pending_payment becomes active only when the first payment is
//     confirmed by the provider, or expired if it never is.
//   - past_due: a renewal was not paid. Paying it (or a later week) makes the
//     subscription active again.
//   - pause_scheduled / cancel_scheduled: the request is recorded and takes
//     effect from a later week, because the current week is already paid.
var transitions = map[Status][]Status{
	PendingPayment:  {Active, Expired},
	Active:          {PauseScheduled, Paused, PastDue, CancelScheduled, Cancelled},
	PauseScheduled:  {Active, Paused, PastDue, CancelScheduled, Cancelled},
	Paused:          {Active, CancelScheduled, Cancelled},
	PastDue:         {Active, CancelScheduled, Cancelled},
	CancelScheduled: {Active, Cancelled},
	Cancelled:       {},
	Expired:         {},
}

// CanTransition reports whether from -> to is a legal change.
func CanTransition(from, to Status) bool { return slices.Contains(transitions[from], to) }

// Live reports whether the subscription is in progress (not ended).
func (s Status) Live() bool { return s != Cancelled && s != Expired }

// Delivering reports whether new weeks are being created.
func (s Status) Delivering() bool {
	return s == Active || s == PauseScheduled || s == Paused || s == PastDue || s == CancelScheduled
}

// Label is the status in words, for customers.
func (s Status) Label() string {
	switch s {
	case PendingPayment:
		return "Waiting for payment"
	case Active:
		return "Active"
	case PauseScheduled:
		return "Pause scheduled"
	case Paused:
		return "Paused"
	case PastDue:
		return "Payment needed"
	case CancelScheduled:
		return "Ending"
	case Cancelled:
		return "Cancelled"
	case Expired:
		return "Not started"
	}
	return string(s)
}
