package event

import (
	"time"

	"github.com/alexandrevicenzi/go-sse"
	"github.com/labstack/echo/v4"
)

type (
	Hub struct {
		server *sse.Server
	}
)

// heartbeatInterval keeps the stream from looking idle. A reverse proxy or
// tunnel in front of pprotein drops a connection that carries nothing for a
// minute or so, and a collection can run for longer than that.
const heartbeatInterval = 15 * time.Second

func NewHub() *Hub {
	h := &Hub{
		server: sse.NewServer(&sse.Options{}),
	}
	go h.heartbeat()
	return h
}

// heartbeat sends a named event that the view does not listen for, so it
// reaches the proxy without being parsed as an entry.
func (h *Hub) heartbeat() {
	for range time.Tick(heartbeatInterval) {
		h.server.SendMessage("", sse.NewMessage("", "", "ping"))
	}
}

func (h *Hub) RegisterHandlers(g *echo.Group) {
	g.GET("", echo.WrapHandler(h.server))
}
func (h *Hub) Publish(message []byte) {
	h.server.SendMessage("", sse.SimpleMessage(string(message)))
}
