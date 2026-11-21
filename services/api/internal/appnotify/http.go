package appnotify

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mise.tt/api/internal/audit"
	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/db"
	"mise.tt/api/internal/platform/httpx"
)

type Handler struct {
	S        *Service
	Identity *identity.Service
}

var (
	uuidRe         = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	installationRe = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
)

func (h *Handler) Routes(mux httpx.Router) {
	cust := h.Identity.RequireCustomer
	mux.Handle("GET /api/v1/me/notifications", cust(h.list))
	mux.Handle("POST /api/v1/me/notifications/read-all", cust(h.readAll))
	mux.Handle("POST /api/v1/me/notifications/{id}/read", cust(h.read))
	mux.Handle("POST /api/v1/me/devices", cust(h.registerDevice))
	mux.Handle("DELETE /api/v1/me/devices/{installationId}", cust(h.removeDevice))
	mux.Handle("GET /api/v1/me/account/deletion-request", cust(h.getDeletion))
	mux.Handle("POST /api/v1/me/account/deletion-request", cust(h.requestDeletion))
	mux.Handle("DELETE /api/v1/me/account/deletion-request", cust(h.withdrawDeletion))

	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/account-deletion-requests", adm(identity.PermCustomersRead, h.adminDeletions))
}

func principal(r *http.Request) *identity.Principal { return identity.PrincipalFrom(r.Context()) }

// ---------------------------------------------------------------- notification centre

type NotificationView struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Link      *string    `json:"link"`
	ReadAt    *time.Time `json:"read_at"`
	CreatedAt time.Time  `json:"created_at"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	rows, err := h.S.pool.Query(r.Context(), `
		SELECT id, type, title, body, link, read_at, created_at FROM customer_notifications
		WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT 100`, p.UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer rows.Close()
	items := []NotificationView{}
	unread := 0
	for rows.Next() {
		var n NotificationView
		if err := rows.Scan(&n.ID, &n.Type, &n.Title, &n.Body, &n.Link, &n.ReadAt, &n.CreatedAt); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		if n.ReadAt == nil {
			unread++
		}
		items = append(items, n)
	}
	if err := rows.Err(); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{"items": items, "unread": unread})
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(r.PathValue("id"))
	if !uuidRe.MatchString(id) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	tag, err := h.S.pool.Exec(r.Context(), `UPDATE customer_notifications SET read_at = COALESCE(read_at, now())
		WHERE id = $1 AND user_id = $2`, id, principal(r).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]bool{"read": true})
}

func (h *Handler) readAll(w http.ResponseWriter, r *http.Request) {
	tag, err := h.S.pool.Exec(r.Context(), `UPDATE customer_notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, principal(r).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]int64{"updated": tag.RowsAffected()})
}

// ---------------------------------------------------------------- devices

func (h *Handler) registerDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		InstallationID string `json:"installation_id"`
		Platform       string `json:"platform"`
		PushToken      string `json:"push_token"`
		AppVersion     string `json:"app_version"`
	}
	if err := httpx.Decode(w, r, &in, 8<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	var fields []httpx.FieldError
	if !installationRe.MatchString(in.InstallationID) {
		fields = append(fields, httpx.FieldError{Field: "installation_id", Code: "invalid", Message: "Use 8 to 64 letters, digits, - or _."})
	}
	if in.Platform != "android" && in.Platform != "ios" {
		fields = append(fields, httpx.FieldError{Field: "platform", Code: "invalid", Message: "Use android or ios."})
	}
	if l := len(in.PushToken); l < 16 || l > 4096 {
		fields = append(fields, httpx.FieldError{Field: "push_token", Code: "invalid", Message: "Not a push token."})
	}
	if len(in.AppVersion) > 40 {
		fields = append(fields, httpx.FieldError{Field: "app_version", Code: "too_long", Message: "Too long."})
	}
	if fields != nil {
		httpx.Fail(w, r, httpx.ValidationError(fields))
		return
	}
	p := principal(r)
	var version *string
	if in.AppVersion != "" {
		version = &in.AppVersion
	}
	err := db.InTx(r.Context(), h.S.pool, func(tx pgx.Tx) error {
		// Token rotation, reinstall on the same id, or a token now used by
		// another account: the newest registration wins, the old one ends.
		if _, err := tx.Exec(r.Context(), `UPDATE push_devices SET revoked_at = now(), revoked_reason = 'replaced'
			WHERE revoked_at IS NULL AND (installation_id = $1 OR push_token = $2)`, in.InstallationID, in.PushToken); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `INSERT INTO push_devices (user_id, session_id, installation_id, platform, push_token, app_version)
			VALUES ($1, $2, $3, $4, $5, $6)`, p.UserID, p.SessionID, in.InstallationID, in.Platform, in.PushToken, version)
		return err
	})
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{"registered": true, "push_available": h.S.Pusher != nil})
}

