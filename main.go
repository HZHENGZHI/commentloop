// commentloop is a local review workspace with a live, server-rendered preview.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/hzhengzhi/commentloop/internal/agentcli"
	"github.com/hzhengzhi/commentloop/internal/agentmcp"
	"github.com/hzhengzhi/commentloop/internal/agentrunner"
	"github.com/hzhengzhi/commentloop/internal/config"
	"github.com/hzhengzhi/commentloop/internal/discovery"
	"github.com/hzhengzhi/commentloop/internal/ignore"
	"github.com/hzhengzhi/commentloop/internal/logging"
	"github.com/hzhengzhi/commentloop/internal/server"
	"github.com/pkg/browser"
)

//go:embed all:web/dist
var webFiles embed.FS

var version = "dev"

func main() {
	migrateLegacyState()
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "agent":
			if err := agentcli.Run(args[1:]); err != nil {
				fmt.Fprintln(os.Stderr, "commentloop agent:", err)
				os.Exit(1)
			}
			return
		case "mcp":
			if err := agentmcp.Run(args[1:], version); err != nil {
				fmt.Fprintln(os.Stderr, "commentloop mcp:", err)
				os.Exit(1)
			}
			return
		case "agent-runner":
			if err := agentrunner.Run(args[1:]); err != nil {
				fmt.Fprintln(os.Stderr, "commentloop agent-runner:", err)
				os.Exit(1)
			}
			return
		case "start":
			if hasImmediateFlag(args[1:]) {
				runServer(args[1:])
				return
			}
			if err := startDaemon(args[1:]); err != nil {
				fmt.Fprintln(os.Stderr, "commentloop start:", err)
				os.Exit(1)
			}
			return
		case "stop":
			if err := stopDaemon(); err != nil {
				fmt.Fprintln(os.Stderr, "commentloop stop:", err)
				os.Exit(1)
			}
			return
		case "status":
			statusDaemon()
			return
		case "run":
			runServer(args[1:])
			return
		case "help":
			runServer([]string{"--help"})
			return
		}
	}

	// The daemon child is the actual server process. It must not try to
	// background itself again. --version/--help stay foreground too.
	if os.Getenv("COMMENTLOOP_DAEMON_CHILD") == "1" || hasImmediateFlag(args) {
		runServer(args)
		return
	}

	// Default invocation starts the server in the background. Use
	// `commentloop run ...` for a foreground process.
	if err := startDaemon(args); err != nil {
		fmt.Fprintln(os.Stderr, "commentloop start:", err)
		os.Exit(1)
	}
}

