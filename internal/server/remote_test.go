package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type testSSHServer struct {
	host       string
	port       int
	home       string
	keyPath    string
	knownHosts string
	remoteRoot string

	ln  net.Listener
	wg  sync.WaitGroup
	cfg *ssh.ServerConfig
}

func startTestSSHServer(t *testing.T, remoteRoot string) *testSSHServer {
	t.Helper()

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	clientPub, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	keyBlock, err := ssh.MarshalPrivateKey(clientPriv, "")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(keyBlock), 0o600); err != nil {
		t.Fatal(err)
	}
	hostPub, err := ssh.NewPublicKey(hostPriv.Public())
	if err != nil {
		t.Fatal(err)
	}
	authorized := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(hostPub)))
	knownHosts := filepath.Join(sshDir, "known_hosts")
	line := fmt.Sprintf("[127.0.0.1]:%d %s\n", port, authorized)
	if err := os.WriteFile(knownHosts, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")

	clientPubKey, err := ssh.NewPublicKey(clientPub)
	if err != nil {
		t.Fatal(err)
	}
	expected := ssh.FingerprintSHA256(clientPubKey)
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if ssh.FingerprintSHA256(key) == expected {
				return nil, nil
			}
			return nil, fmt.Errorf("unknown public key %s", ssh.FingerprintSHA256(key))
		},
	}
	cfg.AddHostKey(hostSigner)

	srv := &testSSHServer{
		host: "127.0.0.1", port: port, home: home,
		keyPath: keyPath, knownHosts: knownHosts,
		remoteRoot: remoteRoot, ln: ln, cfg: cfg,
	}
	srv.wg.Add(1)
	go srv.accept()
	t.Cleanup(func() {
		_ = ln.Close()
		srv.wg.Wait()
	})
	return srv
}

func (s *testSSHServer) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			sshConn, chans, reqs, err := ssh.NewServerConn(conn, s.cfg)
			if err != nil {
				_ = conn.Close()
				return
			}
			defer sshConn.Close()
			go ssh.DiscardRequests(reqs)
			for newChannel := range chans {
				if newChannel.ChannelType() != "session" {
					_ = newChannel.Reject(ssh.UnknownChannelType, "session only")
					continue
				}
				channel, requests, err := newChannel.Accept()
				if err != nil {
					continue
				}
				s.wg.Add(1)
				go func() {
					defer s.wg.Done()
					defer channel.Close()
					for req := range requests {
						switch req.Type {
						case "subsystem":
							var payload struct{ Name string }
							_ = ssh.Unmarshal(req.Payload, &payload)
							if payload.Name != "sftp" {
								_ = req.Reply(false, nil)
								continue
							}
							_ = req.Reply(true, nil)
							server, err := sftp.NewServer(channel)
							if err != nil {
								return
							}
							_ = server.Serve()
							_ = server.Close()
							return
						default:
							_ = req.Reply(false, nil)
						}
					}
				}()
			}
		}()
	}
}

func (s *testSSHServer) remoteConfig(path string) remoteConfig {
	return remoteConfig{
		Host: s.host, Port: s.port, User: "tester",
		KeyPath: s.keyPath, Path: path, Name: "test-remote",
	}
}

func TestRemoteFS(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.md"), "# Remote A\n")
	mustMkdir(t, filepath.Join(root, "sub"))
	mustWrite(t, filepath.Join(root, "sub", "b.md"), "# Remote B\n")
	mustWrite(t, filepath.Join(root, "sub", "pic.png"), "not really an image")

	srv := startTestSSHServer(t, root)
	fs, err := newRemoteFS(srv.remoteConfig(root))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = fs.Close() })

	files, err := fs.list()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("remote files = %d, want 2 (%+v)", len(files), files)
	}

	data, _, err := fs.read("a.md", maxRemoteDoc)
	if err != nil || string(data) != "# Remote A\n" {
		t.Fatalf("read: %q err=%v", data, err)
	}
	if err := fs.write("sub/new.md", []byte("# New\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	onDisk, err := os.ReadFile(filepath.Join(root, "sub", "new.md"))
	if err != nil || string(onDisk) != "# New\n" {
		t.Fatalf("remote write not visible: %q err=%v", onDisk, err)
	}
	if err := fs.remove("sub/new.md"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "sub", "new.md")); !os.IsNotExist(err) {
		t.Fatalf("remote remove failed: %v", err)
	}
}

