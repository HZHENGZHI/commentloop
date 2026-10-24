// Command debugrun is a small backend hot-reload supervisor used by
// `make debug`.
//
// It builds the mdlive server from the current sources, starts it, watches
// .go files (plus go.mod/go.sum), and after a successful rebuild replaces the
// old process. If a build fails, the previous server keeps running so the
// preview stays usable while fixing compile errors.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

const debounce = 250 * time.Millisecond

type supervisor struct {
	root     string
	args     []string
	goCmd    string
	tmpDir   string
	watcher  *fsnotify.Watcher
	child    *exec.Cmd
	childOut chan error
	bin      string
	buildSeq int
}

func main() {
	root := flag.String("root", ".", "project root to build and watch")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "用法: debugrun [选项] -- <mdlive 参数...>\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "构建 mdlive，监听 .go 变更并在编译成功后热重启后端。\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	log.SetPrefix("[debug] ")
	log.SetFlags(log.Ltime)

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		log.Fatalf("无效 root: %v", err)
	}
	goCmd := os.Getenv("GO")
	if goCmd == "" {
		goCmd = "go"
	}
	tmpDir, err := os.MkdirTemp("", "mdlive-debug-*")
	if err != nil {
		log.Fatalf("创建临时目录失败: %v", err)
	}

	s := &supervisor{
		root:   absRoot,
		args:   flag.Args(),
		goCmd:  goCmd,
		tmpDir: tmpDir,
	}
	defer func() {
		s.stopChild()
		if s.watcher != nil {
			_ = s.watcher.Close()
		}
		_ = os.RemoveAll(tmpDir)
	}()

	log.Printf("项目根目录: %s", s.root)
	log.Printf("监听 .go / go.mod / go.sum，修改并编译成功后自动热重启后端")
	if err := s.buildAndRun(); err != nil {
		log.Fatalf("初始构建 / 启动失败:\n%v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	if err := s.watch(sigCh); err != nil {
		log.Printf("监听失败: %v", err)
	}
}

func (s *supervisor) buildAndRun() error {
	bin, err := s.build()
	if err != nil {
		return err
	}
	if err := s.startChild(bin); err != nil {
		_ = os.Remove(bin)
		return err
	}
	s.bin = bin
	return nil
}

func (s *supervisor) build() (string, error) {
	s.buildSeq++
	out := filepath.Join(s.tmpDir, fmt.Sprintf("mdlive-%d", s.buildSeq))

	log.Printf("编译中…")
	cmd := exec.Command(s.goCmd, "build", "-o", out, ".")
	cmd.Dir = s.root
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(buf.String())
		if msg == "" {
			return "", fmt.Errorf("%w", err)
		}
		return "", fmt.Errorf("%w\n%s", err, msg)
	}
	return out, nil
}

func (s *supervisor) startChild(bin string) error {
	cmd := exec.Command(bin, s.args...)
	cmd.Dir = s.root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	s.child = cmd
	s.childOut = done
	go func() {
		done <- cmd.Wait()
	}()
	log.Printf("后端已启动 (pid %d)", cmd.Process.Pid)
	return nil
}

func (s *supervisor) stopChild() {
	if s.child == nil {
		return
	}
	child := s.child
	done := s.childOut
	s.child = nil
	s.childOut = nil

	_ = child.Process.Signal(os.Interrupt)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = child.Process.Kill()
		<-done
	}
	if s.bin != "" {
		_ = os.Remove(s.bin)
		s.bin = ""
	}
}

func (s *supervisor) restart() {
	log.Printf("检测到 Go 源码变化，重新编译…")
	bin, err := s.build()
	if err != nil {
		log.Printf("编译失败，旧后端继续运行:\n%v", err)
		return
	}
	s.stopChild()
	if err := s.startChild(bin); err != nil {
		log.Printf("新后端启动失败: %v", err)
		_ = os.Remove(bin)
		return
	}
	s.bin = bin
	log.Printf("后端热重启完成")
}

func (s *supervisor) watch(sigCh <-chan os.Signal) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	s.watcher = w
	if err := s.addDirs(s.root); err != nil {
		return err
	}

	var timer *time.Timer
	var timerC <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-sigCh:
			log.Printf("正在退出…")
			return nil

		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					_ = s.addDirs(ev.Name)
				}
			}
			if !s.relevant(ev.Name) {
				continue
			}
			if timer == nil {
				timer = time.NewTimer(debounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(debounce)
			}
			timerC = timer.C

		case <-timerC:
			timerC = nil
			s.restart()

		case err := <-s.childOut:
			if err != nil {
				log.Printf("后端进程退出: %v", err)
			} else {
				log.Printf("后端进程退出")
			}
			if s.bin != "" {
				_ = os.Remove(s.bin)
				s.bin = ""
			}
			s.child = nil
			s.childOut = nil

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			log.Printf("监听错误: %v", err)
		}
	}
}

func (s *supervisor) addDirs(root string) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if p != root {
			rel, _ := filepath.Rel(root, p)
			if filepath.ToSlash(rel) == "cmd/debugrun" {
				return filepath.SkipDir
			}
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
		}
		if e := s.watcher.Add(p); e != nil {
			log.Printf("监听 %s 失败: %v", p, e)
		}
		return nil
	})
}

func (s *supervisor) relevant(name string) bool {
	base := filepath.Base(name)
	if base == "go.mod" || base == "go.sum" {
		return true
	}
	if strings.HasSuffix(base, "_test.go") {
		return false
	}
	return strings.HasSuffix(base, ".go")
}

func skipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "web", "bin", "node_modules", "dist", "build", "out", "vendor":
		return true
	}
	return false
}
