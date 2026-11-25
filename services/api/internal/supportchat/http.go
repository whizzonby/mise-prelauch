package supportchat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"mise.tt/api/internal/identity"
	"mise.tt/api/internal/platform/httpx"
)

// GuestHeader carries the guest token from the app server (the browser keeps
// it in an httpOnly cookie on the app host and never sees the API).
const GuestHeader = "X-Guest-Token"

// CustomerIDs resolves a signed-in customer's id.
type CustomerIDs interface {
	CustomerID(ctx context.Context, p *identity.Principal) (string, error)
}

type Handler struct {
	S         *Service
	Identity  *identity.Service
	Customers CustomerIDs
	// StartLimit guards creating guest conversations (per client address).
	StartLimit func(http.Handler) http.Handler
	// Stream timing; tests shorten them.
	Heartbeat, Revalidate, MaxStream time.Duration
}

func (h *Handler) Routes(mux httpx.Router) {
	limit := h.StartLimit
	if limit == nil {
		limit = func(n http.Handler) http.Handler { return n }
	}
	// Public.
	mux.Handle("GET /api/v1/chat/availability", http.HandlerFunc(h.availability))
	mux.Handle("GET /api/v1/help/categories", http.HandlerFunc(h.helpCategories))
	mux.Handle("GET /api/v1/help/articles", http.HandlerFunc(h.helpSearch))
	mux.Handle("GET /api/v1/help/articles/{slug}", http.HandlerFunc(h.helpArticle))

	// Guests (token in X-Guest-Token).
	mux.Handle("POST /api/v1/chat/guest/conversations", limit(http.HandlerFunc(h.guestStart)))
	h.ownerRoutes(mux, "/api/v1/chat/guest/conversations", func(fn ownerHandler) http.Handler { return http.HandlerFunc(h.asGuest(fn)) })

	// Signed-in customers.
	cust := h.Identity.RequireCustomer
	mux.Handle("POST /api/v1/me/chat/conversations", cust(h.asCustomer(h.customerStart)))
	mux.Handle("POST /api/v1/me/chat/link-guest", cust(h.linkGuest))
	h.ownerRoutes(mux, "/api/v1/me/chat/conversations", func(fn ownerHandler) http.Handler { return cust(h.asCustomer(fn)) })

	// Staff.
	adm := h.Identity.RequireAdmin
	mux.Handle("GET /api/v1/admin/chat/conversations", adm(identity.PermSupportChatRead, h.inbox))
	mux.Handle("GET /api/v1/admin/chat/conversations/{id}", adm(identity.PermSupportChatRead, h.detail))
	mux.Handle("GET /api/v1/admin/chat/conversations/{id}/messages", adm(identity.PermSupportChatRead, h.agentMessages))
	mux.Handle("POST /api/v1/admin/chat/conversations/{id}/messages", adm(identity.PermSupportChatReply, h.reply))
	mux.Handle("PATCH /api/v1/admin/chat/conversations/{id}", adm(identity.PermSupportChatReply, h.update))
	mux.Handle("POST /api/v1/admin/chat/conversations/{id}/notes", adm(identity.PermSupportChatInternalNotes, h.note))
	mux.Handle("POST /api/v1/admin/chat/conversations/{id}/assign", adm(identity.PermSupportChatAssign, h.assign(false)))
	mux.Handle("POST /api/v1/admin/chat/conversations/{id}/transfer", adm(identity.PermSupportChatTransfer, h.assign(true)))
	mux.Handle("POST /api/v1/admin/chat/conversations/{id}/resolve", adm(identity.PermSupportChatResolve, h.setStatus(true)))
	mux.Handle("POST /api/v1/admin/chat/conversations/{id}/reopen", adm(identity.PermSupportChatResolve, h.setStatus(false)))
	mux.Handle("POST /api/v1/admin/chat/conversations/{id}/escalate", adm(identity.PermCasesManage, h.escalate))
	mux.Handle("POST /api/v1/admin/chat/conversations/{id}/read", adm(identity.PermSupportChatRead, h.agentRead))
	mux.Handle("POST /api/v1/admin/chat/conversations/{id}/typing", adm(identity.PermSupportChatReply, h.agentTyping))
	mux.Handle("GET /api/v1/admin/chat/stream", adm(identity.PermSupportChatRead, h.agentStream))
	mux.Handle("GET /api/v1/admin/chat/agents", adm(identity.PermSupportChatRead, h.agents))
	mux.Handle("GET /api/v1/admin/chat/saved-replies", adm(identity.PermSupportChatRead, h.savedReplies))
	mux.Handle("POST /api/v1/admin/chat/saved-replies", adm(identity.PermSupportChatManage, h.createSavedReply))
	mux.Handle("PATCH /api/v1/admin/chat/saved-replies/{id}", adm(identity.PermSupportChatManage, h.updateSavedReply))
	mux.Handle("GET /api/v1/admin/chat/settings", adm(identity.PermSupportChatManage, h.settings))
	mux.Handle("PUT /api/v1/admin/chat/settings", adm(identity.PermSupportChatManage, h.updateSettings))

	mux.Handle("GET /api/v1/admin/help/articles", adm(identity.PermHelpCentreEdit, h.staffArticles))
	mux.Handle("POST /api/v1/admin/help/articles", adm(identity.PermHelpCentreEdit, h.createArticle))
	mux.Handle("GET /api/v1/admin/help/articles/{id}", adm(identity.PermHelpCentreEdit, h.staffArticle))
	mux.Handle("PATCH /api/v1/admin/help/articles/{id}", adm(identity.PermHelpCentreEdit, h.updateArticle))
	mux.Handle("POST /api/v1/admin/help/articles/{id}/{action}", adm(identity.PermHelpCentreEdit, h.transitionArticle))
}

