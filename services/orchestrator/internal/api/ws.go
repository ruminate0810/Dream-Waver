package api

import (
	"encoding/json"
	"net/http"
	"sync"
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
