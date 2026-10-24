package server

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"mdlive/internal/lsp"
)

const promptOptimizeTimeout = 2 * time.Minute

func (s *Server) optimizePromptTemplate(ctx context.Context, name, description, body string, allowEdit bool) (string, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	body = strings.TrimSpace(body)
	if body == "" && description == "" {
		return "", fmt.Errorf("请先写一个大概的提示词")
	}
	settings := s.settings.get()
	agent := settings.Agent
	if !strings.EqualFold(agent.Type, "pi") || strings.TrimSpace(agent.Command) == "" {
		return "", fmt.Errorf("AI 优化当前需要可用的 pi Agent，请先在设置中配置")
	}
	command, err := lsp.FindCommand(agent.Command)
	if err != nil {
		return "", err
	}
	args := stripSessionArgs(removeModeArg(append([]string{}, agent.Args...)))
	mode := "可以修改文档"
	if !allowEdit {
		mode = "只分析，不能修改文档"
	}
	prompt := fmt.Sprintf(`你是提示词工程专家。请优化下面这个 mdlive 后台 Code Agent 的任务提示词。

这个 Agent 通过 mdlive MCP 读取文档、评论、讨论共识和代码仓库，并按用户评论工作。
任务名称：%s
任务说明：%s
执行模式：%s

当前提示词（可能只是粗略草稿）：
---BEGIN---
%s
---END---

优化要求：
1. 保留原意和任务边界，不要擅自扩大任务范围。
2. 保留所有 {{变量}} 原样，不要发明新变量。可用变量：doc_path, comment_quote, comment_body, comment_thread, group_comment_ids, consensus, discussion_excerpts, code_repos。
3. 让指令具体、可执行、有明确输出格式；覆盖上下文使用、约束、失败/不确定时的处理。
4. 如果模式是“只分析”，明确要求不要修改文档，只通过回复给分析和方案。
5. 不要写 MCP 工具安全规则、不要写 resolve 规则，这些由系统额外追加。
6. 不要要求 Agent 交互式提问，不要输出解释、前言或代码围栏。
7. 使用中文 Markdown，控制在 2000 字以内。

只输出优化后的提示词正文。`, name, description, mode, truncatePromptText(body, 8000))
	runCtx, cancel := context.WithTimeout(ctx, promptOptimizeTimeout)
	defer cancel()
	args = append(args,
		"--print", "--no-tools", "--no-session", "--no-context-files",
		"--no-extensions", "--no-mcp", "--", prompt,
	)
	cmd := exec.CommandContext(runCtx, command, args...)
	if s.primaryPath != "" {
		cmd.Dir = s.primaryPath
	}
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if runCtx.Err() != nil {
			return "", fmt.Errorf("AI 优化超时")
		}
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 1200 {
			detail = detail[len(detail)-1200:]
		}
		if detail != "" {
			return "", fmt.Errorf("AI 优化失败：%s", detail)
		}
		return "", fmt.Errorf("AI 优化失败：%w", err)
	}
	optimized := stripCodeFence(stdout.String())
	if strings.TrimSpace(optimized) == "" {
		return "", fmt.Errorf("AI 没有返回优化结果")
	}
	if len(optimized) > maxConsensusSize {
		optimized = optimized[:maxConsensusSize]
	}
	return optimized, nil
}
