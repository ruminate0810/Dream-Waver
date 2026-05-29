package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 16 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		// MVP: trust the chi/cors layer to gate origins; refine in prod.
		return true
	},
}

// SessionEvents upgrades the connection to WebSocket and streams every event
// emitted for a session to the client until either side closes.
//
// The frontend connects right after CreateSlides returns, using
// `events_url` from the response. Each frame is one JSON-encoded event.Event.
func (h *handlers) SessionEvents(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		errorJSON(w, http.StatusBadRequest, "missing session id")
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // upgrader has already written an error
	}
	defer conn.Close()

	em := h.deps.Hub.Subscribe(sessionID, 128)
	defer h.deps.Hub.Unsubscribe(sessionID, em)

	// Sprint W-followup — keepalive that actually works.
	//
	// Old code only WROTE pings, never read pongs. gorilla/websocket
	// requires the application to call ReadMessage for ping/pong
	// frames to be processed; without it pongs pile up in the OS
	// read buffer and after ~3 minutes WriteMessage starts to fail —
	// exactly the "WS dropped at 3m08s mid-pause-gate" symptom that
	// stranded the user on the spinner. Fix: a 60s read deadline
	// that the pong handler extends on every pong, plus a tiny
	// reader goroutine that drains the connection so pongs reach
	// the handler.
	const (
		pongWait   = 60 * time.Second
		pingPeriod = 25 * time.Second // < pongWait so we always have headroom
	)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	// Closing this channel from EITHER the writer or the reader exit
	// path lets the other side notice the conn is dead and unwind.
	done := make(chan struct{})
	var closeOnce sync.Once
	closeDone := func() { closeOnce.Do(func() { close(done) }) }

	// Reader: drain messages so pong frames reach the handler. We
	// don't expect application messages from clients; any unexpected
	// read error (timeout, close, EOF) means the conn is gone.
	go func() {
		defer closeDone()
		for {
			if _, _, rerr := conn.NextReader(); rerr != nil {
				return
			}
		}
	}()

	// Ping loop. Stops the moment the writer / reader detects a
	// dead conn so we don't leak the ticker goroutine.
	go func() {
		t := time.NewTicker(pingPeriod)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if err := conn.WriteControl(
					websocket.PingMessage, nil,
					time.Now().Add(5*time.Second),
				); err != nil {
					closeDone()
					return
				}
			case <-done:
				return
			}
		}
	}()

	for {
		select {
		case ev, ok := <-em.Channel():
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			payload, _ := json.Marshal(ev)
			if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				closeDone()
				return
			}
		case <-done:
			return
		}
	}
}

// SessionEventLog is the Sprint AA.2 replay endpoint. Returns the
// persisted chat_events for a session as a JSON array ordered by seq
// ascending. The frontend calls this on cold mount (rebuild the full
// conversation lost from React state on refresh) AND on WS reconnect
// (?since=<lastSeq> back-fills only the gap).
//
//	GET /api/v1/sessions/{id}/log?since=<seq>&limit=<n>
//
// Each element mirrors a live WS frame + a `seq` cursor:
//
//	{ "seq": 12, "session_id": "...", "kind": "tool.start", "at": "...", "data": {...} }
func (h *handlers) SessionEventLog(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		errorJSON(w, http.StatusBadRequest, "missing session id")
		return
	}
	if h.deps.Store == nil || h.deps.Store.ChatEvents == nil {
		// No durable log (in-memory dev w/o DB) — empty array so the
		// FE hydrate path is a harmless no-op.
		writeJSON(w, http.StatusOK, map[string]any{"events": []any{}})
		return
	}
	sid, err := uuid.Parse(sessionID)
	if err != nil {
		errorJSON(w, http.StatusBadRequest, "invalid session id")
		return
	}

	var since int64
	if s := r.URL.Query().Get("since"); s != "" {
		if v, perr := strconv.ParseInt(s, 10, 64); perr == nil {
			since = v
		}
	}
	limit := 500
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, perr := strconv.Atoi(l); perr == nil && v > 0 {
			limit = v
		}
	}

	rows, err := h.deps.Store.ChatEvents.ListSince(r.Context(), sid, since, limit)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "list events: "+err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, ev := range rows {
		out = append(out, map[string]any{
			"seq":        ev.Seq,
			"session_id": ev.SessionID.String(),
			"kind":       ev.Kind,
			"at":         ev.At,
			"data":       json.RawMessage(ev.Data),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}