func (h *Handler) removeDevice(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("installationId")
	if !installationRe.MatchString(id) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	if _, err := h.S.pool.Exec(r.Context(), `UPDATE push_devices SET revoked_at = now(), revoked_reason = 'user'
		WHERE installation_id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, principal(r).UserID); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]bool{"removed": true})
}

// ---------------------------------------------------------------- account deletion

type DeletionRequest struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	Reason      string     `json:"reason"`
	RequestedAt time.Time  `json:"requested_at"`
	WithdrawnAt *time.Time `json:"withdrawn_at"`
	HandledAt   *time.Time `json:"handled_at"`
	Email       string     `json:"email,omitempty"`
	Name        string     `json:"name,omitempty"`
	Source      string     `json:"source,omitempty"`
}

// What a request does and does not do, shown to the customer before and after.
const deletionNotice = "Your request is recorded and the Mise team will contact you. Nothing is deleted yet: " +
	"we first finish or cancel any paid deliveries, and some records (orders, invoices and payments) must be kept for the " +
	"period the law requires. Everything else about you is then deleted. You can withdraw the request until we start."

func (h *Handler) latestDeletion(r *http.Request, userID string) (*DeletionRequest, error) {
	var d DeletionRequest
	err := h.S.pool.QueryRow(r.Context(), `SELECT id, status, reason, requested_at, withdrawn_at, handled_at
		FROM account_deletion_requests WHERE user_id = $1 ORDER BY requested_at DESC LIMIT 1`, userID).
		Scan(&d.ID, &d.Status, &d.Reason, &d.RequestedAt, &d.WithdrawnAt, &d.HandledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &d, err
}

func (h *Handler) getDeletion(w http.ResponseWriter, r *http.Request) {
	d, err := h.latestDeletion(r, principal(r).UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{"request": d, "notice": deletionNotice})
}

func (h *Handler) requestDeletion(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Reason       string `json:"reason"`
		Acknowledged bool   `json:"acknowledged"`
	}
	if err := httpx.Decode(w, r, &in, 8<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if !in.Acknowledged {
		httpx.Fail(w, r, httpx.ValidationError([]httpx.FieldError{{Field: "acknowledged", Code: "required", Message: "Please confirm you have read what happens next."}}))
		return
	}
	if len([]rune(in.Reason)) > 1000 {
		httpx.Fail(w, r, httpx.ValidationError([]httpx.FieldError{{Field: "reason", Code: "too_long", Message: "Use at most 1000 characters."}}))
		return
	}
	p := principal(r)
	var id string
	err := db.InTx(r.Context(), h.S.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `INSERT INTO account_deletion_requests (user_id, reason, acknowledged, source)
			VALUES ($1, $2, true, 'app') RETURNING id`, p.UserID, strings.TrimSpace(in.Reason)).Scan(&id); err != nil {
			return err
		}
		return audit.Record(r.Context(), tx, audit.Entry{ActorUserID: p.UserID, Action: "account.deletion_requested", TargetType: "user", TargetID: p.UserID})
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		httpx.Fail(w, r, httpx.NewError(http.StatusConflict, "deletion_already_requested", "You have already asked us to delete your account."))
		return
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	d, err := h.latestDeletion(r, p.UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusCreated, map[string]any{"request": d, "notice": deletionNotice})
}

func (h *Handler) withdrawDeletion(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	tag, err := h.S.pool.Exec(r.Context(), `UPDATE account_deletion_requests SET status = 'withdrawn', withdrawn_at = now()
		WHERE user_id = $1 AND status = 'requested'`, p.UserID)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Fail(w, r, httpx.NewError(http.StatusConflict, "nothing_to_withdraw", "There is no request you can withdraw."))
		return
	}
	_ = audit.Record(r.Context(), h.S.pool, audit.Entry{ActorUserID: p.UserID, Action: "account.deletion_withdrawn", TargetType: "user", TargetID: p.UserID})
	httpx.JSON(w, r, http.StatusOK, map[string]bool{"withdrawn": true})
}

// adminDeletions lists open requests for the team to act on. Read-only:
// erasure itself is not built until the retention policy is approved.
func (h *Handler) adminDeletions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.S.pool.Query(r.Context(), `
		SELECT d.id, d.status, d.reason, d.requested_at, d.withdrawn_at, d.handled_at, u.email, u.first_name || ' ' || u.last_name, d.source
		FROM account_deletion_requests d JOIN users u ON u.id = d.user_id
		ORDER BY (d.status IN ('requested', 'in_progress')) DESC, d.requested_at DESC LIMIT 200`)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	defer rows.Close()
	items := []DeletionRequest{}
	for rows.Next() {
		var d DeletionRequest
		if err := rows.Scan(&d.ID, &d.Status, &d.Reason, &d.RequestedAt, &d.WithdrawnAt, &d.HandledAt, &d.Email, &d.Name, &d.Source); err != nil {
			httpx.Fail(w, r, err)
			return
		}
		items = append(items, d)
	}
	httpx.JSON(w, r, http.StatusOK, map[string]any{"items": items})
}
