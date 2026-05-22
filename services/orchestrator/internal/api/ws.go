package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
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

	// Ping loop to keep proxies happy.
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
		}
	}()

	for ev := range em.Channel() {
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		payload, _ := json.Marshal(ev)
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			return
		}
	}
}