func runServer(args []string) {
	var extraRoots []string
	flag.Func("add-dir", "Add a directory to the workspace (repeatable)", func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("Directory cannot be empty")
		}
		extraRoots = append(extraRoots, value)
		return nil
	})

	var (
		dirFlag   = flag.String("dir", "", "Directory to open (defaults to the current directory; a Markdown file is also accepted)")
		_         = flag.String("addr", "127.0.0.1:4571", "Listen address")
		noOpen    = flag.Bool("no-open", false, "Do not open a browser")
		devMode   = flag.Bool("dev", false, "Development mode: start even when the frontend is not built (for Vite)")
		debugMode = flag.Bool("debug", false, "Debug mode: review mode (read-only UI, agent writes) + hot reload of frontend assets")
		preview   = flag.Bool("preview", false, "Compatibility flag: Commentloop is review-only by default; the UI is read-only and agents write through MCP/CLI")
		webDir    = flag.String("web-dir", "web/dist", "Frontend directory loaded from disk in debug mode")
		noState   = flag.Bool("no-state", false, "Do not read or save workspace roots")
		_         = flag.String("state-dir", "", "State directory (default: user config dir/commentloop)")
		showVer   = flag.Bool("version", false, "Print version and exit")
		configArg = flag.String("config", "", "Config file (YAML; default: user config dir/commentloop/config.yaml if present; env COMMENTLOOP_CONFIG)")
		_         = flag.String("log-level", "info", "Log level: debug, info, warn or error (env COMMENTLOOP_LOG_LEVEL)")
		_         = flag.String("log-format", "text", "Log format: text or json (env COMMENTLOOP_LOG_FORMAT)")
	)
	flag.BoolVar(showVer, "v", false, "Print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "commentloop %s — AI review loop\n\n", version)
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: commentloop [start|run|stop|status] [options] [directory or Markdown file]\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "Subcommands:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  start              Start in the background (default)\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  run                Run in the foreground (debug / process manager)\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  stop               Stop the most recently started instance\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  status             Show runtime status\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  agent-runner       Host Agent Runner; claims external agent jobs\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  agent ...          CLI for code agents (list/read/write/search/changes)\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  agent-runner       Host Agent Runner; claims review jobs\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  mcp                MCP stdio server for MCP-capable agents\n\n")
		flag.PrintDefaults()
	}
	flag.CommandLine.Parse(reorderArgs(flag.CommandLine, args))

	if *showVer {
		fmt.Println(version)
		return
	}
	cfg, cfgFile, err := loadConfig(*configArg)
	if err != nil {
		fatal("%v", err)
	}
	if _, err := logging.Setup(os.Stderr, cfg.Log.Level, cfg.Log.Format); err != nil {
		fatal("%v", err)
	}
	if cfgFile != "" {
		slog.Info("loaded config", "file", cfgFile)
	}

	rootArg := *dirFlag
	if rootArg == "" && flag.NArg() > 0 {
		rootArg = flag.Arg(0)
	}
	if rootArg == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fatal("Could not determine current directory: %v", err)
		}
		rootArg = cwd
	}

	abs, err := filepath.Abs(rootArg)
	if err != nil {
		fatal("Invalid path %q: %v", rootArg, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		fatal("Could not access %q: %v", abs, err)
	}
	root := abs
	initial := ""
	if !info.IsDir() {
		root = filepath.Dir(abs)
		initial = filepath.Base(abs)
		if !ignore.IsMarkdown(initial) {
			fatal("Not a Markdown file: %s", abs)
		}
	}

	dist, err := fs.Sub(webFiles, "web/dist")
	if err != nil {
		fatal("Frontend assets unavailable: %v", err)
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		if !*devMode && !*debugMode {
			fatal("Frontend is not built. Run make build, or use make dev-backend + make dev-web for development.")
		}
		slog.Warn("embedded frontend is not built; using the disk frontend or Vite dev server")
	}

	ln, actualAddr, err := listenWithFallback(cfg.Server.Addr)
	if err != nil {
		fatal("Could not listen on %s: %v", cfg.Server.Addr, err)
	}

	stateDirPath := ""
	if !*noState {
		dir := cfg.Server.StateDir
		if dir == "" {
			if userDir, err := os.UserConfigDir(); err == nil {
				dir = filepath.Join(userDir, "commentloop")
			}
		}
		if dir != "" {
			absState, err := filepath.Abs(dir)
			if err == nil {
				stateDirPath = absState
			}
		}
	}

	// commentloop is review-only: the browser is for reading, commenting and
	// reviewing; documents are written by the background Agent through MCP/CLI.
	// --preview is kept as a compatibility flag.
	_ = *preview
	readOnly := true
	app, err := server.New(server.Options{
		Root:        root,
		ExtraRoots:  extraRoots,
		StateDir:    stateDirPath,
		WebFS:       dist,
		Version:     version,
		AllowRemote: !isLoopbackAddr(actualAddr),
		PreviewOnly: readOnly,
		Debug:       *debugMode,
		WebDir:      *webDir,
		Config:      cfg,
	})
	if err != nil {
		fatal("Startup failed: %v", err)
	}
	defer app.Close()

	baseURL := "http://" + displayAddr(actualAddr) + "/"
	app.SetAgentBaseURL(baseURL)
	browseURL := baseURL
	if initial != "" {
		browseURL += "?file=" + url.QueryEscape(app.PrimaryDocPath(initial))
	}
	discoveryPath, discoveryErr := discovery.Write(baseURL, version)
	if discoveryErr != nil {
		slog.Error("failed to write agent discovery file", "err", discoveryErr)
	} else {
		defer discovery.RemoveOwner(discoveryPath, os.Getpid())
	}

	httpSrv := &http.Server{
		Handler:           app,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelError),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	mode := "edit + preview"
	if *debugMode {
		mode = "debug · review mode · frontend hot reload"
	} else if readOnly {
		mode = "review mode (UI read-only, agent writes)"
	}
	if strings.EqualFold(cfg.Log.Format, "json") {
		// Keep the (daemon) log file valid JSON lines.
		slog.Info("commentloop started", "version", version, "root", app.Root(),
			"roots", app.RootCount(), "mode", mode, "url", browseURL)
	} else {
		fmt.Printf("commentloop %s\n", version)
		fmt.Printf("  Root      %s\n", app.Root())
		fmt.Printf("  Roots     %d\n", app.RootCount())
		fmt.Printf("  Mode      %s\n", mode)
		fmt.Printf("  URL       %s\n", browseURL)
		fmt.Printf("  Stop      Ctrl+C\n")
	}

	if !*noOpen {
		if err := browser.OpenURL(browseURL); err != nil {
			slog.Warn("could not open the browser", "err", err)
		}
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	notifyStopRequests(stop)
	go func() {
		<-stop
		slog.Info("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
		_ = app.Close()
	}()

	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fatal("Server exited unexpectedly: %v", err)
	}
}

// reorderArgs allows flags to appear after positional directory arguments,
// which is not supported by the standard flag package's default parsing.
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	terminated := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			terminated = true
			break
		}
		if len(arg) > 1 && strings.HasPrefix(arg, "-") {
			name := strings.TrimLeft(arg, "-")
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				name = name[:eq]
			}
			f := fs.Lookup(name)
			flags = append(flags, arg)
			if f != nil && !isBoolFlag(f) && !strings.Contains(arg, "=") && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	if terminated {
		flags = append(flags, "--")
	}
	return append(flags, positionals...)
}

