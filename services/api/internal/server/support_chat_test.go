package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mise.tt/api/internal/supportchat"
)

// Sprint 9.1: Mise Chat (guests, customers, the agent inbox, live delivery
// over SSE, reply emails) and the help centre.

func guestTok(tok string) reqOpt {
	return func(r *http.Request) { r.Header.Set(supportchat.GuestHeader, tok) }
}

type chatGuest struct {
	token, conv string
}

func startGuest(t *testing.T, h *harness, name, email, body string) chatGuest {
	t.Helper()
	res := h.do("POST", "/api/v1/chat/guest/conversations", map[string]any{"name": name, "email": email, "body": body,
		"client_message_id": "first-" + strings.ReplaceAll(strings.Split(email, "@")[0], ".", "")})
	h.wantStatus(res, http.StatusCreated)
	g := obj(t, res.Data["guest"])
	c := obj(t, res.Data["conversation"])
	return chatGuest{token: g["token"].(string), conv: c["id"].(string)}
}

func TestGuestChatAndAgentReply(t *testing.T) {
	h := newHarness(t)
	old := supportchat.EmailDelay
	supportchat.EmailDelay = "0 seconds"
	t.Cleanup(func() { supportchat.EmailDelay = old })
	agent := bearer(h.adminLogin("agent@mise.test", "support"))
	agent2 := bearer(h.adminLogin("agent2@mise.test", "support"))
	marketing := bearer(h.adminLogin("marketer@mise.test", "marketing"))

	// Validation and the public availability (nothing promised by default).
	res := h.do("POST", "/api/v1/chat/guest/conversations", map[string]any{"name": "", "email": "nope", "body": ""})
	h.wantStatus(res, http.StatusUnprocessableEntity)
	if len(res.Error.Fields) < 3 {
		t.Fatalf("fields = %+v", res.Error.Fields)
	}
	res = h.do("GET", "/api/v1/chat/availability", nil)
	h.wantStatus(res, http.StatusOK)
	if res.Data["online"] != nil || res.Data["response_expectation"] != supportchat.DefaultExpectation {
		t.Fatalf("availability = %v", res.Data)
	}

	g := startGuest(t, h, "Ayesha", "ayesha@example.com", "Do you deliver to Arima?")
	other := startGuest(t, h, "Brent", "brent@example.com", "Hello")
	if !strings.HasPrefix(g.token, "mgt_") {
		t.Fatalf("token = %q", g.token)
	}
	var stored []byte
	_ = h.pool.QueryRow(context.Background(), `SELECT token_hash FROM support_guest_sessions WHERE email = 'ayesha@example.com'`).Scan(&stored)
	if strings.Contains(string(stored), g.token) || len(stored) != 32 {
		t.Fatal("the guest token is stored in clear")
	}

	// A guest sees only their own conversations; bad tokens are refused.
	h.wantError(h.do("GET", "/api/v1/chat/guest/conversations", nil), http.StatusUnauthorized, "guest_session_invalid")
	h.wantError(h.do("GET", "/api/v1/chat/guest/conversations", nil, guestTok("mgt_made-up")), http.StatusUnauthorized, "guest_session_invalid")
	h.wantStatus(h.do("GET", "/api/v1/chat/guest/conversations/"+other.conv, nil, guestTok(g.token)), http.StatusNotFound)
	h.wantStatus(h.do("POST", "/api/v1/chat/guest/conversations/"+other.conv+"/messages", map[string]any{"body": "hi"}, guestTok(g.token)), http.StatusNotFound)
	res = h.do("GET", "/api/v1/chat/guest/conversations", nil, guestTok(g.token))
	h.wantStatus(res, http.StatusOK)
	if items := list(t, res.Data["items"]); len(items) != 1 || obj(t, items[0])["id"] != g.conv {
		t.Fatalf("guest list = %v", items)
	}

	// Sending is idempotent by client_message_id.
	send := func(body, cid string) map[string]any {
		r := h.do("POST", "/api/v1/chat/guest/conversations/"+g.conv+"/messages", map[string]any{"body": body, "client_message_id": cid}, guestTok(g.token))
		h.wantStatus(r, http.StatusCreated)
		return r.Data
	}
	m1 := send("Also, is there a vegetarian plan?", "msg-000001")
	m2 := send("Also, is there a vegetarian plan?", "msg-000001")
	if m1["seq"] != m2["seq"] || m1["seq"].(float64) != 2 {
		t.Fatalf("resend made a new message: %v vs %v", m1, m2)
	}

	// The inbox: unassigned, waiting; an alert is raised once; marketing cannot read chats.
	h.wantStatus(h.do("GET", "/api/v1/admin/chat/conversations", nil, marketing), http.StatusForbidden)
	res = h.do("GET", "/api/v1/admin/chat/conversations?view=unassigned", nil, agent)
	h.wantStatus(res, http.StatusOK)
	if n := len(list(t, res.Data["items"])); n != 2 {
		t.Fatalf("unassigned = %d", n)
	}
	var alerts int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM admin_alerts WHERE dedupe_key = $1 AND resolved_at IS NULL`, "support_chat.unassigned:"+g.conv).Scan(&alerts)
	if alerts != 1 {
		t.Fatalf("open alerts = %d", alerts)
	}

	// Internal note: visible to agents, never to the guest.
	h.wantStatus(h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/notes", map[string]any{"body": "SECRET-NOTE check zone list"}, agent), http.StatusCreated)
	h.wantStatus(h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/notes", map[string]any{"body": "x"}, marketing), http.StatusForbidden)

	// Agent replies: assigned to them, waiting for the customer, alert cleared, first response recorded.
	res = h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/messages", map[string]any{"body": "Yes, we deliver to Arima.", "client_message_id": "agent-0001"}, agent)
	h.wantStatus(res, http.StatusCreated)
	if res.Data["author"] != "agent" || res.Data["seq"].(float64) != 3 {
		t.Fatalf("reply = %v", res.Data)
	}
	h.wantStatus(h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/messages", map[string]any{"body": "And yes to vegetarian."}, agent), http.StatusCreated)
	res = h.do("GET", "/api/v1/admin/chat/conversations/"+g.conv, nil, agent)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "pending" || res.Data["assignee_id"] == nil || res.Data["first_human_response_at"] == nil || res.Data["notes_visible"] != true {
		t.Fatalf("detail = %v", res.Data)
	}
	if len(list(t, res.Data["notes"])) != 1 || obj(t, res.Data["contact"])["email"] != "ayesha@example.com" {
		t.Fatalf("notes/contact = %v / %v", res.Data["notes"], res.Data["contact"])
	}
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM admin_alerts WHERE dedupe_key = $1 AND resolved_at IS NULL`, "support_chat.unassigned:"+g.conv).Scan(&alerts)
	if alerts != 0 {
		t.Fatal("the unassigned alert stayed open after a reply")
	}

	// The guest sees the replies (agent's first name), never the note.
	res = h.do("GET", "/api/v1/chat/guest/conversations/"+g.conv+"/messages?after=2", nil, guestTok(g.token))
	h.wantStatus(res, http.StatusOK)
	msgs := list(t, res.Data["items"])
	if len(msgs) != 2 || obj(t, msgs[0])["author_name"] == "" || strings.Contains(fmt.Sprint(obj(t, msgs[0])["author_name"]), "@") {
		t.Fatalf("guest messages = %v", msgs)
	}
	for _, path := range []string{"/api/v1/chat/guest/conversations/" + g.conv, "/api/v1/chat/guest/conversations/" + g.conv + "/messages",
		"/api/v1/chat/guest/conversations"} {
		if r := h.do("GET", path, nil, guestTok(g.token)); strings.Contains(string(r.Raw), "SECRET-NOTE") {
			t.Fatalf("internal note leaked through %s", path)
		}
	}
	res = h.do("GET", "/api/v1/chat/guest/conversations/"+g.conv, nil, guestTok(g.token))
	if res.Data["unread"].(float64) != 2 || res.Data["status"] != "pending" {
		t.Fatalf("guest view = %v", res.Data)
	}

	// Two replies in one window: one email, no message text in it.
	h.drainJobs()
	var emails int
	h.mailer.mu.Lock()
	for _, m := range h.mailer.sent {
		if m.To == "ayesha@example.com" {
			emails++
			if strings.Contains(m.Text, "Arima") || strings.Contains(m.Text, "vegetarian") || strings.Contains(m.Text, g.token) {
				t.Errorf("reply email carries message text or the token: %s", m.Text)
			}
		}
	}
	h.mailer.mu.Unlock()
	if emails != 1 {
		t.Fatalf("reply emails = %d, want 1", emails)
	}

	// Reading the conversation clears unread and shows the team a read receipt.
	h.wantStatus(h.do("POST", "/api/v1/chat/guest/conversations/"+g.conv+"/read", map[string]any{"seq": 99}, guestTok(g.token)), http.StatusOK)
	if r := h.do("GET", "/api/v1/chat/guest/conversations/"+g.conv, nil, guestTok(g.token)); r.Data["unread"].(float64) != 0 {
		t.Fatalf("unread after read = %v", r.Data["unread"])
	}
	if r := h.do("GET", "/api/v1/admin/chat/conversations/"+g.conv, nil, agent); r.Data["owner_read_seq"].(float64) != 4 {
		t.Fatalf("owner read seq = %v (read past the end is capped)", r.Data["owner_read_seq"])
	}

	// A reply the guest has already read by the time the email is due: skipped.
	_, _ = h.pool.Exec(context.Background(), `UPDATE support_chat_notifications SET dedupe_key = dedupe_key || '-old'`) // a new window
	h.wantStatus(h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/messages", map[string]any{"body": "Anything else?"}, agent), http.StatusCreated)
	h.wantStatus(h.do("POST", "/api/v1/chat/guest/conversations/"+g.conv+"/read", map[string]any{"seq": 5}, guestTok(g.token)), http.StatusOK)
	h.drainJobs()
	var skipped int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM support_chat_notifications WHERE status = 'skipped'`).Scan(&skipped)
	if skipped != 1 {
		t.Fatalf("skipped emails = %d", skipped)
	}

	// Transfer, assignment rules, resolve, the guest reopens by writing, feedback.
	res = h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/assign", map[string]any{"assignee_id": "00000000-0000-0000-0000-000000000000"}, agent)
	h.wantStatus(res, http.StatusUnprocessableEntity)
	if r := h.do("GET", "/api/v1/admin/chat/agents", nil, agent); len(list(t, r.Data["items"])) != 2 {
		t.Fatalf("agents = %v (the marketer cannot reply, so is not one)", r.Data["items"])
	}
	var agent2ID string
	var marketerID string
	_ = h.pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = 'marketer@mise.test'`).Scan(&marketerID)
	_ = h.pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = 'agent2@mise.test'`).Scan(&agent2ID)
	h.wantStatus(h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/transfer", map[string]any{"assignee_id": marketerID}, agent), http.StatusUnprocessableEntity)
	res = h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/transfer", map[string]any{"assignee_id": agent2ID, "note": "Agent 2 handles Arima"}, agent)
	h.wantStatus(res, http.StatusOK)
	if res.Data["assignee_id"] != agent2ID {
		t.Fatalf("after transfer = %v", res.Data["assignee_id"])
	}
	if r := h.do("GET", "/api/v1/admin/chat/conversations?view=mine", nil, agent2); len(list(t, r.Data["items"])) != 1 {
		t.Fatalf("agent2 mine = %v", r.Data["items"])
	}
	res = h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/resolve", map[string]any{"note": "answered"}, agent2)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "resolved" {
		t.Fatalf("status = %v", res.Data["status"])
	}
	h.wantStatus(h.do("POST", "/api/v1/chat/guest/conversations/"+g.conv+"/feedback", map[string]any{"rating": 9}, guestTok(g.token)), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("POST", "/api/v1/chat/guest/conversations/"+g.conv+"/feedback", map[string]any{"rating": 5, "comment": "Quick!"}, guestTok(g.token)), http.StatusOK)
	send("One more question", "msg-000002")
	res = h.do("GET", "/api/v1/admin/chat/conversations/"+g.conv, nil, agent)
	if res.Data["status"] != "open" {
		t.Fatalf("a message did not reopen the conversation: %v", res.Data["status"])
	}
	types := []string{}
	for _, e := range list(t, res.Data["events"]) {
		types = append(types, obj(t, e)["type"].(string))
	}
	if strings.Join(types, ",") != "created,assigned,transferred,resolved,reopened" {
		t.Fatalf("events = %v", types)
	}
	if _, err := h.pool.Exec(context.Background(), `UPDATE support_messages SET body = 'edited'`); err == nil {
		t.Fatal("messages can be edited")
	}

	// Escalate to a support case: once.
	h.wantStatus(h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/escalate", map[string]any{"category": "delivery"}, marketing), http.StatusForbidden)
	res = h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/escalate", map[string]any{"category": "delivery"}, agent)
	h.wantStatus(res, http.StatusOK)
	if res.Data["case_id"] == nil {
		t.Fatalf("no case linked: %v", res.Data)
	}
	h.wantError(h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/escalate", map[string]any{"category": "delivery"}, agent), http.StatusConflict, "already_escalated")

	// Priority and tags; a guest conversation cannot be linked to an account's order.
	res = h.do("PATCH", "/api/v1/admin/chat/conversations/"+g.conv, map[string]any{"priority": "high", "tags": []string{"Delivery Area", "delivery area", "vegetarian"}}, agent)
	h.wantStatus(res, http.StatusOK)
	if res.Data["priority"] != "high" || fmt.Sprint(res.Data["tags"]) != "[delivery area vegetarian]" {
		t.Fatalf("update = %v %v", res.Data["priority"], res.Data["tags"])
	}
	h.wantStatus(h.do("PATCH", "/api/v1/admin/chat/conversations/"+g.conv, map[string]any{"order_id": "00000000-0000-0000-0000-000000000001"}, agent), http.StatusUnprocessableEntity)

	// Search.
	if r := h.do("GET", "/api/v1/admin/chat/conversations?view=all&q=arima", nil, agent); len(list(t, r.Data["items"])) != 1 {
		t.Fatalf("search = %v", r.Data["items"])
	}

	// Guest starts are limited per email address.
	for i := 0; i < 4; i++ {
		h.do("POST", "/api/v1/chat/guest/conversations", map[string]any{"name": "Spam", "email": "spam@example.com", "body": "hi"})
	}
	h.do("POST", "/api/v1/chat/guest/conversations", map[string]any{"name": "Spam", "email": "spam@example.com", "body": "hi"})
	h.wantError(h.do("POST", "/api/v1/chat/guest/conversations", map[string]any{"name": "Spam", "email": "spam@example.com", "body": "hi"}),
		http.StatusTooManyRequests, "rate_limited")
}

func TestCustomerChatIsolationAndGuestLinking(t *testing.T) {
	h := newHarness(t)
	agent := bearer(h.adminLogin("agent@mise.test", "support"))
	finance := bearer(h.adminLogin("fin@mise.test", "finance"))
	alice := bearer(h.customer("alice@example.com"))
	bob := bearer(h.customer("bob@example.com"))

	res := h.do("POST", "/api/v1/me/chat/conversations", map[string]any{"body": "Where is my order?", "client_message_id": "alice-0001"}, alice)
	h.wantStatus(res, http.StatusCreated)
	conv := obj(t, res.Data["conversation"])["id"].(string)
	// The same start retried returns the same conversation.
	res = h.do("POST", "/api/v1/me/chat/conversations", map[string]any{"body": "Where is my order?", "client_message_id": "alice-0001"}, alice)
	h.wantStatus(res, http.StatusCreated)
	if obj(t, res.Data["conversation"])["id"] != conv {
		t.Fatal("a retried start made a second conversation")
	}
	if r := h.do("GET", "/api/v1/me/chat/conversations", nil, alice); len(list(t, r.Data["items"])) != 1 {
		t.Fatalf("alice has %v", r.Data["items"])
	}

	// Bob cannot see or write to Alice's conversation, through any route.
	for _, path := range []string{"", "/messages"} {
		h.wantStatus(h.do("GET", "/api/v1/me/chat/conversations/"+conv+path, nil, bob), http.StatusNotFound)
	}
	h.wantStatus(h.do("POST", "/api/v1/me/chat/conversations/"+conv+"/messages", map[string]any{"body": "hi"}, bob), http.StatusNotFound)
	h.wantStatus(h.do("GET", "/api/v1/me/chat/conversations/"+conv+"/stream", nil, bob), http.StatusNotFound)
	h.wantStatus(h.do("GET", "/api/v1/me/chat/conversations", nil), http.StatusUnauthorized)

	// Customer email is shown to staff with customers.read only (finance has no chat access at all).
	h.wantStatus(h.do("GET", "/api/v1/admin/chat/conversations/"+conv, nil, finance), http.StatusForbidden)
	res = h.do("GET", "/api/v1/admin/chat/conversations/"+conv, nil, agent)
	if obj(t, res.Data["contact"])["email"] != "alice@example.com" || obj(t, res.Data["contact"])["kind"] != "customer" {
		t.Fatalf("contact = %v", res.Data["contact"])
	}

	// Guest linking: only with the token, a verified account and the same email.
	g := startGuest(t, h, "Alice", "alice@example.com", "Before I sign up: do you deliver to Chaguanas?")
	h.wantStatus(h.do("POST", "/api/v1/me/chat/link-guest", nil, alice), http.StatusUnauthorized)
	h.wantError(h.do("POST", "/api/v1/me/chat/link-guest", nil, bob, guestTok(g.token)), http.StatusForbidden, "email_mismatch")
	res = h.do("POST", "/api/v1/me/chat/link-guest", nil, alice, guestTok(g.token))
	h.wantStatus(res, http.StatusOK)
	if res.Data["linked_conversations"].(float64) != 1 {
		t.Fatalf("linked = %v", res.Data)
	}
	if r := h.do("GET", "/api/v1/me/chat/conversations", nil, alice); len(list(t, r.Data["items"])) != 2 {
		t.Fatalf("after linking alice has %v", r.Data["items"])
	}
	// The guest token no longer works: the history now lives in the account.
	h.wantStatus(h.do("GET", "/api/v1/chat/guest/conversations", nil, guestTok(g.token)), http.StatusUnauthorized)

	// (An unverified account cannot sign in at all while email verification is required, so the
	// service's own email_not_verified guard is the second line of defence.)
}

// readSSE reads events from a stream until want returns true or the timeout passes.
type sseEvent struct{ id, name, data string }

func readSSE(t *testing.T, sc *bufio.Scanner, events chan sseEvent) {
	t.Helper()
	go func() {
		var e sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if e.name != "" {
					events <- e
				}
				e = sseEvent{}
			case strings.HasPrefix(line, "id: "):
				e.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				e.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				e.data = strings.TrimPrefix(line, "data: ")
			}
		}
		close(events)
	}()
}

func waitEvent(t *testing.T, events chan sseEvent, name string, match func(sseEvent) bool) sseEvent {
	t.Helper()
	deadline := time.After(8 * time.Second)
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatalf("stream ended while waiting for %s", name)
			}
			if e.name == name && (match == nil || match(e)) {
				return e
			}
		case <-deadline:
			t.Fatalf("no %s event within 8 seconds", name)
		}
	}
}

func openStream(t *testing.T, srv *httptest.Server, path string, opts ...reqOpt) (*http.Response, chan sseEvent) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+path, nil)
	for _, o := range opts {
		o(req)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	events := make(chan sseEvent, 64)
	if res.StatusCode == http.StatusOK {
		readSSE(t, bufio.NewScanner(res.Body), events)
	}
	return res, events
}

func TestChatLiveDeliveryOverSSE(t *testing.T) {
	h := newHarness(t)
	srv := httptest.NewServer(h.handler)
	t.Cleanup(srv.Close)
	agent := bearer(h.adminLogin("agent@mise.test", "support"))
	marketing := bearer(h.adminLogin("marketer@mise.test", "marketing"))
	g := startGuest(t, h, "Dana", "dana@example.com", "Hello?")
	other := startGuest(t, h, "Eve", "eve@example.com", "Hi")
	h.wantStatus(h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/messages", map[string]any{"body": "Hi Dana, how can we help?"}, agent), http.StatusCreated)

	// Unauthorised subscriptions are refused before any event is sent.
	if res, _ := openStream(t, srv, "/api/v1/chat/guest/conversations/"+other.conv+"/stream", guestTok(g.token)); res.StatusCode != http.StatusNotFound {
		t.Fatalf("other guest's stream = %d", res.StatusCode)
	}
	if res, _ := openStream(t, srv, "/api/v1/admin/chat/stream", marketing); res.StatusCode != http.StatusForbidden {
		t.Fatalf("marketing admin stream = %d", res.StatusCode)
	}

	// Reconnect after seq 1: the missed agent reply (seq 2) arrives first, then live ones.
	res, events := openStream(t, srv, "/api/v1/chat/guest/conversations/"+g.conv+"/stream", guestTok(g.token), func(r *http.Request) { r.Header.Set("Last-Event-ID", "1") })
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream = %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	e := waitEvent(t, events, "message", nil)
	if e.id != "2" || !strings.Contains(e.data, "Hi Dana") {
		t.Fatalf("catch-up = %+v", e)
	}
	waitEvent(t, events, "conversation", nil)

	_, adminEvents := openStream(t, srv, "/api/v1/admin/chat/stream", agent)
	waitEvent(t, adminEvents, "ready", nil)

	// A live agent reply and typing reach the guest; the guest's message reaches the inbox.
	h.wantStatus(h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/typing", nil, agent), http.StatusOK)
	waitEvent(t, events, "typing", nil)
	h.wantStatus(h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/messages", map[string]any{"body": "We deliver island-wide on Saturdays."}, agent), http.StatusCreated)
	e = waitEvent(t, events, "message", func(e sseEvent) bool { return e.id == "3" })
	var m map[string]any
	_ = json.Unmarshal([]byte(e.data), &m)
	if m["author"] != "agent" || m["body"] != "We deliver island-wide on Saturdays." {
		t.Fatalf("live message = %v", m)
	}
	h.wantStatus(h.do("POST", "/api/v1/chat/guest/conversations/"+g.conv+"/messages", map[string]any{"body": "Great, thanks"}, guestTok(g.token)), http.StatusCreated)
	waitEvent(t, adminEvents, "message", func(e sseEvent) bool { return strings.Contains(e.data, g.conv) })
	// The guest's own message comes back on the guest stream too (other tabs stay in step).
	waitEvent(t, events, "message", func(e sseEvent) bool { return e.id == "4" })

	// An internal note signals staff, but the guest stream never carries it.
	h.wantStatus(h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/notes", map[string]any{"body": "SECRET-NOTE"}, agent), http.StatusCreated)
	waitEvent(t, adminEvents, "status", nil)
	h.wantStatus(h.do("POST", "/api/v1/admin/chat/conversations/"+g.conv+"/resolve", nil, agent), http.StatusOK)
	e = waitEvent(t, events, "conversation", func(e sseEvent) bool { return strings.Contains(e.data, `"resolved"`) })
	if strings.Contains(e.data, "SECRET-NOTE") {
		t.Fatal("internal note on the guest stream")
	}
}

func TestChatSettingsAndAvailability(t *testing.T) {
	h := newHarness(t)
	admin := bearer(h.adminLogin("admin@mise.test", "admin"))
	agent := bearer(h.adminLogin("agent@mise.test", "support"))
	h.wantStatus(h.do("GET", "/api/v1/admin/chat/settings", nil, agent), http.StatusForbidden)

	base := map[string]any{"enabled": true, "timezone": "America/Port_of_Spain", "business_hours": []any{}, "holidays": []any{},
		"online_message": "", "offline_message": "We're away", "response_expectation": "", "email_cooldown_minutes": 30}
	put := func(changes map[string]any) response {
		body := map[string]any{}
		for k, v := range base {
			body[k] = v
		}
		for k, v := range changes {
			body[k] = v
		}
		return h.do("PUT", "/api/v1/admin/chat/settings", body, admin)
	}
	h.wantStatus(put(map[string]any{"timezone": "Mars/Base"}), http.StatusUnprocessableEntity)
	h.wantStatus(put(map[string]any{"business_hours": []any{map[string]any{"day": 1, "open": "17:00", "close": "09:00"}}}), http.StatusUnprocessableEntity)
	h.wantStatus(put(map[string]any{"email_cooldown_minutes": 1}), http.StatusUnprocessableEntity)

	all := []any{}
	for d := 0; d < 7; d++ {
		all = append(all, map[string]any{"day": d, "open": "00:00", "close": "23:59"})
	}
	h.wantStatus(put(map[string]any{"business_hours": all, "online_message": "We're here"}), http.StatusOK)
	res := h.do("GET", "/api/v1/chat/availability", nil)
	if res.Data["online"] != true || res.Data["message"] != "We're here" {
		t.Fatalf("open all week = %v", res.Data)
	}
	today := time.Now().In(mustLoc(t, "America/Port_of_Spain")).Format("2006-01-02")
	h.wantStatus(put(map[string]any{"business_hours": all, "holidays": []any{map[string]any{"day": today, "name": "Test holiday"}}}), http.StatusOK)
	res = h.do("GET", "/api/v1/chat/availability", nil)
	if res.Data["online"] != false || res.Data["message"] != "We're away" {
		t.Fatalf("holiday = %v", res.Data)
	}
	// Chat switched off: new conversations are refused.
	h.wantStatus(put(map[string]any{"enabled": false}), http.StatusOK)
	h.wantError(h.do("POST", "/api/v1/chat/guest/conversations", map[string]any{"name": "A", "email": "a@example.com", "body": "hi"}),
		http.StatusServiceUnavailable, "chat_unavailable")

	// Saved replies: agents read, only managers write.
	h.wantStatus(h.do("POST", "/api/v1/admin/chat/saved-replies", map[string]any{"title": "Hours", "body": "x"}, agent), http.StatusForbidden)
	res = h.do("POST", "/api/v1/admin/chat/saved-replies", map[string]any{"title": "Delivery days", "body": "We deliver on the days shown in your account."}, admin)
	h.wantStatus(res, http.StatusCreated)
	id := res.Data["id"].(string)
	if r := h.do("GET", "/api/v1/admin/chat/saved-replies", nil, agent); len(list(t, r.Data["items"])) != 1 {
		t.Fatalf("saved replies = %v", r.Data["items"])
	}
	h.wantStatus(h.do("PATCH", "/api/v1/admin/chat/saved-replies/"+id, map[string]any{"archived": true}, admin), http.StatusOK)
	if r := h.do("GET", "/api/v1/admin/chat/saved-replies", nil, agent); len(list(t, r.Data["items"])) != 0 {
		t.Fatalf("archived reply still listed: %v", r.Data["items"])
	}

	// The support analytics dashboard: counts, available to admins, not to agents.
	h.wantStatus(h.do("GET", "/api/v1/admin/analytics/support", nil, admin), http.StatusOK)
	h.wantStatus(h.do("GET", "/api/v1/admin/analytics/support", nil, agent), http.StatusForbidden)
}

func TestHelpCentreWorkflow(t *testing.T) {
	h := newHarness(t)
	agent := bearer(h.adminLogin("agent@mise.test", "support"))
	admin := bearer(h.adminLogin("admin@mise.test", "admin"))
	finance := bearer(h.adminLogin("fin@mise.test", "finance"))

	res := h.do("GET", "/api/v1/help/categories", nil)
	h.wantStatus(res, http.StatusOK)
	if n := len(list(t, res.Data["items"])); n != 5 {
		t.Fatalf("categories = %d", n)
	}
	if r := h.do("GET", "/api/v1/help/articles", nil); len(list(t, r.Data["items"])) != 0 {
		t.Fatal("articles exist before any were written")
	}

	h.wantStatus(h.do("POST", "/api/v1/admin/help/articles", map[string]any{"slug": "x", "title": "X title"}, finance), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/help/articles", map[string]any{"slug": "Bad Slug", "category": "delivery", "title": "Hi"}, agent), http.StatusUnprocessableEntity)
	h.wantStatus(h.do("POST", "/api/v1/admin/help/articles", map[string]any{"slug": "html", "category": "delivery", "title": "Has html",
		"body": "<script>alert(1)</script>"}, agent), http.StatusUnprocessableEntity)
	res = h.do("POST", "/api/v1/admin/help/articles", map[string]any{"slug": "changing-your-delivery-address", "category": "delivery",
		"title": "Changing your delivery address", "summary": "Where to update it.", "body": "Open Subscription, then Manage."}, agent)
	h.wantStatus(res, http.StatusCreated)
	id := res.Data["id"].(string)
	h.wantError(h.do("POST", "/api/v1/admin/help/articles", map[string]any{"slug": "changing-your-delivery-address", "category": "delivery",
		"title": "Duplicate"}, agent), http.StatusUnprocessableEntity, "validation_failed")

	// Not public until published; the writer cannot publish, a publisher can only publish a reviewed article.
	h.wantStatus(h.do("GET", "/api/v1/help/articles/changing-your-delivery-address", nil), http.StatusNotFound)
	h.wantError(h.do("POST", "/api/v1/admin/help/articles/"+id+"/publish", nil, admin), http.StatusConflict, "invalid_transition")
	h.wantStatus(h.do("POST", "/api/v1/admin/help/articles/"+id+"/submit", nil, agent), http.StatusOK)
	h.wantError(h.do("PATCH", "/api/v1/admin/help/articles/"+id, map[string]any{"title": "Edited in review"}, agent), http.StatusConflict, "not_a_draft")
	h.wantStatus(h.do("POST", "/api/v1/admin/help/articles/"+id+"/publish", nil, agent), http.StatusForbidden)
	h.wantStatus(h.do("POST", "/api/v1/admin/help/articles/"+id+"/publish", map[string]any{"note": "Reviewed"}, admin), http.StatusOK)

	res = h.do("GET", "/api/v1/help/articles?q=address", nil)
	if items := list(t, res.Data["items"]); len(items) != 1 || obj(t, items[0])["slug"] != "changing-your-delivery-address" {
		t.Fatalf("search = %v", res.Data["items"])
	}
	if r := h.do("GET", "/api/v1/help/articles?category=billing-nope", nil); len(list(t, r.Data["items"])) != 0 {
		t.Fatal("unknown category returned articles")
	}
	res = h.do("GET", "/api/v1/help/articles/changing-your-delivery-address", nil)
	h.wantStatus(res, http.StatusOK)
	if res.Data["body"] != "Open Subscription, then Manage." {
		t.Fatalf("article = %v", res.Data)
	}

	// Revising takes it off the site until it is reviewed and published again.
	res = h.do("POST", "/api/v1/admin/help/articles/"+id+"/revise", nil, agent)
	h.wantStatus(res, http.StatusOK)
	if res.Data["status"] != "draft" || res.Data["version"].(float64) != 2 {
		t.Fatalf("revised = %v", res.Data)
	}
	h.wantStatus(h.do("GET", "/api/v1/help/articles/changing-your-delivery-address", nil), http.StatusNotFound)
	res = h.do("GET", "/api/v1/admin/help/articles/"+id, nil, agent)
	if n := len(list(t, res.Data["history"])); n != 4 {
		t.Fatalf("history = %d entries", n)
	}
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
