package notifications

import (
	"context"
	"strings"

	"mise.tt/api/internal/appnotify"
	"mise.tt/api/internal/commerce"
	"mise.tt/api/internal/platform/db"
)

// Sprint 10: each customer email event also becomes an entry in the app's
// notification centre (and a push, when the customer has the app and push
// is configured). Lock-screen text is neutral: no amounts, plans, names or
// message text.

const (
	pushPayment      = "There is an update about a payment. Open Mise to see it."
	pushSubscription = "There is an update to your Mise subscription."
	pushSelection    = "Your meals are waiting to be chosen."
	pushDelivery     = "There is an update about your delivery."
	pushChat         = "The Mise team replied to your message."
)

func commerceNote(kind string, d map[string]string, e commerceEmailData) (appnotify.Note, bool) {
	n := appnotify.Note{Title: e.Heading, PushTitle: "Mise", DedupKey: appnotify.Key("commerce."+kind, d)}
	if len(e.Paragraphs) > 0 {
		n.Body = strings.Join(e.Paragraphs, " ")
	}
	switch kind {
	case commerce.EmailReceipt:
		n.Type, n.Link, n.PushBody = "payment.succeeded", "/subscription", pushPayment
	case commerce.EmailPaymentFailed:
		n.Type, n.Link, n.PushBody = "payment.failed", "/subscription", pushPayment
	case commerce.EmailPaymentDue:
		n.Type, n.Link, n.PushBody = "payment.due", "/subscription", pushPayment
	case commerce.EmailActivated:
		n.Type, n.Link, n.PushBody = "subscription.renewal", "/box", pushSubscription
	case commerce.EmailMenuAvailable, commerce.EmailSelectionRemind:
		n.Type, n.Link, n.PushBody = "selection.reminder", "/box", pushSelection
		if kind == commerce.EmailSelectionRemind {
			n.Type = "selection.deadline"
		}
	case commerce.EmailSelectionConfirm:
		n.Type, n.Link, n.PushBody = "selection.confirmed", "/box", pushSelection
	case commerce.EmailPaused, commerce.EmailResumed, commerce.EmailCancelled, commerce.EmailPlanChanged:
		n.Type, n.Link, n.PushBody = "subscription.changed", "/subscription", pushSubscription
	default:
		return n, false
	}
	return n, true
}

func record(ctx context.Context, inbox *appnotify.Service, q db.Querier, userID string, n appnotify.Note) error {
	if inbox == nil {
		return nil
	}
	return inbox.Record(ctx, q, userID, n)
}
