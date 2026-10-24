package server

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"mdlive/internal/ignore"
)

const (
	maxRemoteFiles    = 5000
	maxRemoteDepth    = 64
	maxRemoteDoc      = 8 << 20
	maxRemoteRaw      = 64 << 20
	remoteMetaTimeout = 30 * time.Second
	remoteIOTimeout   = 60 * time.Second
)

type remoteConfig struct {
	Host    string `json:"host"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	KeyPath string `json:"keyPath,omitempty"`
	Path    string `json:"path"`
	Name    string `json:"name,omitempty"`
	// KnownHostsPath is only used by tests; production uses ~/.ssh/known_hosts.
	KnownHostsPath string `json:"-"`
}

type remoteFS struct {
	cfg       remoteConfig
	conn      net.Conn
	ssh       *ssh.Client
	sftp      *sftp.Client
	mu        sync.Mutex
	closeOnce sync.Once
}

type remoteFile struct {
	Rel     string
	Name    string
	Size    int64
	ModTime time.Time
}

func newRemoteFS(cfg remoteConfig) (*remoteFS, error) {
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.User = strings.TrimSpace(cfg.User)
	cfg.KeyPath = strings.TrimSpace(cfg.KeyPath)
	cfg.Path = strings.TrimSpace(cfg.Path)
	if cfg.Host == "" {
		return nil, fmt.Errorf("远程主机不能为空")
	}
	if cfg.User == "" {
		return nil, fmt.Errorf("SSH 用户名不能为空")
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("SSH 端口无效")
	}
	if cfg.Path == "" {
		return nil, fmt.Errorf("远程目录不能为空")
	}

	auths, agentConn, err := remoteAuth(cfg)
	if err != nil {
		return nil, err
	}
	if agentConn != nil {
		defer agentConn.Close()
	}
	hostKey, err := remoteHostKeyCallback(cfg)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("SSH 连接失败：%w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auths,
		HostKeyCallback: hostKey,
		Timeout:         15 * time.Second,
	})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("SSH 连接失败：%w", err)
	}
	sshClient := ssh.NewClient(sshConn, chans, reqs)

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("SFTP 连接失败：%w", err)
	}
	root, err := resolveRemoteRoot(sftpClient, cfg.Path)
	if err != nil {
		_ = sftpClient.Close()
		_ = sshClient.Close()
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	cfg.Path = root
	return &remoteFS{cfg: cfg, conn: conn, ssh: sshClient, sftp: sftpClient}, nil
}

func remoteAuth(cfg remoteConfig) ([]ssh.AuthMethod, net.Conn, error) {
	var signers []ssh.Signer
	var agentConn net.Conn

	keyPaths := make([]string, 0, 4)
	explicit := cfg.KeyPath != ""
	if explicit {
		keyPaths = append(keyPaths, cfg.KeyPath)
	} else if home, err := os.UserHomeDir(); err == nil {
		for _, name := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
			keyPaths = append(keyPaths, path.Join(home, ".ssh", name))
		}
	}
	for _, keyPath := range keyPaths {
		expanded, err := expandHome(keyPath)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(expanded)
		if err != nil {
			if explicit {
				_ = agentConnClose(agentConn)
				return nil, nil, fmt.Errorf("读取 SSH 私钥失败：%w", err)
			}
			continue
		}
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			if explicit {
				_ = agentConnClose(agentConn)
				return nil, nil, fmt.Errorf("解析 SSH 私钥失败（加密私钥请使用 ssh-agent）：%w", err)
			}
			continue
		}
		signers = append(signers, signer)
	}

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		conn, err := net.Dial("unix", sock)
		if err == nil {
			agentClient := agent.NewClient(conn)
			if agentSigners, err := agentClient.Signers(); err == nil {
				signers = append(signers, agentSigners...)
				agentConn = conn
			} else {
				_ = conn.Close()
			}
		}
	}

	if len(signers) == 0 {
		_ = agentConnClose(agentConn)
		return nil, nil, fmt.Errorf("没有可用的 SSH 认证方式：请启动 ssh-agent，或填写未加密私钥路径")
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signers...)}, agentConn, nil
}

func agentConnClose(conn net.Conn) error {
	if conn == nil {
		return nil
	}
	return conn.Close()
}

func remoteHostKeyCallback(cfg remoteConfig) (ssh.HostKeyCallback, error) {
	paths := []string{}
	if cfg.KnownHostsPath != "" {
		paths = append(paths, cfg.KnownHostsPath)
	} else if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, path.Join(home, ".ssh", "known_hosts"))
	}
	existing := make([]string, 0, len(paths))
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			existing = append(existing, p)
		}
	}
	if len(existing) == 0 {
		return nil, fmt.Errorf("未找到 known_hosts：请先在终端执行 ssh %s@%s 接受主机指纹", cfg.User, cfg.Host)
	}
	cb, err := knownhosts.New(existing...)
	if err != nil {
		return nil, fmt.Errorf("读取 known_hosts 失败：%w", err)
	}
	return cb, nil
}

func resolveRemoteRoot(client *sftp.Client, raw string) (string, error) {
	if raw == "~" || strings.HasPrefix(raw, "~/") {
		home, err := client.RealPath(".")
		if err != nil {
			return "", fmt.Errorf("无法解析远程主目录：%w", err)
		}
		if raw == "~" {
			raw = home
		} else {
			raw = path.Join(home, strings.TrimPrefix(raw, "~/"))
		}
	}
	resolved, err := client.RealPath(raw)
	if err != nil {
		return "", fmt.Errorf("远程目录不可用：%w", err)
	}
	info, err := client.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("远程目录不可用：%w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("远程路径不是目录")
	}
	return path.Clean(resolved), nil
}

func (r *remoteFS) beginOp(timeout time.Duration) {
	if r.conn != nil {
		_ = r.conn.SetDeadline(time.Now().Add(timeout))
	}
}

func (r *remoteFS) endOp() {
	if r.conn != nil {
		_ = r.conn.SetDeadline(time.Time{})
	}
}

func (r *remoteFS) closeLocked() {
	if r.sftp != nil {
		_ = r.sftp.Close()
		r.sftp = nil
	}
	if r.ssh != nil {
		_ = r.ssh.Close()
		r.ssh = nil
	}
	if r.conn != nil {
		_ = r.conn.Close()
		r.conn = nil
	}
}

func (r *remoteFS) opError(err error) error {
	if err == nil {
		return nil
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		r.closeLocked()
	}
	return err
}

func (r *remoteFS) list() ([]remoteFile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sftp == nil {
		return nil, fmt.Errorf("远程连接已关闭")
	}
	r.beginOp(remoteMetaTimeout)
	defer r.endOp()

	out := make([]remoteFile, 0, 64)
	var walk func(dir, rel string, depth int) error
	walk = func(dir, rel string, depth int) error {
		if depth > maxRemoteDepth {
			return nil
		}
		items, err := r.sftp.ReadDir(dir)
		if err != nil {
			if rel == "" {
				return err
			}
			return nil
		}
		for _, item := range items {
			name := item.Name()
			if ignore.IsTemp(name) {
				continue
			}
			childRel := path.Join(rel, name)
			full := path.Join(dir, name)
			if item.IsDir() {
				if ignore.SkipDir(name) || item.Mode()&os.ModeSymlink != 0 {
					continue
				}
				if err := walk(full, childRel, depth+1); err != nil {
					return err
				}
				continue
			}
			if item.Mode()&os.ModeSymlink != 0 || !ignore.IsMarkdown(name) {
				continue
			}
			out = append(out, remoteFile{
				Rel:     childRel,
				Name:    name,
				Size:    item.Size(),
				ModTime: item.ModTime(),
			})
			if len(out) >= maxRemoteFiles {
				return errTooManyRemoteFiles
			}
		}
		return nil
	}
	if err := walk(r.cfg.Path, "", 0); err != nil && err != errTooManyRemoteFiles {
		return nil, r.opError(err)
	}
	return out, nil
}

var errTooManyRemoteFiles = fmt.Errorf("remote file limit reached")

func (r *remoteFS) read(rel string, max int64) ([]byte, os.FileInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sftp == nil {
		return nil, nil, fmt.Errorf("远程连接已关闭")
	}
	r.beginOp(remoteIOTimeout)
	defer r.endOp()
	full := path.Join(r.cfg.Path, rel)
	info, err := r.sftp.Stat(full)
	if err != nil {
		return nil, nil, r.opError(err)
	}
	if info.IsDir() {
		return nil, nil, fmt.Errorf("远程路径是目录")
	}
	if info.Size() > max {
		return nil, nil, fmt.Errorf("远程文件过大")
	}
	f, err := r.sftp.Open(full)
	if err != nil {
		return nil, nil, r.opError(err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, nil, r.opError(err)
	}
	if int64(len(data)) > max {
		return nil, nil, fmt.Errorf("远程文件过大")
	}
	return data, info, nil
}

func (r *remoteFS) write(rel string, data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sftp == nil {
		return fmt.Errorf("远程连接已关闭")
	}
	r.beginOp(remoteIOTimeout)
	defer r.endOp()
	full := path.Join(r.cfg.Path, rel)
	dir := path.Dir(full)
	if err := r.sftp.MkdirAll(dir); err != nil {
		return r.opError(err)
	}
	mode := os.FileMode(0o644)
	if info, err := r.sftp.Stat(full); err == nil {
		mode = info.Mode().Perm()
	}
	tmp := path.Join(dir, fmt.Sprintf(".mdlive-%d", time.Now().UnixNano()))
	f, err := r.sftp.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return r.opError(err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = r.sftp.Remove(tmp)
		return r.opError(err)
	}
	if err := f.Close(); err != nil {
		_ = r.sftp.Remove(tmp)
		return r.opError(err)
	}
	_ = r.sftp.Chmod(tmp, mode)
	if err := r.sftp.PosixRename(tmp, full); err != nil {
		if err := r.sftp.Rename(tmp, full); err != nil {
			_ = r.sftp.Remove(full)
			if err2 := r.sftp.Rename(tmp, full); err2 != nil {
				_ = r.sftp.Remove(tmp)
				return r.opError(err2)
			}
		}
	}
	return nil
}

func (r *remoteFS) remove(rel string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sftp == nil {
		return fmt.Errorf("远程连接已关闭")
	}
	r.beginOp(remoteMetaTimeout)
	defer r.endOp()
	return r.opError(r.sftp.Remove(path.Join(r.cfg.Path, rel)))
}

func (r *remoteFS) stat(rel string) (os.FileInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sftp == nil {
		return nil, fmt.Errorf("远程连接已关闭")
	}
	r.beginOp(remoteMetaTimeout)
	defer r.endOp()
	info, err := r.sftp.Stat(path.Join(r.cfg.Path, rel))
	return info, r.opError(err)
}

func (r *remoteFS) Close() error {
	var err error
	r.closeOnce.Do(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.closeLocked()
	})
	return err
}

func (r *remoteFS) reconnect() error {
	fs, err := newRemoteFS(r.cfg)
	if err != nil {
		return err
	}
	r.mu.Lock()
	oldSftp, oldSSH, oldConn := r.sftp, r.ssh, r.conn
	r.sftp, r.ssh, r.conn = fs.sftp, fs.ssh, fs.conn
	r.mu.Unlock()
	if oldSftp != nil {
		_ = oldSftp.Close()
	}
	if oldSSH != nil {
		_ = oldSSH.Close()
	}
	if oldConn != nil {
		_ = oldConn.Close()
	}
	return nil
}

// browse lists only directories below raw. It is used by the remote folder
// picker before a root is added.
func (r *remoteFS) browse(raw string, showHidden bool) (browseResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sftp == nil {
		return browseResponse{}, fmt.Errorf("远程连接已关闭")
	}
	r.beginOp(remoteMetaTimeout)
	defer r.endOp()
	home, err := r.sftp.RealPath(".")
	if err != nil {
		return browseResponse{}, fmt.Errorf("无法解析远程主目录：%w", err)
	}
	if strings.TrimSpace(raw) == "" {
		raw = home
	}
	if raw == "~" || strings.HasPrefix(raw, "~/") {
		if raw == "~" {
			raw = home
		} else {
			raw = path.Join(home, strings.TrimPrefix(raw, "~/"))
		}
	}
	current, err := r.sftp.RealPath(raw)
	if err != nil {
		return browseResponse{}, fmt.Errorf("远程目录不可用：%w", err)
	}
	current = path.Clean(current)
	info, err := r.sftp.Stat(current)
	if err != nil {
		return browseResponse{}, fmt.Errorf("远程目录不可用：%w", err)
	}
	if !info.IsDir() {
		return browseResponse{}, fmt.Errorf("远程路径不是目录")
	}
	items, err := r.sftp.ReadDir(current)
	if err != nil {
		return browseResponse{}, fmt.Errorf("无法读取远程目录：%w", err)
	}
	entries := make([]browseEntry, 0, len(items))
	truncated := false
	for _, item := range items {
		name := item.Name()
		if strings.HasPrefix(name, ".") && !showHidden {
			continue
		}
		full := path.Join(current, name)
		isDir := item.IsDir()
		if !isDir && item.Mode()&os.ModeSymlink != 0 {
			if target, err := r.sftp.Stat(full); err == nil {
				isDir = target.IsDir()
			}
		}
		if !isDir {
			continue
		}
		entries = append(entries, browseEntry{
			Name: name, Path: full, Hidden: strings.HasPrefix(name, "."),
		})
		if len(entries) >= maxBrowseEntries {
			truncated = true
			break
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := strings.ToLower(entries[i].Name), strings.ToLower(entries[j].Name)
		if a == b {
			return entries[i].Name < entries[j].Name
		}
		return a < b
	})
	parent := ""
	if current != path.Dir(current) {
		parent = path.Dir(current)
	}
	return browseResponse{
		Path: current, Parent: parent, Entries: entries,
		Shortcuts: []browseShortcut{
			{Name: "主目录", Path: home},
			{Name: "根目录", Path: "/"},
		},
		Truncated: truncated,
	}, nil
}