func isBoolFlag(f *flag.Flag) bool {
	type boolFlag interface {
		IsBoolFlag() bool
	}
	b, ok := f.Value.(boolFlag)
	return ok && b.IsBoolFlag()
}

// configFlagKeys maps CLI flags to config keys; only flags set explicitly
// override the config file and environment.
var configFlagKeys = map[string]string{
	"addr":       "server.addr",
	"state-dir":  "server.state_dir",
	"log-level":  "log.level",
	"log-format": "log.format",
}

func loadConfig(path string) (config.Config, string, error) {
	overrides := map[string]any{}
	flag.Visit(func(f *flag.Flag) {
		if key, ok := configFlagKeys[f.Name]; ok {
			overrides[key] = f.Value.String()
		}
	})
	if path == "" {
		path = strings.TrimSpace(os.Getenv("COMMENTLOOP_CONFIG"))
	}
	defaultFile := ""
	if userDir, err := os.UserConfigDir(); err == nil {
		defaultFile = filepath.Join(userDir, "commentloop", "config.yaml")
	}
	return config.Load(config.LoadOptions{File: path, DefaultFile: defaultFile, Overrides: overrides})
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "commentloop: "+format+"\n", args...)
	os.Exit(1)
}

func listenWithFallback(addr string) (net.Listener, string, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, "", err
		}
		return ln, ln.Addr().String(), nil
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port == 0 {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, "", err
		}
		return ln, ln.Addr().String(), nil
	}

	var lastErr error
	for i := 0; i < 30 && port+i <= 65535; i++ {
		candidate := net.JoinHostPort(host, strconv.Itoa(port+i))
		ln, err := net.Listen("tcp", candidate)
		if err == nil {
			return ln, ln.Addr().String(), nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("No available port")
	}
	return nil, "", lastErr
}

func displayAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// migrateLegacyState moves the old mdlive state directory and database to the
// Commentloop names. It is intentionally best-effort: a failed migration must
// not prevent the server from starting with a fresh state directory.
func migrateLegacyState() {
	base, err := os.UserConfigDir()
	if err != nil {
		return
	}
	oldDir := filepath.Join(base, "mdlive")
	newDir := filepath.Join(base, "commentloop")
	if _, err := os.Stat(oldDir); err != nil {
		return
	}
	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		if err := os.Rename(oldDir, newDir); err != nil {
			slog.Warn("failed to migrate legacy state directory", "err", err)
			return
		}
	} else {
		// A new directory already exists (for example after a fresh start).
		// Move known top-level entries without overwriting anything.
		moveLegacyEntries(oldDir, newDir)
	}
	renameLegacyFile(newDir, "mdlive.db", "commentloop.db")
	renameLegacyFile(newDir, "mdlive.db-wal", "commentloop.db-wal")
	renameLegacyFile(newDir, "mdlive.db-shm", "commentloop.db-shm")
	renameLegacyFile(newDir, "mdlive.log", "commentloop.log")
	removeLegacyDirIfEmpty(oldDir)
}

func moveLegacyEntries(oldDir, newDir string) {
	entries, err := os.ReadDir(oldDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		src := filepath.Join(oldDir, entry.Name())
		dst := filepath.Join(newDir, entry.Name())
		if _, err := os.Stat(dst); err == nil {
			if entry.IsDir() {
				moveLegacyEntries(src, dst)
			}
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			slog.Warn("failed to migrate legacy state", "entry", entry.Name(), "err", err)
		}
	}
}

func renameLegacyFile(dir, oldName, newName string) {
	oldPath := filepath.Join(dir, oldName)
	newPath := filepath.Join(dir, newName)
	if _, err := os.Stat(oldPath); err != nil {
		return
	}
	if _, err := os.Stat(newPath); err == nil {
		return
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		slog.Warn("failed to migrate legacy state file", "file", oldName, "err", err)
	}
}

func removeLegacyDirIfEmpty(dir string) {
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
}