func TestRemoteRootIntegration(t *testing.T) {
	remoteRoot := t.TempDir()
	mustWrite(t, filepath.Join(remoteRoot, "remote.md"), "# Remote\n")
	mustMkdir(t, filepath.Join(remoteRoot, "nested"))
	mustWrite(t, filepath.Join(remoteRoot, "nested", "deep.md"), "# Deep remote\n")
	srv := startTestSSHServer(t, remoteRoot)

	primary := t.TempDir()
	mustWrite(t, filepath.Join(primary, "local.md"), "# Local\n")
	stateDir := t.TempDir()

	app, err := New(Options{
		Root: primary, StateDir: stateDir, WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	app.remotePollInterval = 200 * time.Millisecond

	// Remote folder picker: open a browse session and list directories.
	sessionRec := doJSON(t, app, http.MethodPost, "/api/remote/session", remoteSessionRequest{
		Host: srv.host, Port: srv.port, User: "tester", KeyPath: srv.keyPath,
	})
	if sessionRec.Code != http.StatusOK {
		t.Fatalf("remote session status=%d body=%s", sessionRec.Code, sessionRec.Body)
	}
	session := decode[remoteSessionResponse](t, sessionRec)
	if session.Session == "" || session.Home == "" {
		t.Fatalf("remote session = %+v", session)
	}
	browseURL := "/api/remote/browse?session=" + url.QueryEscape(session.Session) + "&path=" + url.QueryEscape(remoteRoot)
	browseRec := doJSON(t, app, http.MethodGet, browseURL, nil)
	if browseRec.Code != http.StatusOK {
		t.Fatalf("remote browse status=%d body=%s", browseRec.Code, browseRec.Body)
	}
	browsed := decode[browseResponse](t, browseRec)
	foundNested := false
	for _, entry := range browsed.Entries {
		if entry.Name == "nested" {
			foundNested = true
		}
	}
	if !foundNested {
		t.Fatalf("remote browse entries = %+v", browsed.Entries)
	}
	rec := doJSON(t, app, http.MethodDelete, "/api/remote/session?session="+url.QueryEscape(session.Session), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("close session status=%d", rec.Code)
	}
	rec = doJSON(t, app, http.MethodGet, browseURL, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("closed session browse status=%d, want 404", rec.Code)
	}

	req := addRemoteRootRequest{
		Name: "remote-notes", Host: srv.host, Port: srv.port,
		User: "tester", KeyPath: srv.keyPath, Path: remoteRoot,
	}
	rec = doJSON(t, app, http.MethodPost, "/api/roots/remote", req)
	if rec.Code != http.StatusOK {
		t.Fatalf("add remote status=%d body=%s", rec.Code, rec.Body)
	}
	added := decode[addRootResponse](t, rec)
	if added.Root.Kind != rootKindRemote {
		t.Fatalf("root kind = %q", added.Root.Kind)
	}

	// Adding a remote root must synchronously prime the file cache so the UI
	// can show files immediately after the connection completes.
	immediate := decode[treeResponse](t, doJSON(t, app, http.MethodGet, "/api/tree", nil))
	immediateFiles := 0
	for _, item := range immediate.Roots {
		if item.Kind == rootKindRemote {
			immediateFiles = len(item.Files)
		}
	}
	if immediateFiles != 2 {
		t.Fatalf("remote files after add = %d, want 2 immediately", immediateFiles)
	}

	var remoteTree treeRoot
	var tree treeResponse
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tree = decode[treeResponse](t, doJSON(t, app, http.MethodGet, "/api/tree", nil))
		if len(tree.Roots) != 2 {
			t.Fatalf("roots = %d, want 2", len(tree.Roots))
		}
		remoteTree = treeRoot{}
		for _, item := range tree.Roots {
			if item.Kind == rootKindRemote {
				remoteTree = item
			}
		}
		if remoteTree.ID != "" && len(remoteTree.Files) == 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if remoteTree.ID == "" || len(remoteTree.Files) != 2 {
		t.Fatalf("remote tree = %+v", remoteTree)
	}
	docPath := added.Root.ID + "/remote.md"
	rec = doJSON(t, app, http.MethodGet, docURL(docPath), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("read remote doc status=%d body=%s", rec.Code, rec.Body)
	}
	doc := decode[docResponse](t, rec)
	if !strings.Contains(doc.HTML, "Remote") {
		t.Fatalf("remote HTML = %s", doc.HTML)
	}
	rec = doJSON(t, app, http.MethodPut, "/api/doc", saveRequest{Path: docPath, Content: "# Saved remote\n"})
	if rec.Code != http.StatusOK {
		t.Fatalf("save remote status=%d body=%s", rec.Code, rec.Body)
	}
	onDisk, _ := os.ReadFile(filepath.Join(remoteRoot, "remote.md"))
	if string(onDisk) != "# Saved remote\n" {
		t.Fatalf("remote save not visible: %q", onDisk)
	}

	// Remote polling should push external changes to a subscribed tab.
	hs := httptest.NewServer(app)
	t.Cleanup(hs.Close)
	wsURL := "ws" + strings.TrimPrefix(hs.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if err := conn.WriteJSON(inMessage{Type: "subscribe", Path: docPath}); err != nil {
		t.Fatal(err)
	}
	readUntil(t, conn, "subscribed")
	mustWrite(t, filepath.Join(remoteRoot, "remote.md"), "# Changed remotely\n")
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var external outMessage
	for {
		var msg outMessage
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("waiting for remote external update: %v", err)
		}
		if msg.Type == "doc" && msg.Origin == "external" {
			external = msg
			break
		}
	}
	if !strings.Contains(external.Content, "Changed remotely") {
		t.Fatalf("external remote content = %q", external.Content)
	}

	// Restart: the remote root must be restored using the persisted config.
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
	app2, err := New(Options{
		Root: primary, StateDir: stateDir, WebFS: mapFS(),
		Version: "test", AllowRemote: true,
	})
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	t.Cleanup(func() { _ = app2.Close() })
	tree = decode[treeResponse](t, doJSON(t, app2, http.MethodGet, "/api/tree", nil))
	remoteFound := false
	for _, item := range tree.Roots {
		if item.Kind == rootKindRemote {
			remoteFound = true
		}
	}
	if !remoteFound {
		t.Fatal("remote root was not restored after restart")
	}

	// Removing the remote root closes the connection but keeps remote files.
	rec = doJSON(t, app2, http.MethodDelete, "/api/roots?id="+added.Root.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove remote status=%d body=%s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(remoteRoot, "remote.md")); err != nil {
		t.Fatalf("removing remote root deleted files: %v", err)
	}
}
