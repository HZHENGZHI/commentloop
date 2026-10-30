package agentmcp

import (
	"context"
	"testing"

	"github.com/hzhengzhi/commentloop/internal/agentclient"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	unreachable := "http://127.0.0.1:1"
	s := &server{client: agentclient.New(unreachable), explicitURL: unreachable}
	serverT, clientT := mcp.NewInMemoryTransports()
	if _, err := s.mcpServer("test").Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestToolsListedThroughSDK(t *testing.T) {
	session := connect(t)
	res, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != len(tools()) {
		t.Fatalf("got %d tools, want %d", len(res.Tools), len(tools()))
	}
	if session.InitializeResult().Instructions != serverInstructions {
		t.Fatal("instructions not advertised")
	}
}

func TestToolFailureIsToolErrorNotProtocolError(t *testing.T) {
	session := connect(t)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "workspace_info", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("protocol error: %v", err)
	}
	if !res.IsError || len(res.Content) == 0 {
		t.Fatalf("want tool error result, got %+v", res)
	}
	if _, ok := res.Content[0].(*mcp.TextContent); !ok {
		t.Fatalf("content type %T", res.Content[0])
	}
}
