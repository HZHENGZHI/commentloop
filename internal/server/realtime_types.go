package server

import (
	"sync"

	"github.com/gorilla/websocket"
)

// Realtime (WebSocket) hub types.

// Client is one live browser connection.
type Client struct {
	hub  *Hub
	srv  *Server
	conn *websocket.Conn
	send chan []byte

	// path is guarded by hub.mu.
	path string

	done      chan struct{}
	closeOnce sync.Once
}

// Hub tracks connected clients and routes messages by document path.
type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
}

type inMessage struct {
	Type    string `json:"type"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

type outMessage struct {
	Type     string `json:"type"`
	Path     string `json:"path,omitempty"`
	Content  string `json:"content"`
	HTML     string `json:"html"`
	Revision string `json:"revision,omitempty"`
	Origin   string `json:"origin,omitempty"`
	Message  string `json:"message,omitempty"`
	Status   string `json:"status,omitempty"`
}
