package coderepo

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

func localGitRun(ctx context.Context, path string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", path}, args...)...)
	// Keep the caller's environment (PATH, HOME, global git config); appending
	// to a nil Env would run git with only the prompt setting.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	text := strings.TrimSpace(output.String())
	if err != nil {
		if text != "" {
			return text, fmt.Errorf("%s", text)
		}
		return text, err
	}
	return text, nil
}

// LocalBranches lists local branches in a working tree.
func LocalBranches(ctx context.Context, path string) ([]string, error) {
	top, err := localGitRun(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("不是 Git 工作区：%w", err)
	}
	top = strings.TrimSpace(top)
	if top == "" {
		return nil, fmt.Errorf("不是 Git 工作区")
	}
	out, err := localGitRun(ctx, path, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	branches := make([]string, 0)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && line != "HEAD" {
			branches = append(branches, line)
		}
	}
	sort.Strings(branches)
	return branches, nil
}

// SwitchLocalBranch changes a clean local working tree to an existing branch.
func SwitchLocalBranch(ctx context.Context, path, branch string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return fmt.Errorf("分支名不能为空")
	}
	check := exec.CommandContext(ctx, "git", "check-ref-format", "--branch", branch)
	if err := check.Run(); err != nil {
		return fmt.Errorf("分支名无效")
	}
	top, err := localGitRun(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("不是 Git 工作区：%w", err)
	}
	top = strings.TrimSpace(top)
	if top == "" {
		return fmt.Errorf("不是 Git 工作区")
	}
	status, err := localGitRun(ctx, path, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("本地仓库有未提交改动，请先提交或 stash 后再切换分支")
	}
	if _, err := localGitRun(ctx, path, "rev-parse", "--verify", "refs/heads/"+branch); err != nil {
		return fmt.Errorf("本地分支不存在：%s", branch)
	}
	if _, err := localGitRun(ctx, path, "checkout", "--quiet", branch); err != nil {
		return fmt.Errorf("切换本地分支失败：%w", err)
	}
	return nil
}
