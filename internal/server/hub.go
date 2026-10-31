package server

import (
	"encoding/json"

	"github.com/gorilla/websocket"
)

func newClient(hub *Hub, srv *Server, conn *websocket.Conn) *Client {
	return &Client{
		hub:  hub,
		srv:  srv,
		conn: conn,
		send: make(chan []byte, 64),
		done: make(chan struct{}),
	}
}

func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

func newHub() *Hub {
	return &Hub{clients: make(map[*Client]struct{})}
}

func (h *Hub) add(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(c *Client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

func (h *Hub) setPath(c *Client, p string) {
	h.mu.Lock()
	c.path = p
	h.mu.Unlock()
}

func (h *Hub) pathOf(c *Client) string {
	h.mu.RLock()
	p := c.path
	h.mu.RUnlock()
	return p
}

func (h *Hub) send(c *Client, msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.sendRaw(c, data)
}

func (h *Hub) sendRaw(c *Client, data []byte) {
	select {
	case c.send <- data:
	default:
		// Slow client: disconnect rather than blocking the server.
		c.close()
	}
}

func (h *Hub) viewers(path string) []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		if c.path == path {
			out = append(out, c)
		}
	}
	return out
}

func (h *Hub) broadcast(msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	for _, c := range clients {
		h.sendRaw(c, data)
	}
}

func (h *Hub) broadcastPath(path string, msg any) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	for _, c := range h.viewers(path) {
		h.sendRaw(c, data)
	}
}

// broadcastEdit sends a lightweight preview to the editor that made the
// change, and the full document to every other viewer.
func (h *Hub) broadcastEdit(sender *Client, path, content, html, revision string) {
	preview, err := json.Marshal(outMessage{
		Type: "preview", Path: path, HTML: html, Revision: revision,
	})
	if err != nil {
		return
	}
	doc, err := json.Marshal(outMessage{
		Type: "doc", Path: path, Content: content, HTML: html,
		Revision: revision, Origin: "remote",
	})
	if err != nil {
		return
	}
	for _, c := range h.viewers(path) {
		if c == sender {
			h.sendRaw(c, preview)
		} else {
			h.sendRaw(c, doc)
		}
	}
}
