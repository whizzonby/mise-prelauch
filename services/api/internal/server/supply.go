package server

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mise.tt/api/internal/inventory"
	"mise.tt/api/internal/menus"
	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/procurement"
	"mise.tt/api/internal/production"
	"mise.tt/api/internal/suppliers"
)

// SupplyChain holds the Sprint 6 services: suppliers, procurement and
// inventory. They share the commerce clock (tests move it) and the business
// time zone.
type SupplyChain struct {
	Suppliers   *suppliers.Service
	Procurement *procurement.Service
	Inventory   *inventory.Service
}

// NewSupplyChain builds the supply-chain services.
func NewSupplyChain(cfg config.Config, pool *pgxpool.Pool, com *Commerce, alerts suppliers.Alerts, prod *production.Service) *SupplyChain {
	now := func() time.Time { return com.Service.Now() }
	sup := suppliers.NewService(pool)
	sup.Alerts, sup.Now, sup.Location = alerts, now, menus.Location()
	pro := procurement.NewService(pool)
	pro.Suppliers, pro.Production, pro.Alerts, pro.Now, pro.Location = sup, prod, alerts, now, menus.Location()
	inv := inventory.NewService(pool)
	inv.Procurement, inv.Production, inv.Alerts, inv.Now, inv.Location = pro, prod, alerts, now, menus.Location()
	// Procurement counts reserved stock through inventory (an interface it defines).
	pro.Stock = inv
	return &SupplyChain{Suppliers: sup, Procurement: pro, Inventory: inv}
}