// ---------------------------------------------------------------- helpers

func reply(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

func decode[T any](w http.ResponseWriter, r *http.Request, max int64) (T, bool) {
	var in T
	if err := httpx.Decode(w, r, &in, max); err != nil {
		httpx.Fail(w, r, err)
		return in, false
	}
	return in, true
}

func afterParam(r *http.Request) int64 {
	v := r.URL.Query().Get("after")
	if v == "" {
		v = r.Header.Get("Last-Event-ID")
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

type seqInput struct {
	Seq int64 `json:"seq"`
}

// ---------------------------------------------------------------- public

func (h *Handler) availability(w http.ResponseWriter, r *http.Request) {
	a, err := h.S.Availability(r.Context())
	reply(w, r, http.StatusOK, a, err)
}

func (h *Handler) helpCategories(w http.ResponseWriter, r *http.Request) {
	c, err := h.S.HelpCategories(r.Context())
	reply(w, r, http.StatusOK, map[string]any{"items": c}, err)
}

func (h *Handler) helpSearch(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.HelpSearch(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("category"))
	reply(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) helpArticle(w http.ResponseWriter, r *http.Request) {
	a, err := h.S.HelpArticleBySlug(r.Context(), r.PathValue("slug"))
	reply(w, r, http.StatusOK, a, err)
}

// ---------------------------------------------------------------- owners

// ownerHandler is an endpoint that acts for a conversation owner.
type ownerHandler func(w http.ResponseWriter, r *http.Request, o Owner, revalidate func(context.Context) error)

func (h *Handler) asGuest(fn ownerHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get(GuestHeader)
		g, err := h.S.GuestFromToken(r.Context(), token)
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		fn(w, r, Owner{Kind: "guest", ID: g.ID}, func(ctx context.Context) error {
			_, err := h.S.GuestFromToken(ctx, token)
			return err
		})
	}
}

func (h *Handler) asCustomer(fn ownerHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cid, err := h.Customers.CustomerID(r.Context(), identity.PrincipalFrom(r.Context()))
		if err != nil {
			httpx.Fail(w, r, err)
			return
		}
		token := identity.BearerToken(r)
		fn(w, r, Owner{Kind: "customer", ID: cid}, func(ctx context.Context) error {
			p, err := h.Identity.Authenticate(ctx, token, identity.AudienceCustomer)
			if err == nil && p == nil {
				err = httpx.ErrUnauthorized
			}
			return err
		})
	}
}

func (h *Handler) ownerRoutes(mux httpx.Router, base string, wrap func(ownerHandler) http.Handler) {
	mux.Handle("GET "+base, wrap(h.ownerList))
	mux.Handle("GET "+base+"/{id}", wrap(h.ownerGet))
	mux.Handle("GET "+base+"/{id}/messages", wrap(h.ownerMessages))
	mux.Handle("POST "+base+"/{id}/messages", wrap(h.ownerSend))
	mux.Handle("POST "+base+"/{id}/read", wrap(h.ownerRead))
	mux.Handle("POST "+base+"/{id}/typing", wrap(h.ownerTyping))
	mux.Handle("POST "+base+"/{id}/feedback", wrap(h.ownerFeedback))
	mux.Handle("GET "+base+"/{id}/stream", wrap(h.ownerStream))
}

func (h *Handler) guestStart(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[GuestInput](w, r, 16<<10)
	if !ok {
		return
	}
	g, c, m, err := h.S.StartGuest(r.Context(), in)
	reply(w, r, http.StatusCreated, map[string]any{"guest": g, "conversation": c, "message": m}, err)
}

func (h *Handler) customerStart(w http.ResponseWriter, r *http.Request, o Owner, _ func(context.Context) error) {
	in, ok := decode[StartInput](w, r, 16<<10)
	if !ok {
		return
	}
	c, m, err := h.S.Start(r.Context(), o, in)
	reply(w, r, http.StatusCreated, map[string]any{"conversation": c, "message": m}, err)
}

func (h *Handler) linkGuest(w http.ResponseWriter, r *http.Request) {
	p := identity.PrincipalFrom(r.Context())
	cid, err := h.Customers.CustomerID(r.Context(), p)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	n, err := h.S.LinkGuest(r.Context(), cid, p.Email, p.EmailVerified, r.Header.Get(GuestHeader))
	reply(w, r, http.StatusOK, map[string]any{"linked_conversations": n}, err)
}

func (h *Handler) ownerList(w http.ResponseWriter, r *http.Request, o Owner, _ func(context.Context) error) {
	list, err := h.S.OwnerConversations(r.Context(), o)
	reply(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) ownerGet(w http.ResponseWriter, r *http.Request, o Owner, _ func(context.Context) error) {
	c, err := h.S.OwnerConversation(r.Context(), o, r.PathValue("id"))
	reply(w, r, http.StatusOK, c, err)
}

func (h *Handler) ownerMessages(w http.ResponseWriter, r *http.Request, o Owner, _ func(context.Context) error) {
	ms, err := h.S.OwnerMessages(r.Context(), o, r.PathValue("id"), afterParam(r))
	reply(w, r, http.StatusOK, map[string]any{"items": ms}, err)
}

func (h *Handler) ownerSend(w http.ResponseWriter, r *http.Request, o Owner, _ func(context.Context) error) {
	in, ok := decode[SendInput](w, r, 16<<10)
	if !ok {
		return
	}
	m, err := h.S.Send(r.Context(), o, r.PathValue("id"), in)
	reply(w, r, http.StatusCreated, m, err)
}

func (h *Handler) ownerRead(w http.ResponseWriter, r *http.Request, o Owner, _ func(context.Context) error) {
	in, ok := decode[seqInput](w, r, 1<<10)
	if !ok {
		return
	}
	reply(w, r, http.StatusOK, map[string]any{"ok": true}, h.S.MarkRead(r.Context(), o, r.PathValue("id"), in.Seq))
}

func (h *Handler) ownerTyping(w http.ResponseWriter, r *http.Request, o Owner, _ func(context.Context) error) {
	reply(w, r, http.StatusOK, map[string]any{"ok": true}, h.S.Typing(r.Context(), o, r.PathValue("id")))
}

func (h *Handler) ownerFeedback(w http.ResponseWriter, r *http.Request, o Owner, _ func(context.Context) error) {
	in, ok := decode[FeedbackInput](w, r, 4<<10)
	if !ok {
		return
	}
	reply(w, r, http.StatusOK, map[string]any{"ok": true}, h.S.Feedback(r.Context(), o, r.PathValue("id"), in))
}

// ---------------------------------------------------------------- streams

type sse struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func startSSE(w http.ResponseWriter) *sse {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no") // no proxy buffering
	w.WriteHeader(http.StatusOK)
	s := &sse{w: w, rc: http.NewResponseController(w)}
	_ = s.flush()
	return s
}

func (s *sse) flush() error {
	// The API server's write timeout is for ordinary requests; a stream
	// extends its own deadline with every write.
	_ = s.rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return s.rc.Flush()
}

func (s *sse) event(id, name string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if id != "" {
		if _, err := fmt.Fprintf(s.w, "id: %s\n", id); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, b); err != nil {
		return err
	}
	return s.flush()
}

func (s *sse) comment(text string) error {
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", text); err != nil {
		return err
	}
	return s.flush()
}

func (h *Handler) timings() (heartbeat, revalidate, max time.Duration) {
	heartbeat, revalidate, max = h.Heartbeat, h.Revalidate, h.MaxStream
	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}
	if revalidate <= 0 {
		revalidate = time.Minute
	}
	if max <= 0 {
		max = 25 * time.Minute // the client reconnects with Last-Event-ID
	}
	return
}

// ownerStream sends the owner's conversation live: messages after the
// client's last seq (Last-Event-ID or ?after=), then each new one; status
// and read-receipt changes; the team's typing. Every wake-up re-reads the
// database with the owner's own authorisation; the session is re-checked
// periodically and the stream ends when it is no longer valid.
func (h *Handler) ownerStream(w http.ResponseWriter, r *http.Request, o Owner, revalidate func(context.Context) error) {
	ctx := r.Context()
	id := r.PathValue("id")
	conv, err := h.S.OwnerConversation(ctx, o, id)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	sub := h.S.Hub.Subscribe(func(e Event) bool { return e.Conversation == conv.ID })
	defer h.S.Hub.Unsubscribe(sub)
	out := startSSE(w)
	after := afterParam(r)
	var lastState string
	send := func() error {
		c, err := h.S.OwnerConversation(ctx, o, id)
		if err != nil {
			return err // no longer this owner's (linked elsewhere): end the stream
		}
		for {
			ms, err := Messages(ctx, h.S.pool, id, after, 100)
			if err != nil {
				return err
			}
			for _, m := range ms {
				if err := out.event(strconv.FormatInt(m.Seq, 10), "message", m); err != nil {
					return err
				}
				after = m.Seq
			}
			if len(ms) < 100 {
				break
			}
		}
		state := c.Status + "|" + strconv.FormatInt(c.TeamReadSeq, 10)
		if state != lastState {
			lastState = state
			return out.event("", "conversation", c)
		}
		return nil
	}
	if err := send(); err != nil {
		return
	}
	heartbeat, recheck, max := h.timings()
	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	check := time.NewTicker(recheck)
	defer check.Stop()
	deadline := time.After(max)
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			return
		case <-check.C:
			if revalidate(ctx) != nil {
				_ = out.event("", "end", map[string]string{"reason": "session_ended"})
				return
			}
		case e := <-sub.C:
			if e.Kind == "typing" {
				if e.Actor == "agent" {
					if out.event("", "typing", map[string]string{"who": "agent"}) != nil {
						return
					}
				}
				continue
			}
			if send() != nil {
				return
			}
		case <-tick.C:
			// Heartbeat, and a database check in case a signal was missed.
			if out.comment("ping") != nil || send() != nil {
				return
			}
		}
	}
}

