package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxWsMessage   = maxDocSize + 1<<20
	maxContentSize = maxDocSize
)

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !s.hostAllowed(r) {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already writes an error response.
		return
	}
	c := newClient(s.hub, s, conn)
	s.hub.add(c)
	go c.writePump()
	c.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.hub.remove(c)
		c.close()
	}()

	c.conn.SetReadLimit(maxWsMessage)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg inMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			c.hub.send(c, outMessage{Type: "error", Message: "无法解析消息"})
			continue
		}
		c.srv.handleWSMessage(c, msg)
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.close()
	}()

	for {
		select {
		case <-c.done:
			return
		case data := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleWSMessage(c *Client, msg inMessage) {
	switch msg.Type {
	case "subscribe":
		ro, rel, err := s.resolveDocRef(msg.Path)
		if err != nil {
			s.hub.send(c, outMessage{Type: "error", Path: msg.Path, Message: "路径无效"})
			return
		}
		canonical := ro.ID + "/" + rel
		snap, err := s.snapshot(canonical)
		if err != nil {
			s.hub.send(c, outMessage{Type: "error", Path: canonical, Message: "无法读取文件"})
			return
		}
		s.hub.setPath(c, canonical)
		s.hub.send(c, outMessage{
			Type: "doc", Path: canonical, Content: snap.content, HTML: snap.result.HTML,
			Revision: snap.result.Revision, Origin: "init",
		})
		s.hub.send(c, outMessage{Type: "subscribed", Path: canonical})

	case "unsubscribe":
		s.hub.setPath(c, "")
		s.hub.send(c, outMessage{Type: "unsubscribed"})

	case "edit":
		if s.previewOnly {
			s.hub.send(c, outMessage{Type: "error", Message: "审阅模式下前台不能编辑；请让后台 Agent 修改"})
			return
		}
		current := s.hub.pathOf(c)
		if current == "" {
			s.hub.send(c, outMessage{Type: "error", Message: "尚未打开文档"})
			return
		}
		if msg.Path != "" && strings.TrimPrefix(msg.Path, "/") != current {
			s.hub.send(c, outMessage{Type: "error", Message: "文档路径不匹配"})
			return
		}
		if len(msg.Content) > maxContentSize {
			s.hub.send(c, outMessage{Type: "error", Message: "文档过大，实时预览已暂停"})
			return
		}
		res := s.renderer.Render([]byte(msg.Content), current)
		s.hub.broadcastEdit(c, current, msg.Content, res.HTML, res.Revision)

	default:
		s.hub.send(c, outMessage{Type: "error", Message: "未知消息类型"})
	}
}
