package delivery

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Carrier is the boundary to a delivery partner. Not every partner offers
// every capability: Capabilities says which calls are real, and the rest
// return ErrUnsupported. No third-party API is implemented: there is no
// provider specification or contract yet. ManualCarrier is the working
// adapter, where staff hand over the manifest and record what the partner
// reports.
type Carrier interface {
	Capabilities() []Capability
	CreateShipment(ctx context.Context, s Shipment) (ShipmentRef, error)
	CancelShipment(ctx context.Context, ref ShipmentRef) error
	RequestStatus(ctx context.Context, ref ShipmentRef) (*PartnerUpdate, error)
	RetrieveProof(ctx context.Context, ref ShipmentRef) (*PartnerProof, error)
	ReportException(ctx context.Context, ref ShipmentRef, summary string) error
}

type Capability string

const (
	CapCreateShipment  Capability = "create_shipment"
	CapCancelShipment  Capability = "cancel_shipment"
	CapRequestStatus   Capability = "request_status"
	CapTrackingWebhook Capability = "tracking_webhook"
	CapRetrieveProof   Capability = "retrieve_proof"
	CapReportException Capability = "report_exception"
)

// ErrUnsupported: the partner does not offer this capability.
var ErrUnsupported = errors.New("delivery: the partner does not support this")

type Shipment struct {
	DeliveryID string
	Number     string
	Packages   []string
}

type ShipmentRef struct {
	DeliveryID string
	Reference  string
}

// PartnerUpdate is a status a partner reports.
type PartnerUpdate struct {
	Status     string
	OccurredAt time.Time
	EventID    string
	Notes      string
}

type PartnerProof struct {
	Method    string
	Reference string
}

// ManualCarrier: the partner works from the printed manifest; staff record
// handoffs, statuses and proof as the partner reports them. Creating a
// shipment is the manifest itself, so it needs no outside call.
type ManualCarrier struct{}

func (ManualCarrier) Capabilities() []Capability {
	return []Capability{CapCreateShipment, CapCancelShipment}
}

func (ManualCarrier) CreateShipment(_ context.Context, s Shipment) (ShipmentRef, error) {
	return ShipmentRef{DeliveryID: s.DeliveryID, Reference: "manifest:" + s.Number}, nil
}

func (ManualCarrier) CancelShipment(context.Context, ShipmentRef) error { return nil }

func (ManualCarrier) RequestStatus(context.Context, ShipmentRef) (*PartnerUpdate, error) {
	return nil, ErrUnsupported
}

func (ManualCarrier) RetrieveProof(context.Context, ShipmentRef) (*PartnerProof, error) {
	return nil, ErrUnsupported
}

func (ManualCarrier) ReportException(context.Context, ShipmentRef, string) error {
	return ErrUnsupported
}

// ---------------------------------------------------------------- partner webhooks (designed, not routed)

// WebhookVerifier checks a partner's signed webhook before anything in it
// is believed: the partner identity (its own secret), an HMAC-SHA256
// signature over "timestamp.body", a timestamp window against replay, and
// an event id the caller records for idempotency. It is not mounted on any
// route: a webhook is activated only with a real provider's specification
// and credentials. Rate limiting and retries then use the API's existing
// limiter and the job queue.
type WebhookVerifier struct {
	Secret []byte
	Window time.Duration
	Now    func() time.Time
}

var (
	ErrBadSignature = errors.New("delivery webhook: signature does not match")
	ErrStale        = errors.New("delivery webhook: timestamp outside the allowed window")
)

// Verify checks a header like "t=1700000000,v1=<hex>" for body.
func (v WebhookVerifier) Verify(header string, body []byte) error {
	var ts int64
	var sig []byte
	for _, part := range strings.Split(header, ",") {
		k, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts, _ = strconv.ParseInt(val, 10, 64)
		case "v1":
			sig, _ = hex.DecodeString(val)
		}
	}
	if ts == 0 || len(sig) == 0 || len(v.Secret) == 0 {
		return ErrBadSignature
	}
	now := time.Now
	if v.Now != nil {
		now = v.Now
	}
	w := v.Window
	if w == 0 {
		w = 5 * time.Minute
	}
	if d := now().Sub(time.Unix(ts, 0)); d > w || d < -w {
		return ErrStale
	}
	m := hmac.New(sha256.New, v.Secret)
	m.Write([]byte(strconv.FormatInt(ts, 10) + "."))
	m.Write(body)
	if !hmac.Equal(m.Sum(nil), sig) {
		return ErrBadSignature
	}
	return nil
}