// agentStream tells the inbox what changed (conversation id and kind); the
// inbox then fetches through the ordinary endpoints. The staff member's
// session and permission are re-checked periodically.
func (h *Handler) agentStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := identity.BearerToken(r)
	sub := h.S.Hub.Subscribe(nil)
	defer h.S.Hub.Unsubscribe(sub)
	out := startSSE(w)
	if out.event("", "ready", map[string]bool{"ok": true}) != nil {
		return
	}
	heartbeat, recheck, max := h.timings()
	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	check := time.NewTicker(recheck)
	defer check.Stop()
	deadline := time.After(max)
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			return
		case <-check.C:
			p, err := h.Identity.Authenticate(ctx, token, identity.AudienceAdmin)
			if err != nil || p == nil || !p.Can(identity.PermSupportChatRead) || !p.MFA.Complete() {
				_ = out.event("", "end", map[string]string{"reason": "session_ended"})
				return
			}
		case e := <-sub.C:
			if e.Kind == "typing" && e.Actor == "agent" {
				continue // agents do not need each other's typing
			}
			if out.event("", e.Kind, map[string]any{"conversation_id": e.Conversation, "seq": e.Seq, "actor": e.Actor}) != nil {
				return
			}
		case <-tick.C:
			if out.comment("ping") != nil {
				return
			}
		}
	}
}

