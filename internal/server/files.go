package server

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (s *Server) readRootFile(ro *root, rel string, max int64) ([]byte, time.Time, error) {
	if ro.Kind == rootKindRemote {
		if ro.remoteFS == nil {
			return nil, time.Time{}, fmt.Errorf("远程目录未连接")
		}
		data, info, err := ro.remoteFS.read(rel, max)
		if err != nil {
			return nil, time.Time{}, err
		}
		return data, info.ModTime(), nil
	}
	abs, err := localFile(ro, rel)
	if err != nil {
		return nil, time.Time{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, time.Time{}, err
	}
	if info.IsDir() {
		return nil, time.Time{}, fmt.Errorf("路径是目录")
	}
	if max > 0 && info.Size() > max {
		return nil, time.Time{}, fmt.Errorf("文件过大")
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, time.Time{}, err
	}
	return data, info.ModTime(), nil
}

func (s *Server) statRootFile(ro *root, rel string) (os.FileInfo, error) {
	if ro.Kind == rootKindRemote {
		if ro.remoteFS == nil {
			return nil, fmt.Errorf("远程目录未连接")
		}
		return ro.remoteFS.stat(rel)
	}
	abs, err := localFile(ro, rel)
	if err != nil {
		return nil, err
	}
	return os.Stat(abs)
}

func (s *Server) writeRootFile(ro *root, rel string, data []byte) error {
	if ro.Kind == rootKindRemote {
		if ro.remoteFS == nil {
			return fmt.Errorf("远程目录未连接")
		}
		return ro.remoteFS.write(rel, data)
	}
	abs, err := localFile(ro, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(abs, data)
}

func (s *Server) removeRootFile(ro *root, rel string) error {
	if ro.Kind == rootKindRemote {
		if ro.remoteFS == nil {
			return fmt.Errorf("远程目录未连接")
		}
		return ro.remoteFS.remove(rel)
	}
	abs, err := localFile(ro, rel)
	if err != nil {
		return err
	}
	return os.Remove(abs)
}
