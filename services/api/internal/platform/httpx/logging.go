package httpx

import (
	"context"
	"io"
	"log/slog"
)

// requestIDHandler adds the request id to every log record written with a
// request context, so one id ties together everything a request did.
type requestIDHandler struct{ slog.Handler }

func (h requestIDHandler) Handle(ctx context.Context, rec slog.Record) error {
	if id := RequestID(ctx); id != "" {
		rec.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, rec)
}

func (h requestIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return requestIDHandler{h.Handler.WithAttrs(attrs)}
}

func (h requestIDHandler) WithGroup(name string) slog.Handler {
	return requestIDHandler{h.Handler.WithGroup(name)}
}

// NewLogger builds the JSON logger used by every binary.
func NewLogger(w io.Writer, level slog.Level, service, env string) *slog.Logger {
	base := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(requestIDHandler{base}).With("service", service, "env", env)
}
