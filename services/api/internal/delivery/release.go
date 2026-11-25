package delivery

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Release is a verified fulfillment handed to delivery.
type Release struct {
	ObligationID string
	OrderID      string
	OrderNumber  string
	CustomerID   string
	// The order's week (Monday): eligible delivery dates are counted from it.
	Week time.Time
	// The address copied onto the order when it was made (orders.Address).
	Address      json.RawMessage
	PackageCount int
	Actor        string
}

// Address is the delivery address snapshot (the order's copy, unchanged).
type Address struct {
	RecipientName string  `json:"recipient_name"`
	Phone         *string `json:"phone,omitempty"`
	Line1         string  `json:"line1"`
	Line2         *string `json:"line2,omitempty"`
	Area          *string `json:"area,omitempty"`
	City          string  `json:"city"`
	Region        string  `json:"region"`
	RegionLabel   string  `json:"region_label"`
	Country       string  `json:"country"`
	Instructions  *string `json:"instructions,omitempty"`
}

// Coverage is how an address relates to the configured zones.
type Coverage struct {
	Validation string // serviceable, unzoned, not_serviceable, unknown
	ZoneID     string
	Message    string
}

// CoverageFor applies the coverage rules: the customer-facing region status
// (delivery_zones) and the operational zone covering the region. An address
// is serviceable only when an active region is covered by an active zone.
func CoverageFor(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, country, region string) (Coverage, error) {
	var regionStatus, zoneID, zoneStatus string
	err := q.QueryRow(ctx, `
		SELECT z.status, COALESCE(sz.id::text, ''), COALESCE(sz.status, '')
		FROM delivery_zones z
		LEFT JOIN delivery_zone_regions r ON r.country = z.country AND r.region = z.region
		LEFT JOIN delivery_service_zones sz ON sz.id = r.zone_id
		WHERE z.country = $1 AND z.region = $2`, country, region).Scan(&regionStatus, &zoneID, &zoneStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return Coverage{Validation: "unknown", Message: "No delivery coverage is recorded for this region."}, nil
	}
	if err != nil {
		return Coverage{}, err
	}
	switch {
	case regionStatus != "active":
		return Coverage{Validation: "not_serviceable", Message: "Mise does not currently deliver to this region (" + Human(regionStatus) + ")."}, nil
	case zoneID == "":
		return Coverage{Validation: "unzoned", Message: "The region is served but no delivery zone covers it yet; assign a zone by hand."}, nil
	case zoneStatus != "active":
		return Coverage{Validation: "unzoned", ZoneID: "", Message: "The zone covering this region is not active; assign a zone by hand."}, nil
	}
	return Coverage{Validation: "serviceable", ZoneID: zoneID}, nil
}

// CreateFromRelease makes the delivery for a released fulfillment, in the
// caller's transaction. It is idempotent per obligation. An address outside
// coverage is flagged and raises a blocking exception; it is never silently
// treated as serviceable.
func (s *Service) CreateFromRelease(ctx context.Context, tx pgx.Tx, in Release) (string, error) {
	var existing string
	err := tx.QueryRow(ctx, `SELECT id::text FROM deliveries WHERE obligation_id = $1`, in.ObligationID).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if in.Week.IsZero() {
		return "", errors.New("delivery: release without the order's week")
	}
	var a Address
	if err := json.Unmarshal(in.Address, &a); err != nil || strings.TrimSpace(a.Line1) == "" {
		return "", unprocessable("address_missing", "The order has no usable delivery address.")
	}
	cov, err := CoverageFor(ctx, tx, a.Country, a.Region)
	if err != nil {
		return "", err
	}
	number, err := NextNumber(ctx, tx, "DLV", WeekPeriod(s.Now().In(s.Location)))
	if err != nil {
		return "", err
	}
	zoneSource := "none"
	if cov.ZoneID != "" {
		zoneSource = "coverage_rule"
	}
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO deliveries (number, obligation_id, order_id, customer_id, zone_id, zone_source, address, address_validation, package_count, order_week)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id::text`,
		number, in.ObligationID, in.OrderID, in.CustomerID, nilIfEmpty(cov.ZoneID), zoneSource, in.Address, cov.Validation, in.PackageCount, in.Week).Scan(&id); err != nil {
		return "", err
	}
	if err := s.track(ctx, tx, trackInput{DeliveryID: id, Type: "created", To: "pending_assignment", Attempt: 1, Source: "system",
		Actor: in.Actor, Notes: "Released from fulfillment " + in.OrderNumber + ".", Key: "created"}); err != nil {
		return "", err
	}
	if cov.Validation != "serviceable" {
		sev := "medium"
		if cov.Validation == "not_serviceable" {
			sev = "high"
		}
		if _, err := s.RaiseException(ctx, tx, ExceptionInput{Stage: "delivery", Type: "address_problem", Severity: sev, Blocking: cov.Validation == "not_serviceable",
			Summary: "Address needs a zone: " + Human(cov.Validation), Notes: cov.Message, DeliveryID: id, NextAction: "none",
			DedupeKey: "address:" + id + ":1", Actor: in.Actor}); err != nil {
			return "", err
		}
	}
	return id, record(ctx, tx, in.Actor, "delivery.created", "delivery", id, map[string]any{"address_validation": cov.Validation, "zone_source": zoneSource})
}

// ConfirmationCode is the six-digit code for one delivery attempt. It is an
// HMAC of the delivery id and attempt under the server key, so it matches
// only that delivery and is never stored.
func (s *Service) ConfirmationCode(deliveryID string, attempt int) string {
	m := hmac.New(sha256.New, append([]byte("mise-delivery-code-v1:"), s.CodeKey...))
	fmt.Fprintf(m, "%s:%d", deliveryID, attempt)
	n := binary.BigEndian.Uint32(m.Sum(nil)[:4]) % 1000000
	return fmt.Sprintf("%06d", n)
}

func (s *Service) checkCode(deliveryID string, attempt int, code string) bool {
	return hmac.Equal([]byte(s.ConfirmationCode(deliveryID, attempt)), []byte(strings.TrimSpace(code)))
}
