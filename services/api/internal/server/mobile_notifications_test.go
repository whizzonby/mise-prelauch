package server_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"mise.tt/api/internal/appnotify"
	"mise.tt/api/internal/payments/testpay"
)

// Sprint 10 Stage 9: the app's notification centre, push devices and
// account deletion requests.

type fakePusher struct {
	mu   sync.Mutex
	sent []appnotify.PushMessage
	dead map[string]bool
}

func (f *fakePusher) Send(_ context.Context, m appnotify.PushMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dead[m.Token] {
		return appnotify.ErrUnregistered
	}
	f.sent = append(f.sent, m)
	return nil
}

func (f *fakePusher) to(token string) []appnotify.PushMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []appnotify.PushMessage
	for _, m := range f.sent {
		if m.Token == token {
			out = append(out, m)
		}
	}
	return out
}

func (h *harness) registerDevice(tok reqOpt, installation, token string) {
	h.t.Helper()
	h.wantStatus(h.do("POST", "/api/v1/me/devices", map[string]any{
		"installation_id": installation, "platform": "android", "push_token": token, "app_version": "0.10.0+1",
	}, tok), http.StatusOK)
}

func TestNotificationCentreAndPushFromRealEvents(t *testing.T) {
	s := newShop(t)
	h := s.h
	web, addr := s.customer("pushy@example.com")
	g := h.mobileLogin("pushy@example.com", "install-push-0001")
	access, refresh := tokens(g)
	app := bearer(access)
	const token = "fcm-token-aaaaaaaaaaaaaaaaaaaaaaaa"
	h.registerDevice(app, "install-push-0001", token)

	sub, ref := s.subscribe(web, addr, s.basic, nil)
	s.pay(ref, "approve", testpay.CardApprove)
	h.drainJobs()

	res := h.do("GET", "/api/v1/me/notifications", nil, app)
	h.wantStatus(res, http.StatusOK)
	items := list(t, res.Data["items"])
	types := map[string]map[string]any{}
	for _, x := range items {
		n := obj(t, x)
		types[n["type"].(string)] = n
	}
	paid, ok := types["payment.succeeded"]
	if !ok {
		t.Fatalf("no payment notification: %s", res.Raw)
	}
	if paid["link"] != "/subscription" || paid["read_at"] != nil || res.Data["unread"].(float64) < 1 {
		t.Fatalf("notification = %v", paid)
	}

	// Push went out, with nothing sensitive on the lock screen.
	msgs := h.push.to(token)
	if len(msgs) == 0 {
		t.Fatal("no push sent")
	}
	for _, m := range msgs {
		if strings.Contains(m.Body, "TTD") || strings.Contains(m.Title+m.Body, "pushy") || m.Data["notification_id"] == "" {
			t.Fatalf("push leaks or lacks data: %+v", m)
		}
	}

	// Running the jobs again records nothing twice.
	before := h.count(`SELECT count(*) FROM customer_notifications`)
	h.drainJobs()
	if after := h.count(`SELECT count(*) FROM customer_notifications`); after != before {
		t.Fatalf("notifications %d → %d after a rerun", before, after)
	}

	// Read one, then all.
	h.wantStatus(h.do("POST", "/api/v1/me/notifications/"+paid["id"].(string)+"/read", nil, app), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/me/notifications/read-all", nil, app), http.StatusOK)
	if n := h.count(`SELECT count(*) FROM customer_notifications WHERE read_at IS NULL`); n != 0 {
		t.Fatalf("%d unread after read-all", n)
	}

	// Another customer cannot see or mark it.
	other := bearer(h.customer("nosy-neighbour@example.com"))
	h.wantStatus(h.do("POST", "/api/v1/me/notifications/"+paid["id"].(string)+"/read", nil, other), http.StatusNotFound)
	r := h.do("GET", "/api/v1/me/notifications", nil, other)
	h.wantStatus(r, http.StatusOK)
	if len(list(t, r.Data["items"])) != 0 {
		t.Fatalf("someone else's notifications visible: %s", r.Raw)
	}

	// Signing the phone out removes the device: later events reach the
	// centre but are not pushed to it.
	sent := len(h.push.to(token))
	h.wantStatus(h.do("POST", "/api/v1/auth/mobile/logout", map[string]string{"refresh_token": refresh}), http.StatusOK)
	h.wantStatus(h.do("POST", "/api/v1/me/subscriptions/"+sub+"/pause", map[string]any{"weeks": 1}, web), http.StatusOK)
	h.drainJobs()
	if got := len(h.push.to(token)); got != sent {
		t.Fatalf("pushed to a signed-out device (%d → %d)", sent, got)
	}
	if n := h.count(`SELECT count(*) FROM push_devices WHERE revoked_reason = 'logout'`); n != 1 {
		t.Fatalf("device not revoked at logout (%d)", n)
	}
	if n := h.count(`SELECT count(*) FROM customer_notifications WHERE type = 'subscription.changed'`); n != 1 {
		t.Fatalf("pause not recorded in the centre (%d)", n)
	}
}

func TestPushTokenRotationAndInvalidTokenRemoval(t *testing.T) {
	s := newShop(t)
	h := s.h
	web, addr := s.customer("rotate@example.com")
	access, _ := tokens(h.mobileLogin("rotate@example.com", "install-rot-0001"))
	app := bearer(access)
	h.registerDevice(app, "install-rot-0001", "fcm-old-token-aaaaaaaaaaaaaaaa")
	// Rotation replaces the registration; only one stays live.
	h.registerDevice(app, "install-rot-0001", "fcm-new-token-bbbbbbbbbbbbbbbb")
	if n := h.count(`SELECT count(*) FROM push_devices WHERE revoked_at IS NULL`); n != 1 {
		t.Fatalf("live devices = %d", n)
	}
	h.push.mu.Lock()
	h.push.dead = map[string]bool{"fcm-new-token-bbbbbbbbbbbbbbbb": true}
	h.push.mu.Unlock()
	_, ref := s.subscribe(web, addr, s.basic, nil)
	s.pay(ref, "approve", testpay.CardApprove)
	h.drainJobs()
	if n := h.count(`SELECT count(*) FROM push_devices WHERE revoked_reason = 'unregistered'`); n != 1 {
		t.Fatalf("unregistered token not removed (%d)", n)
	}
	// Bad input is refused.
	bad := h.do("POST", "/api/v1/me/devices", map[string]any{"installation_id": "x", "platform": "windows", "push_token": "short"}, app)
	h.wantError(bad, http.StatusUnprocessableEntity, "validation_failed")
	// Guests cannot register devices.
	h.wantStatus(h.do("POST", "/api/v1/me/devices", map[string]any{"installation_id": "install-guest-01", "platform": "ios", "push_token": "fcm-guest-token-cccccccccccc"}), http.StatusUnauthorized)
}

func TestAccountDeletionRequest(t *testing.T) {
	h := newHarness(t)
	tok := bearer(h.customer("leaving@example.com"))

	h.wantError(h.do("POST", "/api/v1/me/account/deletion-request", map[string]any{"reason": "Moving abroad"}, tok), http.StatusUnprocessableEntity, "validation_failed")
	res := h.do("POST", "/api/v1/me/account/deletion-request", map[string]any{"reason": "Moving abroad", "acknowledged": true}, tok)
	h.wantStatus(res, http.StatusCreated)
	if obj(t, res.Data["request"])["status"] != "requested" || !strings.Contains(res.Data["notice"].(string), "Nothing is deleted yet") {
		t.Fatalf("request = %s", res.Raw)
	}
	h.wantError(h.do("POST", "/api/v1/me/account/deletion-request", map[string]any{"acknowledged": true}, tok), http.StatusConflict, "deletion_already_requested")
	// Recording a request deletes nothing.
	if n := h.count(`SELECT count(*) FROM users WHERE email = 'leaving@example.com' AND status = 'active'`); n != 1 {
		t.Fatal("the account changed on request")
	}
	if n := h.count(`SELECT count(*) FROM audit_log WHERE action = 'account.deletion_requested'`); n != 1 {
		t.Fatal("request not audited")
	}

	// Staff see the queue with customers.read; customers do not.
	support := bearer(h.adminLogin("privacy-desk@mise.test", "support"))
	list0 := h.do("GET", "/api/v1/admin/account-deletion-requests", nil, support)
	h.wantStatus(list0, http.StatusOK)
	if len(list(t, list0.Data["items"])) != 1 {
		t.Fatalf("queue = %s", list0.Raw)
	}
	h.wantStatus(h.do("GET", "/api/v1/admin/account-deletion-requests", nil, tok), http.StatusUnauthorized)

	h.wantStatus(h.do("DELETE", "/api/v1/me/account/deletion-request", nil, tok), http.StatusOK)
	h.wantError(h.do("DELETE", "/api/v1/me/account/deletion-request", nil, tok), http.StatusConflict, "nothing_to_withdraw")
	got := h.do("GET", "/api/v1/me/account/deletion-request", nil, tok)
	h.wantStatus(got, http.StatusOK)
	if obj(t, got.Data["request"])["status"] != "withdrawn" {
		t.Fatalf("after withdrawal: %s", got.Raw)
	}
}

func TestFCMAdapterAgainstAFakeGoogle(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))

	var tokenCalls int
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			_ = r.ParseForm()
			if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || strings.Count(r.Form.Get("assertion"), ".") != 2 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			tokenCalls++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"access_token":"ya29.fake","expires_in":3600}`))
		case strings.HasSuffix(r.URL.Path, "/messages:send"):
			if r.Header.Get("Authorization") != "Bearer ya29.fake" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			mu.Lock()
			bodies = append(bodies, b)
			mu.Unlock()
			msg := b["message"].(map[string]any)
			switch msg["token"] {
			case "gone":
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"status":"NOT_FOUND","details":[{"errorCode":"UNREGISTERED"}]}}`))
			case "garbage":
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"status":"INVALID_ARGUMENT"}}`))
			default:
				_, _ = w.Write([]byte(`{"name":"projects/p/messages/1"}`))
			}
		}
	}))
	defer srv.Close()

	sa, _ := json.Marshal(map[string]string{"project_id": "mise-test", "client_email": "push@mise-test.iam.gserviceaccount.com", "private_key": pemKey, "token_uri": srv.URL + "/token"})
	f, err := appnotify.ParseFCM(sa)
	if err != nil {
		t.Fatal(err)
	}
	f.SendURL = srv.URL + "/v1/projects/mise-test/messages:send"
	ctx := context.Background()
	m := appnotify.PushMessage{Token: "good", Platform: "android", Title: "Mise", Body: "Update", Data: map[string]string{"type": "chat.reply"}, CollapseKey: "n1"}
	if err := f.Send(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := f.Send(ctx, m); err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 1 {
		t.Fatalf("access token fetched %d times, want 1 (cached)", tokenCalls)
	}
	m.Token = "gone"
	if err := f.Send(ctx, m); !errors.Is(err, appnotify.ErrUnregistered) {
		t.Fatalf("gone token: %v", err)
	}
	m.Token = "garbage"
	if err := f.Send(ctx, m); !errors.Is(err, appnotify.ErrInvalidToken) {
		t.Fatalf("bad token: %v", err)
	}
	android := bodies[0]["message"].(map[string]any)["android"].(map[string]any)["notification"].(map[string]any)
	if android["visibility"] != "PRIVATE" {
		t.Fatalf("android visibility = %v", android["visibility"])
	}
	if _, err := appnotify.ParseFCM([]byte(`{"project_id":"x"}`)); err == nil {
		t.Fatal("incomplete service account accepted")
	}
}
