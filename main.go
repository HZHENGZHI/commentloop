// mdlive is a local review workspace with a live, server-rendered preview.
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mdlive/internal/agentcli"
	"mdlive/internal/agentmcp"
	"mdlive/internal/agentrunner"
	"mdlive/internal/discovery"
	"mdlive/internal/ignore"
	"mdlive/internal/server"
)

//go:embed all:web/dist
var webFiles embed.FS

var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "agent":
			if err := agentcli.Run(args[1:]); err != nil {
				fmt.Fprintln(os.Stderr, "mdlive agent:", err)
				os.Exit(1)
			}
			return
		case "mcp":
			if err := agentmcp.Run(args[1:], version); err != nil {
				fmt.Fprintln(os.Stderr, "mdlive mcp:", err)
				os.Exit(1)
			}
			return
		case "agent-runner":
			if err := agentrunner.Run(args[1:]); err != nil {
				fmt.Fprintln(os.Stderr, "mdlive agent-runner:", err)
				os.Exit(1)
			}
			return
		case "start":
			if hasImmediateFlag(args[1:]) {
				runServer(args[1:])
				return
			}
			if err := startDaemon(args[1:]); err != nil {
				fmt.Fprintln(os.Stderr, "mdlive start:", err)
				os.Exit(1)
			}
			return
		case "stop":
			if err := stopDaemon(); err != nil {
				fmt.Fprintln(os.Stderr, "mdlive stop:", err)
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
	if os.Getenv("MDLIVE_DAEMON_CHILD") == "1" || hasImmediateFlag(args) {
		runServer(args)
		return
	}

	// Default invocation starts the server in the background. Use
	// `mdlive run ...` for a foreground process.
	if err := startDaemon(args); err != nil {
		fmt.Fprintln(os.Stderr, "mdlive start:", err)
		os.Exit(1)
	}
}

func runServer(args []string) {
	var extraRoots []string
	flag.Func("add-dir", "追加一个目录到工作区（可重复）", func(value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("目录不能为空")
		}
		extraRoots = append(extraRoots, value)
		return nil
	})

	var (
		dirFlag   = flag.String("dir", "", "要打开的目录（默认：当前目录；也可传文件路径）")
		addrFlag  = flag.String("addr", "127.0.0.1:4571", "监听地址")
		noOpen    = flag.Bool("no-open", false, "不自动打开浏览器")
		devMode   = flag.Bool("dev", false, "开发模式：前端未构建时也启动（配合 Vite）")
		debugMode = flag.Bool("debug", false, "调试模式：审阅模式（前台只读、Agent 可写）+ 前端资源热更新")
		preview   = flag.Bool("preview", false, "兼容保留：mdlive 默认就是审阅模式；前台只读，Agent 可通过 MCP/CLI 写入")
		webDir    = flag.String("web-dir", "web/dist", "调试模式从磁盘加载的前端目录")
		noState   = flag.Bool("no-state", false, "不读取 / 保存目录列表")
		stateDir  = flag.String("state-dir", "", "目录列表的保存位置（默认：用户配置目录/mdlive）")
		showVer   = flag.Bool("version", false, "打印版本号后退出")
	)
	flag.BoolVar(showVer, "v", false, "打印版本号后退出")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "mdlive %s — Markdown 实时预览\n\n", version)
		fmt.Fprintf(flag.CommandLine.Output(), "用法: mdlive [start|run|stop|status] [选项] [目录或 Markdown 文件]\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "子命令:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  start              后台启动（默认）\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  run                前台运行（调试 / 由进程管理器托管）\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  stop               停止最近启动的实例\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  status             查看运行状态\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  agent-runner       宿主机 Agent Runner，领取外部 Agent 任务\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  agent ...          给代码 Agent 的 CLI（list/read/write/search/changes）\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  agent-runner       宿主机 Agent Runner，领取评论任务\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  mcp                MCP stdio server（给支持 MCP 的 Agent）\n\n")
		flag.PrintDefaults()
	}
	flag.CommandLine.Parse(reorderArgs(flag.CommandLine, args))

	if *showVer {
		fmt.Println(version)
		return
	}

	rootArg := *dirFlag
	if rootArg == "" && flag.NArg() > 0 {
		rootArg = flag.Arg(0)
	}
	if rootArg == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fatal("无法获取当前目录: %v", err)
		}
		rootArg = cwd
	}

	abs, err := filepath.Abs(rootArg)
	if err != nil {
		fatal("无效路径 %q: %v", rootArg, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		fatal("无法访问 %q: %v", abs, err)
	}
	root := abs
	initial := ""
	if !info.IsDir() {
		root = filepath.Dir(abs)
		initial = filepath.Base(abs)
		if !ignore.IsMarkdown(initial) {
			fatal("不是 Markdown 文件: %s", abs)
		}
	}

	dist, err := fs.Sub(webFiles, "web/dist")
	if err != nil {
		fatal("前端资源不可用: %v", err)
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		if !*devMode && !*debugMode {
			fatal("前端尚未构建：请运行 make build，或在开发时使用 make dev-backend + make dev-web")
		}
		log.Println("mdlive: 内嵌前端尚未构建，继续使用磁盘前端或 Vite 开发服务器")
	}

	ln, actualAddr, err := listenWithFallback(*addrFlag)
	if err != nil {
		fatal("无法监听 %s: %v", *addrFlag, err)
	}

	stateDirPath := ""
	if !*noState {
		dir := *stateDir
		if dir == "" {
			if userDir, err := os.UserConfigDir(); err == nil {
				dir = filepath.Join(userDir, "mdlive")
			}
		}
		if dir != "" {
			absState, err := filepath.Abs(dir)
			if err == nil {
				stateDirPath = absState
			}
		}
	}

	// mdlive is review-only: the browser is for reading, commenting and
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
	})
	if err != nil {
		fatal("启动失败: %v", err)
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
		log.Printf("mdlive: 写入 Agent discovery 文件失败: %v", discoveryErr)
	} else {
		defer discovery.RemoveOwner(discoveryPath, os.Getpid())
	}

	httpSrv := &http.Server{
		Handler:           app,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	fmt.Printf("mdlive %s\n", version)
	fmt.Printf("  主目录  %s\n", app.Root())
	fmt.Printf("  目录数  %d\n", app.RootCount())
	if *debugMode {
		fmt.Printf("  模式    调试 · 审阅模式 · 前端热更新\n")
	} else if readOnly {
		fmt.Printf("  模式    审阅模式（前台只读，Agent 可写）\n")
	} else {
		fmt.Printf("  模式    编辑 + 预览\n")
	}
	fmt.Printf("  地址    %s\n", browseURL)
	fmt.Printf("  停止    Ctrl+C\n")

	if !*noOpen {
		openBrowser(browseURL)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		log.Println("mdlive: 正在退出…")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
		_ = app.Close()
	}()

	if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
		fatal("服务异常退出: %v", err)
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

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "mdlive: "+format+"\n", args...)
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
		lastErr = fmt.Errorf("没有可用端口")
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

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "linux":
		cmd = exec.Command("xdg-open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("mdlive: 无法自动打开浏览器: %v", err)
		return
	}
	go func() { _ = cmd.Wait() }()
}
