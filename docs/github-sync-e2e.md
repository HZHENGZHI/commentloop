# GitHub 同步手动 E2E

R1 的真实同步需要在本机用真实的 fine-grained PAT 验证；不要把 token 贴在公开 Issue/PR 里。

1. 在 GitHub 创建 fine-grained PAT，只授权目标仓库的 **Issues: Read and write**。
2. 打开 Commentloop 的任意文档 → 评论面板 → GitHub 图标。
3. 填写 `owner/repo` 和 Issue/PR 编号，粘贴 PAT，点「保存配置」。
4. 点「测试连接」，确认返回 Issue/PR 标题。
5. 点「同步当前评论」，确认 GitHub 目标 Issue/PR 出现 Commentloop 评论；打开返回链接。
6. 回到弹窗点「清除 token」，确认配置里的 token 状态变为未设置。
7. 记录 PR/Issue 链接和截图到工单 #58；不要记录 token。

注意：token 通过 macOS `security` CLI 写入 Keychain；该 CLI 会把密码作为进程参数短暂暴露。已记录为 R1 后续安全改进。