// ---------------------------------------------------------------- staff

func principal(r *http.Request) *identity.Principal { return identity.PrincipalFrom(r.Context()) }

func (h *Handler) inbox(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := h.S.Inbox(r.Context(), principal(r), InboxFilter{View: q.Get("view"), Q: q.Get("q")})
	reply(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	d, err := h.S.Get(r.Context(), principal(r), r.PathValue("id"))
	reply(w, r, http.StatusOK, d, err)
}

func (h *Handler) agentMessages(w http.ResponseWriter, r *http.Request) {
	ms, err := h.S.AgentMessages(r.Context(), r.PathValue("id"), afterParam(r))
	reply(w, r, http.StatusOK, map[string]any{"items": ms}, err)
}

func (h *Handler) reply(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[SendInput](w, r, 16<<10)
	if !ok {
		return
	}
	m, err := h.S.Reply(r.Context(), principal(r), r.PathValue("id"), in)
	reply(w, r, http.StatusCreated, m, err)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[UpdateInput](w, r, 8<<10)
	if !ok {
		return
	}
	d, err := h.S.Update(r.Context(), principal(r), r.PathValue("id"), in)
	reply(w, r, http.StatusOK, d, err)
}

func (h *Handler) note(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[SendInput](w, r, 16<<10)
	if !ok {
		return
	}
	n, err := h.S.AddNote(r.Context(), principal(r), r.PathValue("id"), in.Body)
	reply(w, r, http.StatusCreated, n, err)
}

func (h *Handler) assign(transfer bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		in, ok := decode[AssignInput](w, r, 4<<10)
		if !ok {
			return
		}
		d, err := h.S.Assign(r.Context(), principal(r), r.PathValue("id"), in, transfer)
		reply(w, r, http.StatusOK, d, err)
	}
}

