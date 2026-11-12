package identity

import (
	"net/http"
	"regexp"
	"strings"

	"mise.tt/api/internal/platform/httpx"
)

var staffIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// StaffRoutes serves staff administration. limit applies to the public
// invitation acceptance.
func (h *Handler) StaffRoutes(mux httpx.Router, limit func(http.Handler) http.Handler) {
	s := h.Service
	mux.Handle("GET /api/v1/admin/staff", s.RequireAdmin(PermUsersRead, h.listStaff))
	mux.Handle("POST /api/v1/admin/staff/invitations", s.RequireAdmin(PermUsersManage, h.invite))
	mux.Handle("DELETE /api/v1/admin/staff/invitations/{id}", s.RequireAdmin(PermUsersManage, h.revokeInvitation))
	mux.Handle("POST /api/v1/admin/staff/{id}/roles", s.RequireAdmin(PermUsersManage, h.grantRole))
	mux.Handle("POST /api/v1/admin/staff/{id}/roles/revoke", s.RequireAdmin(PermUsersManage, h.revokeRole))
	mux.Handle("POST /api/v1/admin/staff/{id}/disable", s.RequireAdmin(PermUsersManage, h.setDisabled(true)))
	mux.Handle("POST /api/v1/admin/staff/{id}/enable", s.RequireAdmin(PermUsersManage, h.setDisabled(false)))
	mux.Handle("POST /api/v1/admin/staff/{id}/signout", s.RequireAdmin(PermUsersManage, h.signOutStaff))
	mux.Handle("GET /api/v1/admin/roles", s.RequireAdmin(PermUsersRead, h.roles))
	mux.Handle("POST /api/v1/admin/auth/invitations/accept", limit(http.HandlerFunc(h.acceptInvitation)))
}

func staffRespond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, status, v)
}

// reasonInput is the body of a sensitive change: a reason is required.
type reasonInput struct {
	Role   string `json:"role"`
	Reason string `json:"reason"`
}

func readReason(w http.ResponseWriter, r *http.Request) (reasonInput, bool) {
	var in reasonInput
	if err := httpx.Decode(w, r, &in, 2<<10); err != nil {
		httpx.Fail(w, r, err)
		return in, false
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if n := len([]rune(in.Reason)); n < 3 || n > 300 {
		httpx.Fail(w, r, httpx.ValidationError([]httpx.FieldError{{Field: "reason", Code: "required", Message: "Give a reason (3 to 300 characters)."}}))
		return in, false
	}
	return in, true
}

func staffID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !staffIDPattern.MatchString(id) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return "", false
	}
	return id, true
}

func (h *Handler) listStaff(w http.ResponseWriter, r *http.Request) {
	staff, err := h.Service.ListStaff(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	invites, err := h.Service.OpenInvitations(r.Context())
	staffRespond(w, r, http.StatusOK, map[string]any{"items": staff, "invitations": invites}, err)
}

func (h *Handler) invite(w http.ResponseWriter, r *http.Request) {
	var in InviteInput
	if err := httpx.Decode(w, r, &in, 2<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	inv, err := h.Service.Invite(r.Context(), PrincipalFrom(r.Context()), in)
	staffRespond(w, r, http.StatusCreated, inv, err)
}

func (h *Handler) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	id, ok := staffID(w, r)
	if !ok {
		return
	}
	err := h.Service.RevokeInvitation(r.Context(), PrincipalFrom(r.Context()), id)
	staffRespond(w, r, http.StatusOK, map[string]bool{"revoked": true}, err)
}

func (h *Handler) grantRole(w http.ResponseWriter, r *http.Request) {
	id, ok := staffID(w, r)
	if !ok {
		return
	}
	in, ok := readReason(w, r)
	if !ok {
		return
	}
	err := h.Service.GrantStaffRole(r.Context(), PrincipalFrom(r.Context()), id, in.Role, in.Reason)
	staffRespond(w, r, http.StatusOK, map[string]bool{"granted": true}, err)
}

func (h *Handler) revokeRole(w http.ResponseWriter, r *http.Request) {
	id, ok := staffID(w, r)
	if !ok {
		return
	}
	in, ok := readReason(w, r)
	if !ok {
		return
	}
	err := h.Service.RevokeStaffRole(r.Context(), PrincipalFrom(r.Context()), id, in.Role, in.Reason)
	staffRespond(w, r, http.StatusOK, map[string]bool{"revoked": true}, err)
}

func (h *Handler) setDisabled(disabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := staffID(w, r)
		if !ok {
			return
		}
		in, ok := readReason(w, r)
		if !ok {
			return
		}
		err := h.Service.SetStaffDisabled(r.Context(), PrincipalFrom(r.Context()), id, disabled, in.Reason)
		staffRespond(w, r, http.StatusOK, map[string]bool{"disabled": disabled}, err)
	}
}

func (h *Handler) signOutStaff(w http.ResponseWriter, r *http.Request) {
	id, ok := staffID(w, r)
	if !ok {
		return
	}
	in, ok := readReason(w, r)
	if !ok {
		return
	}
	n, err := h.Service.SignOutStaff(r.Context(), PrincipalFrom(r.Context()), id, in.Reason)
	staffRespond(w, r, http.StatusOK, map[string]int64{"sessions_ended": n}, err)
}

func (h *Handler) roles(w http.ResponseWriter, r *http.Request) {
	roles, perms, err := h.Service.Roles(r.Context())
	staffRespond(w, r, http.StatusOK, map[string]any{"roles": roles, "permissions": perms}, err)
}

func (h *Handler) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(w, r, &in, 2<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	err := h.Service.AcceptInvitation(r.Context(), in.Token, in.Password)
	staffRespond(w, r, http.StatusOK, map[string]bool{"accepted": true}, err)
}
