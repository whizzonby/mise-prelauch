package delivery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestStateMachineAllowsOnlyListedMoves(t *testing.T) {
	ok := [][2]string{{"pending_assignment", "assigned"}, {"assigned", "ready_for_pickup"}, {"ready_for_pickup", "picked_up"},
		{"picked_up", "in_transit"}, {"in_transit", "delivered"}, {"arriving", "delivery_failed"}, {"delivery_failed", "returned"},
		{"delivery_failed", "pending_assignment"}}
	for _, m := range ok {
		if !CanMove(m[0], m[1]) {
			t.Errorf("%s → %s should be allowed", m[0], m[1])
		}
	}
	bad := [][2]string{{"pending_assignment", "delivered"}, {"assigned", "in_transit"}, {"ready_for_pickup", "delivered"},
		{"delivered", "delivery_failed"}, {"delivered", "pending_assignment"}, {"returned", "pending_assignment"}, {"cancelled", "assigned"},
		{"picked_up", "cancelled"}}
	for _, m := range bad {
		if CanMove(m[0], m[1]) {
			t.Errorf("%s → %s should be refused", m[0], m[1])
		}
	}
	for _, s := range Statuses {
		if _, ok := transitions[s]; !ok {
			t.Errorf("status %s has no transition entry", s)
		}
	}
}

func TestConfirmationCodesBelongToOneDeliveryAndAttempt(t *testing.T) {
	s := &Service{CodeKey: []byte("key-one")}
	a := s.ConfirmationCode("11111111-1111-1111-1111-111111111111", 1)
	if len(a) != 6 || a != s.ConfirmationCode("11111111-1111-1111-1111-111111111111", 1) {
		t.Fatalf("code = %q", a)
	}
	if a == s.ConfirmationCode("11111111-1111-1111-1111-111111111111", 2) || a == s.ConfirmationCode("22222222-2222-2222-2222-222222222222", 1) {
		t.Error("codes repeat across attempts or deliveries")
	}
	if (&Service{CodeKey: []byte("key-two")}).ConfirmationCode("11111111-1111-1111-1111-111111111111", 1) == a {
		t.Error("the code does not depend on the server key")
	}
	if !s.checkCode("11111111-1111-1111-1111-111111111111", 1, " "+a+" ") || s.checkCode("11111111-1111-1111-1111-111111111111", 1, "123456x") {
		t.Error("checkCode")
	}
}

func TestWebhookVerifierChecksSignatureAndReplayWindow(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	v := WebhookVerifier{Secret: []byte("partner-secret"), Now: func() time.Time { return now }}
	body := []byte(`{"event_id":"e1","status":"in_transit"}`)
	sign := func(ts int64, b []byte, secret string) string {
		m := hmac.New(sha256.New, []byte(secret))
		m.Write([]byte(strconv.FormatInt(ts, 10) + "."))
		m.Write(b)
		return "t=" + strconv.FormatInt(ts, 10) + ",v1=" + hex.EncodeToString(m.Sum(nil))
	}
	if err := v.Verify(sign(now.Unix(), body, "partner-secret"), body); err != nil {
		t.Fatalf("valid signature refused: %v", err)
	}
	if err := v.Verify(sign(now.Unix(), body, "other-partner"), body); !errors.Is(err, ErrBadSignature) {
		t.Errorf("another partner's secret: %v", err)
	}
	if err := v.Verify(sign(now.Unix(), body, "partner-secret"), []byte(`{"event_id":"e1","status":"delivered"}`)); !errors.Is(err, ErrBadSignature) {
		t.Errorf("tampered body: %v", err)
	}
	if err := v.Verify(sign(now.Add(-10*time.Minute).Unix(), body, "partner-secret"), body); !errors.Is(err, ErrStale) {
		t.Errorf("replayed old message: %v", err)
	}
	if err := v.Verify("garbage", body); !errors.Is(err, ErrBadSignature) {
		t.Errorf("garbage header: %v", err)
	}
	if err := (WebhookVerifier{}).Verify(sign(now.Unix(), body, ""), body); !errors.Is(err, ErrBadSignature) {
		t.Errorf("no secret configured must refuse: %v", err)
	}
}

func TestManualCarrierCapabilities(t *testing.T) {
	c := ManualCarrier{}
	if _, err := c.RequestStatus(nil, ShipmentRef{}); !errors.Is(err, ErrUnsupported) {
		t.Error("manual partners have no status API")
	}
	caps := c.Capabilities()
	if len(caps) != 2 || caps[0] != CapCreateShipment {
		t.Errorf("capabilities = %v", caps)
	}
}
