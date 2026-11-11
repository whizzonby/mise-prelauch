package testpay

import (
	"errors"
	"net/http"
	"regexp"

	"mise.tt/api/internal/platform/httpx"
)

var refPattern = regexp.MustCompile(`^tps_[0-9a-f]{30}$`)

// Routes serves the simulated provider's hosted page actions. They exist
// only when the test provider is enabled, which configuration refuses in
// production. A session reference is unguessable, like a real provider's
// hosted checkout link.
func (p *Provider) Routes(mux httpx.Router, limit func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/test-payments/sessions/{ref}", limit(http.HandlerFunc(p.getSession)))
	mux.Handle("POST /api/v1/test-payments/sessions/{ref}/complete", limit(http.HandlerFunc(p.complete)))
}

func (p *Provider) getSession(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("ref")
	if !refPattern.MatchString(ref) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	s, err := p.Session(r.Context(), ref)
	if errors.Is(err, ErrNotFound) {
		err = httpx.ErrNotFound
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, s)
}

func (p *Provider) complete(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("ref")
	if !refPattern.MatchString(ref) {
		httpx.Fail(w, r, httpx.ErrNotFound)
		return
	}
	var in struct {
		Outcome string `json:"outcome"`
		Card    string `json:"card"`
	}
	if err := httpx.Decode(w, r, &in, 1<<10); err != nil {
		httpx.Fail(w, r, err)
		return
	}
	if in.Outcome != "approve" && in.Outcome != "decline" && in.Outcome != "cancel" {
		httpx.Fail(w, r, httpx.ValidationError([]httpx.FieldError{{Field: "outcome", Code: "invalid", Message: "Choose approve, decline or cancel."}}))
		return
	}
	s, err := p.Complete(r.Context(), ref, in.Outcome, in.Card)
	if errors.Is(err, ErrNotFound) {
		err = httpx.ErrNotFound
	}
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.JSON(w, r, http.StatusOK, s)
}
