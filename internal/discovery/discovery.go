// Package discovery stores the address of running local commentloop servers so CLI
// and MCP clients can find them.
package discovery

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"
)

type Info struct {
	URL       string    `json:"url"`
	PID       int       `json:"pid"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"startedAt"`
}

func dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "commentloop"), nil
}

func instancesDir() (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "instances"), nil
}

func Path() (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "server.json"), nil
}

// AgentTokenPath is the short-lived capability token file shared by the
// host agent-runner and the MCP process it launches.
func AgentTokenPath() (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "agent-token"), nil
}

// AgentURLPath is the short-lived server URL file shared by the host
// agent-runner and MCP processes that do not inherit its environment.
func AgentURLPath() (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "agent-url"), nil
}

// LogPath is where a background commentloop instance appends its stdout/stderr.
func LogPath() (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "commentloop.log"), nil
}

// Find returns the discovery record for a specific process.
func Find(pid int) (Info, error) {
	for _, info := range readInstanceFiles() {
		if info.PID == pid {
			return info, nil
		}
	}
	return Info{}, os.ErrNotExist
}

// RemovePID removes a process's discovery record.
func RemovePID(pid int) {
	_, _ = removeInstance(pid)
}

// Read returns the newest live instance. If the latest file is stale, it
// promotes the newest remaining live instance.
func Read() (Info, error) {
	path, err := Path()
	if err != nil {
		return Info{}, err
	}
	if data, err := os.ReadFile(path); err == nil {
		var info Info
		if json.Unmarshal(data, &info) == nil && info.URL != "" && processAlive(info.PID) {
			return info, nil
		}
	}
	infos := liveInstances(0)
	if len(infos) == 0 {
		return Info{}, os.ErrNotExist
	}
	info := infos[0]
	_ = writeLatest(info)
	return info, nil
}

// Write records this server as the latest instance and returns its
// per-instance discovery file. Call RemoveOwner for that file on shutdown.
func Write(url, version string) (string, error) {
	instances, err := instancesDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(instances, 0o700); err != nil {
		return "", err
	}
	info := Info{URL: url, PID: os.Getpid(), Version: version, StartedAt: time.Now()}

	// Prune discovery files left behind by crashed processes.
	for _, old := range readInstanceFiles() {
		if old.PID != info.PID && !processAlive(old.PID) {
			_, _ = removeInstance(old.PID)
		}
	}

	instancePath := filepath.Join(instances, instanceFileName(info.PID))
	if err := writeInfo(instancePath, info); err != nil {
		return "", err
	}
	if err := writeLatest(info); err != nil {
		return "", err
	}
	return instancePath, nil
}

// RemoveOwner removes this instance and, if this process was the latest,
// promotes the newest remaining live instance.
func RemoveOwner(instancePath string, pid int) {
	_, _ = removeInstance(pid)
	latestPath, err := Path()
	if err != nil {
		return
	}
	data, err := os.ReadFile(latestPath)
	if err != nil {
		return
	}
	var latest Info
	if json.Unmarshal(data, &latest) != nil || latest.PID != pid {
		return
	}
	infos := liveInstances(pid)
	if len(infos) == 0 {
		_ = os.Remove(latestPath)
		return
	}
	_ = writeLatest(infos[0])
}

func writeLatest(info Info) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeInfo(path, info)
}

func writeInfo(path string, info Info) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), "server-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func instanceFileName(pid int) string {
	return strconv.Itoa(pid) + ".json"
}

func readInstanceFiles() []Info {
	instances, err := instancesDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(instances)
	if err != nil {
		return nil
	}
	out := make([]Info, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(instances, entry.Name()))
		if err != nil {
			continue
		}
		var info Info
		if json.Unmarshal(data, &info) == nil && info.URL != "" && info.PID > 0 {
			out = append(out, info)
		}
	}
	return out
}

func liveInstances(excludePID int) []Info {
	infos := readInstanceFiles()
	live := make([]Info, 0, len(infos))
	for _, info := range infos {
		if info.PID == excludePID {
			continue
		}
		if processAlive(info.PID) {
			live = append(live, info)
		} else {
			_, _ = removeInstance(info.PID)
		}
	}
	sort.Slice(live, func(i, j int) bool { return live[i].StartedAt.After(live[j].StartedAt) })
	return live
}

func removeInstance(pid int) (string, error) {
	instances, err := instancesDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(instances, instanceFileName(pid))
	return path, os.Remove(path)
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
	return err == nil || errors.Is(err, syscall.EPERM)
}
