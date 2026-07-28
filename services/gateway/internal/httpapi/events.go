package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sgao19/erp-go/pkg/grpcx"
)

// heartbeat keeps the connection alive through proxies that cut idle streams.
// Comment lines are ignored by the client but count as traffic.
const heartbeat = 25 * time.Second

// streamEvents is the one long-lived response in the whole gateway: a
// Server-Sent Events stream carrying "what you are looking at changed" hints.
//
// SSE rather than WebSocket because the traffic is one-directional. The
// browser never pushes anything back; it re-reads through the normal REST API,
// so permissions are checked exactly where they always were and there is no
// second code path that can disagree with the first.
func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	op, ok := grpcx.OperatorFromContext(r.Context())
	if !ok || op.EmployeeID == 0 {
		s.writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_MISSING", "缺少登录凭证")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, "GATEWAY_NO_STREAMING", "服务端不支持流式响应")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Tell nginx and friends not to buffer; a buffered event stream is a
	// stream that arrives all at once, minutes late.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	events := s.Live.Listen(ctx, op.TenantID, op.EmployeeID)
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return // browser went away
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case e, open := <-events:
			if !open {
				return
			}
			body, err := json.Marshal(e)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, body); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
