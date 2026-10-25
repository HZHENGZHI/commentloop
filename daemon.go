package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hzhengzhi/commentloop/internal/discovery"
)

const daemonStartTimeout = 10 * time.Second

func hasImmediateFlag(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "-h", "--help", "-help", "-v", "--version", "-version":
			return true
		}
	}
	return false
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == "--"+name || arg == "-"+name ||
			strings.HasPrefix(arg, "--"+name+"=") || strings.HasPrefix(arg, "-"+name+"=") {
			return true
		}
	}
	return false
}

func startDaemon(args []string) error {
	if info, err := discovery.Read(); err == nil {
		return fmt.Errorf("commentloop 已在运行 (pid %d, %s)；先执行 commentloop stop", info.PID, info.URL)
	}

	logPath, err := discovery.LogPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	childArgs := append([]string{}, args...)
	if !hasFlag(childArgs, "no-open") {
		childArgs = append(childArgs, "--no-open")
	}
	cmd := exec.Command(exe, childArgs...)
	cmd.Env = append(os.Environ(), "COMMENTLOOP_DAEMON_CHILD=1")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()

	deadline := time.Now().Add(daemonStartTimeout)
	for time.Now().Before(deadline) {
		info, findErr := discovery.Find(pid)
		if findErr == nil && info.URL != "" {
			if healthOK(info.URL) {
				fmt.Printf("commentloop 已启动\n")
				fmt.Printf("  PID    %d\n", info.PID)
				fmt.Printf("  地址    %s\n", info.URL)
				fmt.Printf("  版本    %s\n", info.Version)
				fmt.Printf("  日志    %s\n", logPath)
				fmt.Printf("  停止    commentloop stop\n")
				fmt.Printf("  状态    commentloop status\n")
				if !hasFlag(args, "no-open") {
					openBrowser(info.URL)
				}
				return nil
			}
		}
		time.Sleep(120 * time.Millisecond)
	}
	return fmt.Errorf("启动超时，请查看日志 %s", logPath)
}

func stopDaemon() error {
	info, err := discovery.Read()
	if err != nil {
		return fmt.Errorf("没有运行中的 commentloop 实例")
	}
	process, err := os.FindProcess(info.PID)
	if err != nil {
		discovery.RemovePID(info.PID)
		return fmt.Errorf("找不到进程 %d", info.PID)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !isNoSuchProcess(err) {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(info.PID) {
			discovery.RemovePID(info.PID)
			fmt.Printf("commentloop 已停止 (pid %d)\n", info.PID)
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	// A background consensus/agent task may be ignoring graceful shutdown.
	_ = process.Signal(syscall.SIGKILL)
	for i := 0; i < 20 && processAlive(info.PID); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	discovery.RemovePID(info.PID)
	if processAlive(info.PID) {
		return fmt.Errorf("无法停止进程 %d", info.PID)
	}
	fmt.Printf("commentloop 已强制停止 (pid %d)\n", info.PID)
	return nil
}

func statusDaemon() {
	info, err := discovery.Read()
	if err != nil {
		fmt.Println("commentloop 未运行")
		return
	}
	uptime := time.Since(info.StartedAt)
	if uptime < 0 {
		uptime = 0
	}
	fmt.Printf("commentloop 运行中\n")
	fmt.Printf("  PID    %d\n", info.PID)
	fmt.Printf("  地址    %s\n", info.URL)
	fmt.Printf("  版本    %s\n", info.Version)
	fmt.Printf("  启动    %s\n", info.StartedAt.Local().Format("2006-01-02 15:04:05"))
	fmt.Printf("  运行    %s\n", formatDuration(uptime))
	if logPath, err := discovery.LogPath(); err == nil {
		fmt.Printf("  日志    %s\n", logPath)
	}

	client := &http.Client{Timeout: 1200 * time.Millisecond}
	resp, err := client.Get(strings.TrimRight(info.URL, "/") + "/api/config")
	if err != nil {
		fmt.Println("  健康    无法连接")
		return
	}
	defer resp.Body.Close()
	var config struct {
		Root        string `json:"root"`
		RootCount   int    `json:"rootCount"`
		PreviewOnly bool   `json:"previewOnly"`
		Debug       bool   `json:"debug"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		fmt.Println("  健康    响应无法解析")
		return
	}
	mode := "评审模式"
	if !config.PreviewOnly {
		mode = "编辑 + 预览"
	}
	if config.Debug {
		mode += " · debug"
	}
	fmt.Printf("  健康    正常 (%s)\n", mode)
	fmt.Printf("  主目录  %s\n", config.Root)
	fmt.Printf("  目录数  %d\n", config.RootCount)
}

func healthOK(rawURL string) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(strings.TrimRight(rawURL, "/") + "/api/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || isPermission(err)
}

func isNoSuchProcess(err error) bool {
	return errors.Is(err, syscall.ESRCH)
}

func isPermission(err error) bool {
	return errors.Is(err, syscall.EPERM)
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}
