package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var githubRepoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func runCommand(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

type githubTokenStore interface {
	Get() (string, error)
	Set(string) error
	Clear() error
}

type keychainTokenStore struct {
	service string
	account string
}

func (k keychainTokenStore) Get() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", errors.New("系统钥匙串目前仅支持 macOS")
	}
	out, err := runCommand("security", "find-generic-password", "-s", k.service, "-a", k.account, "-w")
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

func (k keychainTokenStore) Set(token string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("系统钥匙串目前仅支持 macOS")
	}
	// The macOS security CLI only accepts the password as an argument. It is
	// visible in the process list briefly; the token is never written to the
	// database or logged. A Security.framework implementation would remove this
	// exposure and is tracked as R1 follow-up.
	_, err := runCommand("security", "add-generic-password", "-U", "-s", k.service, "-a", k.account, "-w", token)
	return err
}

func (k keychainTokenStore) Clear() error {
	if runtime.GOOS != "darwin" {
		return errors.New("系统钥匙串目前仅支持 macOS")
	}
	_, err := runCommand("security", "delete-generic-password", "-s", k.service, "-a", k.account)
	return err
}

type githubSyncRow struct {
	ID        string `gorm:"type:text;primaryKey"`
	Repo      string `gorm:"type:text;not null;default:''"`
	Issue     int    `gorm:"type:integer;not null;default:0"`
	UpdatedAt string `gorm:"type:text;not null"`
}

func (githubSyncRow) TableName() string { return "github_sync" }

type githubSyncConfig struct {
	Repo       string `json:"repo"`
	Issue      int    `json:"issue"`
	Configured bool   `json:"configured"`
	TokenSet   bool   `json:"tokenSet"`
}

func (s *Server) loadGitHubSync() (githubSyncRow, error) {
	var row githubSyncRow
	err := s.db.Take(&row, "id = ?", "default").Error
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "record not found") {
			return githubSyncRow{ID: "default"}, nil
		}
		return githubSyncRow{}, err
	}
	return row, nil
}

func (s *Server) githubConfig() (githubSyncConfig, error) {
	row, err := s.loadGitHubSync()
	if err != nil {
		return githubSyncConfig{}, err
	}
	token, _ := s.githubTokens.Get()
	return githubSyncConfig{
		Repo: row.Repo, Issue: row.Issue,
		Configured: row.Repo != "" && row.Issue > 0,
		TokenSet:   token != "",
	}, nil
}

func (s *Server) handleGetGitHubConfig(w http.ResponseWriter, r *http.Request) {
	config, err := s.githubConfig()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, config)
}

type putGitHubConfigRequest struct {
	Repo       string `json:"repo"`
	Issue      int    `json:"issue"`
	Token      string `json:"token"`
	ClearToken bool   `json:"clearToken"`
}

func (s *Server) handlePutGitHubConfig(w http.ResponseWriter, r *http.Request) {
	var req putGitHubConfigRequest
	if !bindJSON(w, r, 64*1024, &req) {
		return
	}
	req.Repo = strings.TrimSpace(req.Repo)
	if !githubRepoPattern.MatchString(req.Repo) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "GitHub 仓库格式应为 owner/repo"})
		return
	}
	if req.Issue <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Issue/PR 编号必须大于 0"})
		return
	}
	row := githubSyncRow{ID: "default", Repo: req.Repo, Issue: req.Issue, UpdatedAt: formatTime(time.Now())}
	if err := s.db.Save(&row).Error; err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if req.ClearToken {
		if err := s.githubTokens.Clear(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	} else if strings.TrimSpace(req.Token) != "" {
		if err := s.githubTokens.Set(strings.TrimSpace(req.Token)); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	config, err := s.githubConfig()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) githubToken() (string, error) {
	token, err := s.githubTokens.Get()
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", errors.New("请先在 GitHub 同步设置中保存 token")
	}
	return token, nil
}

func (s *Server) githubRequest(method, path string, payload any, out any) error {
	row, err := s.loadGitHubSync()
	if err != nil {
		return err
	}
	if row.Repo == "" || row.Issue <= 0 {
		return errors.New("请先配置 GitHub 仓库和 Issue/PR 编号")
	}
	token, err := s.githubToken()
	if err != nil {
		return err
	}
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	base := strings.TrimRight(s.githubAPIBase, "/")
	req, err := http.NewRequest(method, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "commentloop")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &apiErr)
		message := strings.TrimSpace(apiErr.Message)
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("GitHub API %d: %s", resp.StatusCode, message)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (s *Server) handleTestGitHubSync(w http.ResponseWriter, r *http.Request) {
	row, err := s.loadGitHubSync()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var issue struct {
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
		State   string `json:"state"`
	}
	if err := s.githubRequest(http.MethodGet, fmt.Sprintf("/repos/%s/issues/%d", row.Repo, row.Issue), nil, &issue); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"title": issue.Title, "url": issue.HTMLURL, "state": issue.State})
}

type githubCommentRequest struct {
	Body string `json:"body"`
}

func (s *Server) handleGitHubComment(w http.ResponseWriter, r *http.Request) {
	var req githubCommentRequest
	if !bindJSON(w, r, 128*1024, &req) {
		return
	}
	if strings.TrimSpace(req.Body) == "" || len(req.Body) > 60000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "评论内容不能为空或过长"})
		return
	}
	row, err := s.loadGitHubSync()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var result struct {
		HTMLURL string `json:"html_url"`
		ID      int64  `json:"id"`
	}
	path := fmt.Sprintf("/repos/%s/issues/%d/comments", row.Repo, row.Issue)
	if err := s.githubRequest(http.MethodPost, path, map[string]string{"body": req.Body}, &result); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": result.HTMLURL, "id": result.ID})
}