func (h *Handler) setStatus(resolve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Note string `json:"note"`
		}
		if r.ContentLength != 0 {
			if err := httpx.Decode(w, r, &in, 4<<10); err != nil {
				httpx.Fail(w, r, err)
				return
			}
		}
		d, err := h.S.SetStatus(r.Context(), principal(r), r.PathValue("id"), resolve, in.Note)
		reply(w, r, http.StatusOK, d, err)
	}
}

func (h *Handler) escalate(w http.ResponseWriter, r *http.Request) {
	if !principal(r).Can(identity.PermSupportChatReply) {
		httpx.Fail(w, r, httpx.ErrForbidden)
		return
	}
	in, ok := decode[EscalateInput](w, r, 8<<10)
	if !ok {
		return
	}
	d, err := h.S.Escalate(r.Context(), principal(r), r.PathValue("id"), in)
	reply(w, r, http.StatusOK, d, err)
}

func (h *Handler) agentRead(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[seqInput](w, r, 1<<10)
	if !ok {
		return
	}
	reply(w, r, http.StatusOK, map[string]any{"ok": true}, h.S.AgentRead(r.Context(), principal(r), r.PathValue("id"), in.Seq))
}

func (h *Handler) agentTyping(w http.ResponseWriter, r *http.Request) {
	reply(w, r, http.StatusOK, map[string]any{"ok": true}, h.S.AgentTyping(r.Context(), principal(r), r.PathValue("id")))
}

func (h *Handler) agents(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.Agents(r.Context())
	reply(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) savedReplies(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.SavedReplies(r.Context(), r.URL.Query().Get("archived") == "true")
	reply(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) createSavedReply(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[SavedReplyInput](w, r, 16<<10)
	if !ok {
		return
	}
	sr, err := h.S.CreateSavedReply(r.Context(), principal(r), in)
	reply(w, r, http.StatusCreated, sr, err)
}

func (h *Handler) updateSavedReply(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[SavedReplyInput](w, r, 16<<10)
	if !ok {
		return
	}
	sr, err := h.S.UpdateSavedReply(r.Context(), principal(r), r.PathValue("id"), in)
	reply(w, r, http.StatusOK, sr, err)
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	st, err := h.S.Settings(r.Context())
	reply(w, r, http.StatusOK, st, err)
}

func (h *Handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[Settings](w, r, 32<<10)
	if !ok {
		return
	}
	st, err := h.S.UpdateSettings(r.Context(), principal(r), in)
	reply(w, r, http.StatusOK, st, err)
}

func (h *Handler) staffArticles(w http.ResponseWriter, r *http.Request) {
	list, err := h.S.StaffArticles(r.Context(), r.URL.Query().Get("status"))
	reply(w, r, http.StatusOK, map[string]any{"items": list}, err)
}

func (h *Handler) staffArticle(w http.ResponseWriter, r *http.Request) {
	a, err := h.S.StaffArticle(r.Context(), r.PathValue("id"))
	reply(w, r, http.StatusOK, a, err)
}

func (h *Handler) createArticle(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ArticleInput](w, r, 64<<10)
	if !ok {
		return
	}
	a, err := h.S.CreateArticle(r.Context(), principal(r), in)
	reply(w, r, http.StatusCreated, a, err)
}

func (h *Handler) updateArticle(w http.ResponseWriter, r *http.Request) {
	in, ok := decode[ArticleInput](w, r, 64<<10)
	if !ok {
		return
	}
	a, err := h.S.UpdateArticle(r.Context(), principal(r), r.PathValue("id"), in)
	reply(w, r, http.StatusOK, a, err)
}

func (h *Handler) transitionArticle(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Note string `json:"note"`
	}
	if r.ContentLength != 0 {
		if err := httpx.Decode(w, r, &in, 4<<10); err != nil {
			httpx.Fail(w, r, err)
			return
		}
	}
	a, err := h.S.TransitionArticle(r.Context(), principal(r), r.PathValue("id"), r.PathValue("action"), in.Note)
	reply(w, r, http.StatusOK, a, err)
}
