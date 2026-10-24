package coderepo

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type GitHubSource struct {
	Owner    string
	Repo     string
	Name     string
	CloneURL string
	WebURL   string
	SSH      bool
}

var gitHubNameRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ParseGitHub accepts https://github.com/owner/repo, git@github.com:owner/repo,
// ssh://git@github.com/owner/repo and owner/repo shorthand.
func ParseGitHub(raw string) (GitHubSource, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return GitHubSource{}, fmt.Errorf("GitHub 仓库地址不能为空")
	}
	ssh := false
	pathPart := ""
	lower := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(lower, "git@github.com:"):
		ssh = true
		pathPart = raw[len("git@github.com:"):]
	case strings.HasPrefix(lower, "ssh://git@github.com/"):
		ssh = true
		pathPart = raw[len("ssh://git@github.com/"):]
	case strings.HasPrefix(lower, "https://github.com/"):
		pathPart = raw[len("https://github.com/"):]
	case strings.HasPrefix(lower, "http://github.com/"):
		pathPart = raw[len("http://github.com/"):]
	case strings.HasPrefix(lower, "github.com/"):
		pathPart = raw[len("github.com/"):]
	default:
		// owner/repo shorthand
		if strings.Count(raw, "/") == 1 && !strings.Contains(raw, ":") {
			pathPart = raw
		} else {
			return GitHubSource{}, fmt.Errorf("只支持 github.com 仓库地址")
		}
	}
	pathPart = strings.TrimSuffix(strings.Trim(pathPart, "/"), ".git")
	pathPart = strings.SplitN(pathPart, "?", 2)[0]
	pathPart = strings.SplitN(pathPart, "#", 2)[0]
	segments := strings.Split(pathPart, "/")
	if len(segments) != 2 || !gitHubNameRE.MatchString(segments[0]) || !gitHubNameRE.MatchString(segments[1]) {
		return GitHubSource{}, fmt.Errorf("GitHub 地址格式应为 github.com/owner/repo")
	}
	owner, repo := segments[0], segments[1]
	source := GitHubSource{
		Owner: owner, Repo: repo, Name: repo,
		CloneURL: "https://github.com/" + owner + "/" + repo + ".git",
		WebURL:   "https://github.com/" + owner + "/" + repo,
		SSH:      ssh,
	}
	if ssh {
		source.CloneURL = "git@github.com:" + owner + "/" + repo + ".git"
	}
	return source, nil
}

func CloneGitHub(ctx context.Context, source GitHubSource, branch, dest, token string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	_ = os.RemoveAll(dest)
	args := []string{"clone", "--depth", "1"}
	if strings.TrimSpace(branch) != "" {
		args = append(args, "--branch", strings.TrimSpace(branch))
	}
	args = append(args, source.CloneURL, dest)
	cmd := exec.CommandContext(ctx, "git", args...)
	env := os.Environ()
	env = append(env, "GIT_TERMINAL_PROMPT=0")
	if token != "" && !source.SSH {
		auth := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		env = append(env,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+auth,
		)
	}
	cmd.Env = env
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(dest)
		detail := strings.TrimSpace(output.String())
		if len(detail) > 1500 {
			detail = detail[len(detail)-1500:]
		}
		if detail != "" {
			return fmt.Errorf("克隆 GitHub 仓库失败：%s", detail)
		}
		return fmt.Errorf("克隆 GitHub 仓库失败：%w", err)
	}
	// Ensure the clone is not writable by accidental code tools? The repository
	// is only read through coderepo; leave git's normal permissions intact.
	return nil
}

// GitHubCloneTimeout is the upper bound for a shallow clone from the API.
const GitHubCloneTimeout = 3 * time.Minute

func GitHubEnv(source GitHubSource, token string) []string {
	env := os.Environ()
	env = append(env, "GIT_TERMINAL_PROMPT=0")
	if token != "" && !source.SSH {
		auth := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		env = append(env,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+auth,
		)
	}
	return env
}

func UpdateGitHubBranch(ctx context.Context, source GitHubSource, dest, branch, token string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return fmt.Errorf("分支名不能为空")
	}
	check := exec.CommandContext(ctx, "git", "check-ref-format", "--branch", branch)
	if err := check.Run(); err != nil {
		return fmt.Errorf("分支名无效")
	}
	run := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dest}, args...)...)
		cmd.Env = GitHubEnv(source, token)
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		if err := cmd.Run(); err != nil {
			detail := strings.TrimSpace(output.String())
			if len(detail) > 1200 {
				detail = detail[len(detail)-1200:]
			}
			if detail != "" {
				return fmt.Errorf("%s", detail)
			}
			return err
		}
		return nil
	}
	if err := run("fetch", "--depth", "1", "origin", branch); err != nil {
		return fmt.Errorf("拉取分支失败：%w", err)
	}
	if err := run("checkout", "-B", branch, "FETCH_HEAD"); err != nil {
		return fmt.Errorf("切换分支失败：%w", err)
	}
	return nil
}

func ListGitHubBranches(ctx context.Context, source GitHubSource, dest, token string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", dest, "ls-remote", "--heads", "origin")
	cmd.Env = GitHubEnv(source, token)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	branches := make([]string, 0)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !strings.HasPrefix(fields[1], "refs/heads/") {
			continue
		}
		branches = append(branches, strings.TrimPrefix(fields[1], "refs/heads/"))
	}
	sort.Strings(branches)
	return branches, nil
}
