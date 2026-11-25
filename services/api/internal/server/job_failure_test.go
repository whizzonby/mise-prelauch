package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"mise.tt/api/internal/platform/consumer"
	"mise.tt/api/internal/platform/jobs"
)

// Brief §39 "notification failure" and "analytics consumer failure": a job
// whose handler fails (an SMTP server refusing, a consumer erroring or
// panicking) is retried with backoff, never silently dropped, ends as failed
// with its error after the last attempt, and is visible on the System page.
// The event it came from is not lost: the consumer can replay it.
func TestFailingEmailAndConsumerJobsRetryThenStayVisible(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	admin := bearer(h.adminLogin("admin@mise.test", "admin"))

	// A consumer routes one event type to a job, at least once.
	if _, err := h.pool.Exec(ctx, `INSERT INTO event_consumer_offsets (consumer) VALUES ('failing_test') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(ctx, `INSERT INTO domain_events (type, aggregate_type, aggregate_id) VALUES ('payment.succeeded', 'payment', gen_random_uuid())`); err != nil {
		t.Fatal(err)
	}
	d := &consumer.Dispatcher{Pool: h.pool, Name: "failing_test", Routes: map[string]consumer.Route{
		"payment.succeeded": {Kind: "test.consumer", Payload: func(id int64, agg string) any { return map[string]any{"event_id": id} }},
	}}
	if n, err := d.Dispatch(ctx); err != nil || n != 1 {
		t.Fatalf("dispatch = %d, %v", n, err)
	}
	// An email job, as the mail jobs enqueue it.
	if err := jobs.Enqueue(ctx, h.pool, "commerce.email", map[string]any{"to": "customer@example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(ctx, `UPDATE jobs SET max_attempts = 2`); err != nil {
		t.Fatal(err)
	}

	r := jobs.NewRunner(h.pool)
	r.Register("test.consumer", func(context.Context, json.RawMessage) error { panic("consumer bug") })
	r.Register("commerce.email", func(context.Context, json.RawMessage) error { return errors.New("smtp: 451 try again later") })

	// First attempt: both fail and are put back with a delay, keeping the error.
	for i := 0; i < 2; i++ {
		if ran, err := r.RunOne(ctx); err != nil || !ran {
			t.Fatalf("run %d: %v %v", i, ran, err)
		}
	}
	var queued, delayed int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status = 'queued' AND last_error <> ''), count(*) FILTER (WHERE run_at > now())
		FROM jobs`).Scan(&queued, &delayed); err != nil {
		t.Fatal(err)
	}
	if queued != 2 || delayed != 2 {
		t.Fatalf("after one failure: %d queued with an error, %d delayed; want 2, 2", queued, delayed)
	}
	// Nothing runs again before its backoff.
	if ran, _ := r.RunOne(ctx); ran {
		t.Fatal("a failed job ran again before its backoff")
	}

	// Last attempt: both end as failed, with the reason recorded.
	if _, err := h.pool.Exec(ctx, `UPDATE jobs SET run_at = now()`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if ran, err := r.RunOne(ctx); err != nil || !ran {
			t.Fatalf("retry %d: %v %v", i, ran, err)
		}
	}
	rows, err := h.pool.Query(ctx, `SELECT kind, status, attempts, last_error FROM jobs ORDER BY kind`)
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		kind, status, lastError string
		attempts                int
	}
	var got []outcome
	for rows.Next() {
		var o outcome
		if err := rows.Scan(&o.kind, &o.status, &o.attempts, &o.lastError); err != nil {
			t.Fatal(err)
		}
		got = append(got, o)
	}
	rows.Close()
	if len(got) != 2 || got[0].status != "failed" || got[1].status != "failed" || got[0].attempts != 2 ||
		got[0].lastError != "smtp: 451 try again later" || got[1].lastError != "handler panicked: consumer bug" {
		t.Fatalf("final job states = %+v", got)
	}

	// Staff see both on the System page.
	res := h.do("GET", "/api/v1/admin/system/status", nil, admin)
	h.wantStatus(res, http.StatusOK)
	if res.Data["failed_jobs_24h"] != float64(2) || res.Data["email_failures_24h"] != float64(1) {
		t.Fatalf("system status = failed %v, email %v", res.Data["failed_jobs_24h"], res.Data["email_failures_24h"])
	}

	// The event is not lost: replaying the consumer from the start enqueues it again.
	if err := consumer.Replay(ctx, h.pool, "failing_test", 0); err != nil {
		t.Fatal(err)
	}
	if n, err := d.Dispatch(ctx); err != nil || n != 1 {
		t.Fatalf("replay dispatch = %d, %v", n, err)
	}
}
