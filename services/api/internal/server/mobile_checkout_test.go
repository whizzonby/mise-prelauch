package server_test

import (
	"net/http"
	"strings"
	"testing"

	"mise.tt/api/internal/platform/config"
	"mise.tt/api/internal/server"
)

// Sprint 10: hosted checkout started from the app returns to the app, at a
// URL the server sets. The return proves nothing: only the server's status does.

func TestAppCheckoutReturnsToTheAppAndIsVerifiedServerSide(t *testing.T) {
	s := newShop(t)
	h := s.h
	web, addr := s.customer("app-buyer@example.com")
	_ = web
	access, _ := tokens(h.mobileLogin("app-buyer@example.com", "install-pay-0001"))
	app := bearer(access)

	// An unknown client value is refused before anything is created.
	bad := h.do("POST", "/api/v1/me/subscriptions", map[string]any{"plan_id": s.basic, "address_id": addr, "client": "https://evil.example/steal"}, app)
	h.wantError(bad, http.StatusUnprocessableEntity, "validation_failed")
	if n := h.count(`SELECT count(*) FROM subscriptions`); n != 0 {
		t.Fatalf("a refused request created %d subscriptions", n)
	}

	res := h.do("POST", "/api/v1/me/subscriptions", map[string]any{"plan_id": s.basic, "address_id": addr, "client": "app"}, app)
	h.wantStatus(res, http.StatusCreated)
	co := obj(t, res.Data["checkout"])
	redirect := co["redirect_url"].(string)
	ref := redirect[strings.LastIndex(redirect, "/")+1:]
	id := co["id"].(string)

	session := h.do("GET", "/api/v1/test-payments/sessions/"+ref, nil)
	h.wantStatus(session, http.StatusOK)
	if got, want := session.Data["return_url"], "mise-local://checkout/return?checkout="+id; got != want {
		t.Fatalf("return_url = %v, want %v", got, want)
	}

	// Before the provider confirms, the result is pending whatever the app says.
	r := h.do("GET", "/api/v1/checkout/sessions/"+id, nil, app)
	h.wantStatus(r, http.StatusOK)
	if r.Data["outcome"] != "pending" {
		t.Fatalf("outcome before payment = %v", r.Data["outcome"])
	}

	s.pay(ref, "approve", "")
	h.drainJobs()
	r = h.do("GET", "/api/v1/checkout/sessions/"+id, nil, app)
	h.wantStatus(r, http.StatusOK)
	if r.Data["outcome"] != "succeeded" {
		t.Fatalf("outcome after payment = %v", r.Data["outcome"])
	}

	// A duplicate subscribe while one exists is refused, not charged twice.
	dup := h.do("POST", "/api/v1/me/subscriptions", map[string]any{"plan_id": s.basic, "address_id": addr, "client": "app"}, app)
	if dup.Status != http.StatusConflict {
		t.Fatalf("second subscribe: status %d, body %s", dup.Status, dup.Raw)
	}

	// Another customer cannot read this checkout.
	other, _ := s.customer("someone-else@example.com")
	h.wantStatus(h.do("GET", "/api/v1/checkout/sessions/"+id, nil, other), http.StatusNotFound)
}

func TestAppCheckoutUnavailableWithoutReturnURL(t *testing.T) {
	s := newShop(t, func(c *config.Config, _ *server.Limits) { c.MobilePaymentReturnURL = "" })
	h := s.h
	tok, addr := s.customer("no-app-pay@example.com")
	res := h.do("POST", "/api/v1/me/subscriptions", map[string]any{"plan_id": s.basic, "address_id": addr, "client": "app"}, tok)
	h.wantError(res, http.StatusServiceUnavailable, "app_checkout_unavailable")
	if n := h.count(`SELECT count(*) FROM subscriptions`); n != 0 {
		t.Fatalf("a refused request created %d subscriptions", n)
	}
	// The website flow is unchanged.
	s.subscribe(tok, addr, s.basic, nil)
}
