// Package delivery answers one question for now: can Mise deliver to a
// region? Zones are configured by operations in delivery_zones. While none
// are configured the answer is always "unknown", and the customer app says
// availability is pending. Nothing here promises delivery.
//
// Delivery partners (Food Drop or others) will sit behind their own interface
// in a later sprint; this package does not know about them.
package delivery

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"mise.tt/api/internal/platform/db"
)

// Availability of delivery to a place.
type Availability string

const (
	// Unknown: no zone information for this place (including "no zones yet").
	Unknown Availability = "unknown"
	// Available: an active zone covers it.
	Available Availability = "available"
	// Planned: a zone is planned but not running.
	Planned Availability = "planned"
	// Unavailable: Mise has decided not to deliver here for now.
	Unavailable Availability = "unavailable"
)

// Eligibility tells whether a region is served. The database implementation
// is the only one today; a delivery partner's coverage API could be another.
type Eligibility interface {
	Check(ctx context.Context, country, region string) (Availability, error)
}

// Zones reads delivery_zones.
type Zones struct{ DB db.Querier }

func (z Zones) Check(ctx context.Context, country, region string) (Availability, error) {
	if country == "" || region == "" {
		return Unknown, nil
	}
	var status string
	err := z.DB.QueryRow(ctx, `SELECT status FROM delivery_zones WHERE country = $1 AND region = $2`, country, region).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Unknown, nil
	}
	if err != nil {
		return Unknown, err
	}
	switch status {
	case "active":
		return Available, nil
	case "planned":
		return Planned, nil
	case "unavailable":
		return Unavailable, nil
	}
	return Unknown, nil
}
