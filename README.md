# mdlive

本地优先的 Markdown 审阅工作区：**Go 后端 + React 前端**，编译后是单个可执行文件，macOS 与 Linux 通用。

mdlive 不提供浏览器内编辑。前台只负责阅读、评论、审阅和回滚；文档由后台 Agent 通过 MCP/CLI 写入。服务只监听本机回环地址，文件读写、Markdown 渲染、文件监听全部由 Go 进程完成。一个工作区可以同时添加多个分散在不同位置的目录，运行时也能随时新增 / 移除。前端静态资源通过 `go:embed` 打进二进制，所以分发时不需要 Node.js，也不需要拷贝 `web/dist`。

## 特性

- **实时预览**：Agent 或外部编辑器写入后，Go 使用 goldmark 渲染（GFM 表格、任务列表、删除线、脚注、自动标题锚点），带 Chroma 代码高亮，并通过 WebSocket 推送页面。
- **外部修改同步**：用 Obsidian、VS Code 等外部工具或 Agent 修改文件时，`fsnotify` 会监听变化并把新内容推送到页面。
- **多目录工作区**：可同时添加多个目录（笔记可能散落在不同磁盘 / 文件夹），侧栏按目录分组展示；支持运行时添加 / 移除，移除只影响工作区、不删除磁盘文件。
- **远程服务器（SSH）**：通过 SSH + SFTP 添加远程目录，像本地目录一样浏览、预览和评审远程 Markdown；写入由 Agent 完成；支持图片、相对链接和每 15 秒轮询外部变更。
- **Code Agent 集成**：内置 `mdlive agent` CLI 与 MCP stdio server，代码 Agent 可以发现、读取、写入本地和远程 Markdown，支持搜索和“最近修改”查询。
- **文档结合代码仓库**：每篇文档可以关联一个或多个本机目录或 GitHub 仓库（浅克隆到本地缓存）；Agent 通过 MCP 读取代码文件、搜索符号、查看 Git 分支 / 未提交改动，从而按真实代码情况写文档。代码仓库只读，不会被修改。
- **页面内代码浏览 + LSP 跳转**：在预览右上角 **代码** 中浏览仓库文件、查看只读源码；支持在代码中 `⌘/Ctrl + 点击` 符号或点 **跳转定义**，通过 gopls / TypeScript / Python / Rust 语言服务器跳转到定义位置。
- **设置页**：顶部齿轮打开设置，可配置各语言的 LSP 服务器命令 / 参数 / 启用状态，以及后台 Code Agent 类型、显示名称和启动命令。
- **评论自动唤醒 pi Agent**：开启后，用户新增评论或回复评论时，mdlive 会通过 pi 的 RPC 模式立即启动后台 Agent；Agent 通过 MCP 读取文档 / 代码、写回修改并回复评论，完成后等待用户确认。
- **每文档独立 Agent 会话**：每篇文档有自己的 pi 会话，可以新建多个会话；顶部文档标题下方可切换当前会话，切换后新的评论任务使用所选会话，历史上下文互相隔离。
- **讨论上下文与文档共识**：自动检测本机 Pi / Claude Code / Codex / Cursor 的会话，导入当前文档并提炼成结构化“文档共识”；之后评论修改自动参考共识，也可以在评论里手动勾选具体讨论片段；每轮 Agent 修改完成后会自动更新共识，减少与早期讨论的偏差。
- **评审模式（默认）**：不渲染编辑器，禁止编辑已有文档；保留文件树、新建 / 导入 / 删除文档、预览、评论、关联和局部回滚。文档内容由后台 Agent 通过 MCP / CLI 写入。`--preview` 作为兼容参数保留。
- **前后端热重载**：`make debug` 在 React / CSS 改动后自动刷新页面；Go 代码改动由仓库内置的 supervisor 重新编译并热重启后端，浏览器 WebSocket 自动重连。调试模式默认是审阅模式（前台只读，Agent 可写）。
- **文件树与搜索**：跨目录搜索、切换文档，保留新建 / 导入 / 删除文档入口；每个目录独立递归监听。
- **行内评论 / Review**：在预览中选中文字直接评论；评论锚定到 Markdown 位置，Code Agent 修改后可以回复修改前后内容，形成可追溯的审阅线程。可以把不同位置的评论标记为一组，Code Agent 会一起阅读并要求一起修改；如果后面的修改不如前面，还可以只回滚某条评论所在的段落。
- **SQLite 持久化**：工作区目录和评论统一存储在本地 SQLite（`mdlive.db`），支持自动迁移旧 `roots.json`。
- **安全保存**：原子写入（临时文件 + rename），保留原文件权限；每个文件只能在其所属目录内解析，拦截 `..` 和越界软链接。
- **本地运行**：默认绑定 `127.0.0.1`，校验 Host / Origin，避免局域网和跨站访问。
- **跨平台**：纯 Go（无 cgo）+ 标准浏览器；macOS 用 `open`、Linux 用 `xdg-open` 自动打开发页面。
- **单二进制**：前端构建产物嵌入 Go，`make cross` 可一次产出 macOS / Linux 的 amd64、arm64 版本。

## 快速开始

### 环境要求

- Go **1.26+**（构建后端）
- Node.js **22+**、npm（仅构建前端时需要）

### 构建与运行

```bash
cd ~/Desktop/coding/mdlive

# 首次构建会自动安装前端依赖、执行 vite build，再编译二进制
make build

# 打开一个笔记目录（会自动选择最近打开或最近修改的 Markdown 文件）
./bin/mdlive ~/Documents/notes

# 同时添加多个目录（可重复 --add-dir）
./bin/mdlive ~/Documents/notes --add-dir ~/obsidian-vault --add-dir /Volumes/work/wiki

# 也可以直接打开某个 Markdown 文件
./bin/mdlive ~/Documents/notes/日记.md

# 指定端口，或禁止自动打开浏览器
./bin/mdlive --addr 127.0.0.1:8080 --no-open ~/Documents/notes
```

不传目录时默认打开当前工作目录：

```bash
cd ~/Documents/notes && ~/Desktop/coding/mdlive/bin/mdlive
```

启动后终端会打印实际地址（端口被占用时会自动向后尝试 30 个端口），并自动打开浏览器。

### 安装到 PATH

```bash
cp bin/mdlive /usr/local/bin/mdlive   # 或者 ~/.local/bin
mdlive ~/notes
```

macOS 如果提示“无法验证开发者”，可对自行构建的二进制执行：

```bash
xattr -d com.apple.quarantine /usr/local/bin/mdlive
```

## 命令行参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `--dir <path>` | 当前目录 | 主目录 |
| `--add-dir <path>` | 可重复 | 追加一个目录到工作区 |
| `--state-dir <path>` | 用户配置目录/mdlive | 保存运行时添加目录的位置 |
| `--no-state` | `false` | 不读取 / 保存额外目录配置 |
| `--addr <host:port>` | `127.0.0.1:4571` | 监听地址；端口占用会自动递增重试 |
| `--no-open` | `false` | 不自动打开浏览器 |
| `--preview` | `false` | 兼容保留：mdlive 默认就是评审模式；前台不能编辑已有文档，但可新建 / 导入 / 删除，Agent MCP/CLI 可写 |
| `--debug` | `false` | 调试模式：审阅模式 + 前端资源热更新 |
| `--web-dir <path>` | `web/dist` | 调试模式下从磁盘加载的前端目录 |
| `--dev` | `false` | 开发模式：即使 `web/dist` 尚未构建也启动后端 |
| `--version` | | 打印版本号 |
| 位置参数 `<dir\|file>` | | 与 `--dir` 等价；传文件时以所在目录为根 |

## 多目录工作区

除启动参数外，可以在界面里直接管理目录：

- 侧栏顶部的 **文件夹 +** 按钮：打开目录选择弹窗
- 弹窗从当前主目录的上一级开始，可点击目录逐级浏览；支持“上一级”、主目录 / 桌面 / 文稿 / 下载 / 根目录 / 外接磁盘快捷入口
- 路径栏仍可直接输入或粘贴路径（支持 `~`），按 Enter 或“转到”跳转
- 可选显示隐藏目录；已添加的目录会标记并禁用添加
- 每个目录行右侧的 **×**：从工作区移除（不会删除磁盘文件）。点击后目录行变为“移除中”，侧栏底部显示 spinner 进度条，服务端立即从工作区移除目录，后台关闭 watcher / SSH 连接
- 目录行标记 **主**：启动时的主目录

合并规则：

1. 主目录（位置参数 / `--dir`，未指定时是当前目录）
2. 启动时的 `--add-dir` 目录
3. 上一次运行保存的完整目录列表

**当前工作区的所有目录都会持久化**，包括主目录、`--add-dir` 目录和界面添加的目录；重启时自动合并并去重。配置保存到：

- macOS：`~/Library/Application Support/mdlive/mdlive.db`
- Linux：`~/.config/mdlive/mdlive.db`（遵循 `os.UserConfigDir()`）

数据库使用纯 Go 的 `modernc.org/sqlite`，因此 `CGO_ENABLED=0` 交叉编译仍然可用。旧的 `roots.json` 会在首次启动时自动导入 SQLite，并保留为备份。评论、锚点、回复、修改前后内容、完整文档快照和代码仓库关联都存在同一个数据库中。

使用 `--no-state` 可以完全禁用磁盘持久化（内存数据库，仅当前进程）；`--state-dir` 可指定其他位置。评审模式下仍允许添加 / 移除目录、新建 / 导入 / 删除文档、评论、关联和局部回滚；已有的文档内容只能由 Agent 写入。

> 目录选择使用应用内文件夹浏览器（不是系统原生文件对话框），因此 macOS / Linux 行为一致；添加前会经过 `EvalSymlinks` 和目录校验。

## 远程服务器（SSH）

在“添加目录”弹窗中切换到 **远程服务器**：

| 字段 | 说明 |
|------|------|
| 名称 | 侧栏显示名，留空时使用远程目录名 |
| 主机 / 端口 / 用户名 | SSH 连接信息，端口默认 22 |
| 私钥路径 | 可选；留空时尝试 `~/.ssh/id_ed25519`、`id_ecdsa`、`id_rsa` 和 ssh-agent |
| 远程目录 | 可留空；填写时作为浏览起点，例如 `/home/me/notes` 或 `~/notes` |

点击 **浏览远程目录** 后会先显示连接动画：

- 连接期间显示转圈动画和目标服务器
- 可点击 **关闭** 取消连接并返回表单
- 连接失败时保留错误信息，可重新连接或关闭
- 连接成功后进入远程目录浏览器：逐级点击、上一级、主目录 / 根目录快捷入口、路径跳转、隐藏目录开关；选好后点击 **添加此目录**，无需手动输入完整路径。
- 点击 **添加此目录** 后会显示 **正在读取远程文件…** spinner，服务端先完成首次文件索引再返回，侧栏随即显示文件，而不是先出现一个空目录。
- 应用启动 / 重连时，尚未完成首次扫描的远程根目录会在侧栏显示 **读取中**；扫描完成后自动填充文件。

连接成功后，远程目录会作为一个带 **远程** 标记的根目录出现在侧栏，支持：

- 递归浏览远程 Markdown，打开 / 预览 / 评审；支持新建 / 导入 / 删除
- 已有内容由 Agent 通过 SFTP 写入；前台不提供编辑器 / 保存
- 远程图片经 `/raw/` 读取
- 外部修改每 15 秒轮询一次，变化时推送 tree / 文档更新
- 目录配置持久化，重启后自动重连

安全与认证：

- 使用 **SSH 私钥 / ssh-agent**，不保存密码；不支持密码登录和 ProxyJump。
- 主机指纹通过 `~/.ssh/known_hosts` 校验。首次连接前请先在终端执行一次 `ssh user@host`（非 22 端口加 `-p`）接受指纹。
- 暂不支持 `~/.ssh/config` 别名和跳板机（ProxyJump）；主机 / 用户 / 端口需显式填写。
- 远端目录选择器使用应用内浏览 UI，不调用系统原生文件夹对话框。
- 远程服务器启动时不可达会跳过该目录；网络恢复后重新添加或重启。
- 远程根目录过大（例如整个 `/root`）时首次扫描可能较慢；建议添加具体的笔记目录。
- 远程连接使用 Go 原生 SSH/SFTP 库，无需在远程安装 mdlive。

## 讨论上下文与文档共识

在预览右上角点击 **讨论**：

1. 点 **导入会话**。mdlive 会检查本机已安装 / 有会话数据的 Agent，并按 Agent 分栏显示，不再靠手动筛选标题或目录：
   - Pi：mdlive 状态目录和 `~/.pi/agent/sessions`
   - Claude Code：`~/.claude/projects`
   - Codex：`~/.codex/sessions` / `archived_sessions`
   - Cursor：`~/.cursor/projects/*/agent-transcripts`
   - 其他已安装但暂未实现读取的 Agent 会显示“暂未支持”
2. 导入后会调用已配置的 pi Agent，从会话中提炼结构化共识：目标、已确认决策、约束与术语、已否决方案、待确认事项、最近修改。
3. 共识会自动带入之后的评论修改 Prompt；不需要每次手动粘贴长对话。
4. 每轮 Agent 成功完成评论修改后，mdlive 会在后台重新提炼共识（同一文档串行合并），并实时刷新页面。
5. 已导入的会话文件会每 5 秒检查一次；原 Agent 继续对话、追加消息后，mdlive 会自动同步会话内容并刷新共识。
6. 新增评论或回复时，还可以展开 **参考讨论**，手动勾选特定导入会话；选中的会话会附带相关片段给 Agent。
7. 如果当前评论和旧共识冲突，Prompt 明确要求以最新用户反馈为准；无法判断时由 Agent 说明冲突。

共识会包含来源和版本号，保存在本机 SQLite；导入会话只保存对话文本（用户 / Agent 文本，跳过工具调用和系统提示），不会修改会话文件。共识处理中会显示“整理讨论 → 提炼共识 → 保存结果”的进度动画；关闭弹窗后服务端继续处理。

会话读取只针对各 Agent 自己的 JSONL 记录：Pi、Claude Code、Codex（未压缩 rollout）、Cursor agent transcript。Codex 的 `.jsonl.zst` 压缩会话、GUI 数据库型会话和未列出的 Agent 暂不支持；需要先导出为普通 JSONL 或等待后续适配。

## 行内评论（Review）

在预览中选中文字，会出现 **评论** 按钮：

1. 输入意见并添加。
2. 评论以行号 + 原文引用锚定，预览中对应文字高亮。
3. 打开预览顶部的 **评论** 面板查看线程、定位、标记解决或删除。评论卡片默认折叠，点左侧箭头或“N 条讨论”摘要展开；面板右上角可以 **全部展开 / 全部收起**。评论按**创建时间从早到晚**稳定排序，Agent 修改导致行号 / 锚点变化或评论被回复都不会改变列表顺序。
4. 如果几条评论需要一起改，点击评论卡片里的 **关联评论**，勾选同文档其他位置的评论并保存。卡片会显示 **N 条关联**，列出组内评论及其行号，可以点击跳转；之后也可以打开 **关联评论** 调整分组。
5. 同组评论会作为一条关联需求交给 Code Agent：它会一起阅读所有 thread，修改时把所有组内评论一起处理，并为每一条追加本轮 before/after。
6. **只有你可以确认解决**。Code Agent 的修改、回复和 MCP 工具都不会把评论标记为已解决；它只负责追加修改记录。你点击 **解决** 后线程才会关闭，点击 **重新打开** 可以继续。
7. **局部回滚此段**：Code Agent 每次修改时会保存修改前 / 修改后两份整篇文档快照，用于定位每个评论回合对应的段落。每条 Agent 修改下方可以分别选择 **回滚前** 或 **回滚后**。点击后先确认，然后**只把这条评论所在的 Markdown 段落**恢复为那一轮修改前 / 修改后的内容；其他位置后来的修改会保留。
   - 关联组回滚时，会恢复组内各评论对应的段落，而不是整篇文档。
   - 回滚会追加一条“版本回滚”系统记录（含回滚前 / 恢复后片段），评论重新打开等你确认。
   - `write_file` 自动回复会同时保存 before/after 完整快照；更早的历史回复如果只有 before/after 片段，会使用片段作为回滚前内容的兜底。
   - 如果当前文档结构变化太大，无法可靠定位该段落，mdlive 会拒绝回滚并提示，而不是粗暴覆盖整篇文档。
   - 只保留每篇文档最近 30 个快照；更早的版本按钮会消失。
   - 只有用户能回滚；Agent 没有回滚工具。
   - `write_file` 的 `comment_ids` 流程会带上精确的修改后内容；手动 `reply_comment`、Agent CLI 或旧版 MCP 回复时，服务端也会读取当前磁盘文档补一份快照。只有升级前的旧回复没有快照。

评论保存在本机 SQLite，不会写入 Markdown 文件本身；远程文档的评论同样保存在本机数据库。关联关系和完整文档快照也只存在 SQLite 中。

Code Agent 通过 MCP / CLI 读取并回复评论：

- `list_comments` 获取评论 ID、原文、行号、thread 和 `groupId`；同组评论还会在 `groups` 中列出。
- `read_file` 会同时返回文档的评论和评论组。
- `reply_comment` 可以附带 `before` / `after` 内容；但它和 `write_file` 都不能 resolve 评论。Agent 即使认为已经改完，也必须保持线程打开，等待用户确认。
- 修改文件时如果传入 `comment_ids` + `comment`，`write_file` 会自动把这一轮修改前后的段落追加到对应评论，并更新评论锚点，形成“最初内容 → 每轮修改后内容”的审阅记录。若只传了关联组的一部分 ID，返回结果会警告并列出还应一起处理的 ID。每次这样的修改还会保存整篇文档快照用于定位段落，用户可以在评论面板局部回滚该评论对应的段落。
- 回滚不是 Agent 工具；MCP / CLI 都不能回滚或 resolve，只有用户能在界面操作。
- 如果对结果不满意，可以在评论卡片里点 **继续修改 / 回复** 写下新意见；评论会自动重新打开，Agent 下一轮会读取完整 thread（含之前所有 before/after）继续修改，直到你点 **解决**。
- 评论锚点会优先使用最新一轮修改后的内容；原选中文字始终作为“最初内容”保留显示。Agent 修改后原文被替换时，预览高亮和定位会跟随新内容。
- 服务端定位顺序：原始 quote / 最新 after 字面匹配 → 已保存的 Agent 段落 → 字母数字级模糊匹配。这样能处理 Markdown 粗体 / 链接 / 行内代码导致的“渲染文字 ≠ 源码文字”，以及局部回滚后最新一轮文字已不在文档中的情况。
- 如果仍然显示 **未定位**，通常是因为：评论对应文字已被完全重写且没有可用的 Agent 快照（例如旧回复或 Agent 直接改磁盘）；远程文件暂时不可读；或段落被移动 / 删除到无法可靠恢复。评论本身不会丢失，仍可在面板查看、回复或删除。

## 设置

点击顶部齿轮打开 **设置**，当前有两个分组：

### LSP 服务器

- 按语言配置 Go / TypeScript / TSX / JavaScript / JSX / Python / Rust
- 每项可以启用 / 停用、修改命令和参数（参数用空格分隔）
- 点 **检查** 只验证可执行文件是否存在，不会启动服务器
- **恢复默认** 可恢复内置命令
- 保存后会关闭已有语言服务器连接，下次跳转按新配置重新启动
- 配置保存在本机 SQLite，不写入项目

### 后台 Code Agent

- 选择 Agent 类型：pi / Claude Code / Codex / Cursor / 自定义
- 每篇文档拥有独立会话；在文档标题下方的会话选择器中新建 / 切换
- 可修改界面显示名称和可选启动命令
- 显示名称会用于评论、回复、状态提示中的 “Code Agent” 文案
- **检查命令** 只验证可执行文件是否存在
- 打开 **用户提交评论后自动运行 Agent** 后，新增评论或用户回复会通过 pi RPC 自动启动后台 pi Agent；提示词包含文档路径、评论 ID、thread 和已关联代码仓库；自动任务会禁用 pi 内置 bash/find 工具，只允许通过 mdlive MCP 访问文档和代码
- **每篇文档有自己的 Agent 会话**，可在文档标题下方的会话选择器中新建 / 切换；不同会话的 pi 上下文互相隔离
- 会话元数据存在 SQLite；pi 会话文件存放在 mdlive 状态目录的 `pi-sessions/` 下，应用重启后可以继续使用同一会话
- Agent 运行时，预览标题栏显示转圈和当前阶段，例如“正在读取文档”“正在写入文档”“正在生成修改内容”；完成后显示绿色确认，失败显示错误提示
- 自动运行目前只支持 pi；其他 Agent 类型只保存显示名 / 命令，仍由外部客户端通过 MCP 接入
- 自动运行需要本机 `pi` 已配置模型和 mdlive MCP；会使用真实的模型调用来修改文档，默认关闭，需要手动开启
- 每个文档 / 选中的 Agent 会话独立维护一个 pi RPC 进程；同一会话内多条评论按顺序排队处理，不会每条评论新建会话
- 不同文档、不同会话之间的上下文隔离；切换会话后新的评论任务进入新会话，已排队的旧任务仍在原会话执行
- 只有应用重启、pi 进程崩溃 / 超时，或 Agent 命令 / 参数修改后才会重建进程；会话文件可用于恢复上下文
- 成功、失败会通过页面 toast 提示。Agent 不应 resolve 评论，仍由用户确认
- 手动 Agent 会话仍建议在切换配置后重新加载 MCP

设置 API：

- `GET /api/settings` / `GET /api/settings/defaults`
- `PUT /api/settings`
- `POST /api/settings/check-command`

## 文档关联代码仓库

在预览右上角点击 **代码** 按钮，打开代码仓库管理：

1. 输入代码仓库根目录，或点 **浏览** 选择目录
2. 点 **关联**；同一仓库可以关联到多篇文档
3. 关联后，文档的 `read_file` 会返回 `codeRepos`；Agent 可以直接用 MCP 代码工具分析仓库

Agent 代码工具：

- `list_code_repos`：按文档路径读取已关联仓库
- `code_repo_info`：路径、Git 分支 / HEAD、未提交文件
- `list_code_files`：列出源码文件（跳过 `.git`、`node_modules`、构建目录等）
- `read_code`：按行读取文件（单文件上限 1 MiB）
- `search_code`：大小写不敏感文本搜索，返回路径、行号和匹配行

代码仓库是**只读**的；mdlive 不会修改代码。关联关系保存在本机 SQLite，不写入 Markdown。GitHub 私有仓库可以使用 SSH 地址（需要已配置 SSH key），或在本机设置 `GITHUB_TOKEN` / `GH_TOKEN`，或先 `gh auth login`。

### 切换分支（本地 / GitHub）

代码浏览窗口顶部会显示当前分支。点击分支按钮可以读取分支列表并随时切换：

- **GitHub 仓库**：读取远程分支列表，拉取后更新 mdlive 克隆缓存；不会修改远程仓库
- **本地 Git 仓库**：列出本地分支，执行 `git checkout`；切换前会二次确认
- 本地仓库有未提交改动时会拒绝切换，避免把未提交内容带到另一个分支
- 切换后会更新代码浏览文件树 / 源码，并重置 LSP 连接
- 本地分支切换会改变该仓库的工作区，但不会修改 Markdown 文档

### 页面内代码浏览与 LSP

点击预览右上角的 **代码**，再选择文件：

- 左侧按原始目录树展示文件，支持筛选和文件夹展开 / 收起；右侧 CodeMirror 只读查看源码，带语法高亮
- 点击代码符号后按 **⌘/Ctrl + 点击**，或点顶部 **跳转定义**
- LSP 返回的目标如果仍在仓库内，会打开文件并定位到定义行
- 定义在仓库外（例如 Go 标准库）时会提示外部路径，不会越界打开
- 语言服务器按文件类型自动选择：
  - Go：`gopls`（`go install golang.org/x/tools/gopls@latest`）
  - TypeScript / JavaScript：`typescript-language-server --stdio`
  - Python：`pyright-langserver --stdio`
  - Rust：`rust-analyzer`
- 未安装对应的语言服务器时，代码浏览仍可用，跳转会显示安装提示；服务端会额外查找 `~/go/bin`、`~/.local/bin`、Homebrew 路径

## 给 Code Agent 使用（CLI / MCP）

mdlive 启动后会把实际地址写入：

- macOS：`~/Library/Application Support/mdlive/server.json`
- Linux：`~/.config/mdlive/server.json`

### CLI

```bash
# 工作区概览
./bin/mdlive agent info

# 列出文件（--refresh 强制刷新远程目录）
./bin/mdlive agent list --refresh

# 读取远程 Markdown
./bin/mdlive agent read Ali/go/pkg/mod/github.com/cespare/xxhash/v2@v2.3.0/README.md

# 写入 / 新建（stdin 或 --content）
printf '# Hello\n' | ./bin/mdlive agent write Ali/notes/hello.md

# 搜索 / 最近修改
./bin/mdlive agent search todo
./bin/mdlive agent changes --since 30m --refresh
```

路径支持：

- canonical：`<rootID>/<相对路径>`
- 友好路径：`<目录名>/<相对路径>`，例如 `Ali/notes/todo.md`
- 唯一文件名 / 相对路径

默认连接 discovery 文件中的 mdlive；也可以 `--url http://127.0.0.1:4571` 或环境变量 `MDLIVE_URL` 指定。

### MCP

本机已配置的 pi MCP 入口：

```bash
pi mcp add mdlive --exposure direct \
  --description "Read and write the mdlive local + SSH/SFTP remote Markdown workspace" \
  -- /Users/iamkkk/Desktop/coding/mdlive/bin/mdlive mcp

pi mcp list
```

MCP 工具（共 13 个）：

- `workspace_info`
- `list_files`（可选 `refresh`）
- `search_files`
- `read_file`（同时返回评论）
- `write_file`（本地 / 远程 SFTP；可传 `comment_ids` 回复评论并保存可回滚快照；关联组应一次传入全部 ID）
- `list_comments`（返回评论、`groupId` 和 `groups`）
- `reply_comment`（支持 before / after；不能 resolve，只有用户能关闭评论）
- `list_code_repos`（文档已关联 / 全部代码仓库）
- `code_repo_info`（Git 分支、HEAD、未提交文件）
- `list_code_files`（仓库源码文件树）
- `read_code`（按行读取代码）
- `search_code`（跨代码文件搜索）
- `changes`（最近修改）

在已有 pi 会话中添加后需要 `/reload` 或新开会话。CLI/MCP 都通过本机 mdlive 服务操作工作区，因此需要 mdlive 正在运行；mdlive 默认前台不能编辑已有文档，普通 `PUT` 写请求会被拒绝；新建 / 导入 / 删除文档仍可用。Agent CLI/MCP 会带 `agent` 标记，可写入。已经运行的旧 MCP 进程不会发送这个标记，需要重启 MCP（pi 中使用 `/reload` 或新开会话）。

## 评审模式（默认，前台不编辑，Agent 写入）

mdlive 现在默认就是前台审阅窗口、后台由 Agent 改稿：

```bash
./bin/mdlive ~/Documents/notes
./bin/mdlive ~/Documents/notes/当前笔记.md
```

- 不渲染编辑器，不自动保存；保留新建 / 导入 / 删除文档入口，方便管理评审材料。
- 前台发出的普通 `PUT/POST/DELETE /api/doc` 会返回 403；Agent CLI/MCP 的请求带 `X-Mdlive-Agent` 标记，仍可在后台写入。
- 评论、关联、局部回滚属于审阅操作，在审阅模式下可用；回滚只替换评论所在段落。
- 仍可通过文件树切换文档；后台 Agent 或外部编辑器保存后，预览经 fsnotify + WebSocket 自动更新。
- 快捷键只保留 `Mod+B`（侧边栏）和 `Mod+K`（搜索）。

## 调试模式（前后端热重载）

```bash
# 默认打开当前目录；DIR 可指定笔记目录
make debug DIR=~/Documents/notes

# 只热重载后端，不启动前端 build watch
make debug-backend DIR=~/Documents/notes

# 不自动打开浏览器
make debug DIR=~/Documents/notes OPEN=--no-open
```

`make debug` 启动三层：

1. `vite build --watch`：React / CSS 改动自动重建；
2. Go supervisor（`cmd/debugrun`）：监听 `.go` / `go.mod` / `go.sum`，重新编译 mdlive；
3. mdlive（`--debug`）：从磁盘 `web/dist` 加载前端，审阅模式。

热重载行为：

| 改动 | 行为 |
|------|------|
| React / CSS | Vite 重建 → Go 轮询到新 `index.html` → 浏览器整页刷新 |
| Go 后端 | supervisor 编译 → 旧进程优雅退出 → 启动新进程 → 前端 WebSocket 自动重连 |
| Markdown 文件 | fsnotify → WebSocket 推送 → 预览内容更新（无需刷新） |
| 编译错误 | 打印错误，**旧后端继续运行**，预览不中断 |

前端页面在 Go 重启时不会整页刷新，只会短暂显示“重新连接中…”，随后恢复实时同步；当前文件和滚动位置由 React 页面保留。

说明：

- 前端热更新是整页自动刷新，不是 React Fast Refresh；需要真正 HMR 时使用下面的 Vite 开发模式。
- 后端热重载依靠自动重启进程（约零点几秒中断），不是 Go 语言层面的 in-process 热替换。
- 调试模式默认审阅模式；前台不显示编辑器、不手动写文件，后台 Agent 仍可通过 MCP/CLI 写入。
- `--debug` 找不到 `web/dist` 时会回退到内嵌前端，并在界面提示热更新不可用。
- 安装后的 `bin/mdlive --debug` 只负责磁盘资源加载与审阅模式；监听源码、重新编译由 `make debug` / `cmd/debugrun` 提供（需要 Go 工具链）。

## 开发模式（Vite HMR）

后端和前端分开热更新，适合改 React 交互：

```bash
# 终端 1：Go 服务（默认 127.0.0.1:4571，--dev 允许前端尚未构建）
make dev-backend

# 终端 2：Vite 开发服务器（localhost:5173，代理 /api /raw /ws 到 Go）
make dev-web
```

如果只想调试审阅界面，可让后端以审阅模式启动：

```bash
make dev-backend DEV_FLAGS="--dev --preview"
make dev-web
```

> 全新克隆且还没有 `web/dist` 时，直接 `go run .` 会提示前端未构建；使用上面的开发模式或先执行一次 `make build`。

开发时访问 <http://localhost:5173>。生产构建：

```bash
make build     # 等价于 cd web && npm run build，然后 go build
```

## 交叉编译

```bash
make cross
# bin/mdlive-darwin-arm64
# bin/mdlive-darwin-amd64
# bin/mdlive-linux-arm64
# bin/mdlive-linux-amd64
```

把对应文件复制到目标机器即可运行，无需安装 Go/Node。前端资源已经嵌入。

## 快捷键

| 快捷键 | 功能 |
| --- | --- |
| `Mod+B` | 显示 / 隐藏侧边栏 |
| `Mod+K` | 聚焦文件搜索 |

> `Mod` 在 macOS 上是 ⌘，在 Linux 上是 Ctrl。审阅模式下仅保留 `Mod+B`、`Mod+K` 等浏览快捷键。

## 工作方式

```text
浏览器（React）
  │  REST：文件树 / 读取 / 评论 / 评审 / 服务端渲染兜底
  │  WebSocket：订阅当前文档、Agent 写入、外部修改推送
  ▼
Go 服务（标准库 net/http + gorilla/websocket）
  ├── goldmark：Markdown → HTML，Chroma 代码高亮
  ├── fsnotify：每个目录独立递归监听，外部编辑实时推送
  ├── roots：多目录注册 / 持久化 / 运行时添加移除
  ├── remote：SSH + SFTP 远程目录 / 轮询同步
  ├── agentcli / agentmcp：Code Agent CLI 与 MCP server
  └── go:embed：内嵌 web/dist，单二进制分发

开发调试（make debug）
  ├── vite build --watch
  ├── cmd/debugrun：监听 .go → go build → 热重启后端
  └── mdlive --debug：磁盘 web/dist + 评审模式
```

- **预览渲染在 Go**：Agent / 外部写入后由服务端渲染 HTML；代码高亮和 GFM 扩展始终一致。
- **即时反馈**：文件变化或 Agent 写入通过 WebSocket 推送；WebSocket 断开时前端自动改用 `/api/render`，连接恢复后重新订阅。
- **写入由 Agent 完成**：浏览器不提供编辑器、自动保存或保存快捷键；Agent 修改文档后可选择局部回滚。
- **图片**：Markdown 中相对路径图片会被重写为 `/raw/...`，由 Go 从该文件所属目录内安全读取；远程图片正常加载。

## 目录结构

```text
mdlive/
├── main.go                  # CLI、监听端口、打开浏览器、嵌入前端
├── internal/
│   ├── server/              # HTTP API、WebSocket、目录注册 / 浏览、远程 SFTP、路径沙箱、调试热更新
│   ├── render/              # goldmark + Chroma 渲染
│   ├── watcher/             # fsnotify 递归监听
│   └── ignore/              # 忽略规则 / Markdown 扩展名
├── cmd/debugrun/            # 后端热重载 supervisor（make debug）
├── web/
│   ├── src/                 # React + CodeMirror 前端
│   └── dist/                # 构建产物（被 go:embed）
├── Makefile
└── README.md
```

## HTTP API（本地）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/config` | 主目录、目录数、版本 |
| GET | `/api/tree` | 多目录文件树（`roots` + 扁平 `files`） |
| POST | `/api/agent/refresh` | 刷新远程文件树缓存（Agent 用） |
| GET | `/api/browse?path=&hidden=1` | 浏览器目录选择器（只列目录） |
| POST | `/api/roots` | 添加本地目录（JSON：`{"path":"~/notes"}`） |
| POST | `/api/roots/remote` | 添加远程目录（host / port / user / keyPath / path） |
| POST | `/api/remote/session` | 建立临时 SSH 浏览会话（不持久化） |
| GET | `/api/remote/browse` | 浏览远程目录（session + path） |
| DELETE | `/api/remote/session` | 关闭临时浏览会话 |
| DELETE | `/api/roots?id=` | 从工作区移除目录，不删除文件 |
| GET | `/api/doc?path=` | 读取并渲染文档（path 为 `<rootID>/<相对路径>`） |
| PUT | `/api/doc` | 保存（不存在则创建） |
| POST | `/api/doc` | 新建（已存在返回 409） |
| DELETE | `/api/doc?path=` | 删除 |
| GET | `/api/comments?path=` | 获取文档 / 全部评论 |
| POST | `/api/comments` | 添加评论（quote 为选中文本） |
| POST | `/api/comments/reply` | 回复评论（before / after；Agent 的 resolve 会被忽略） |
| POST | `/api/comments/resolve` | 用户标记解决 / 重新打开（界面使用） |
| POST | `/api/comments/group` | 设置评论关联组（同文档） |
| POST | `/api/comments/rollback` | 用户局部回滚到某轮 Agent 修改前 / 后（side=before/after） |
| GET | `/api/code/repos` | 所有已注册代码仓库 |
| GET | `/api/doc/code-repos?path=` | 文档已关联的代码仓库 |
| POST | `/api/doc/code-repos` | 添加 / 关联代码仓库（docPath + repoPath / repoId） |
| DELETE | `/api/doc/code-repos?path=&id=` | 取消文档与代码仓库的关联 |
| GET | `/api/code/info?repo=` | 仓库 Git 信息 |
| GET | `/api/code/files?repo=&query=&limit=` | 仓库源码文件列表 |
| GET | `/api/code/file?repo=&path=&start=&end=` | 读取仓库文件 |
| GET | `/api/code/search?repo=&q=&limit=` | 搜索仓库源码 |
| GET | `/api/code/branches?id=` | 本地 / GitHub 仓库分支列表 |
| POST | `/api/code/branch` | 切换本地 Git 或 GitHub 仓库分支 |
| POST | `/api/lsp/definition` | 跳转定义（repo / path / line / character） |
| GET / PUT | `/api/settings` | 读取 / 保存 LSP 与 Code Agent 设置 |
| GET | `/api/doc/discussions?path=` | 文档共识 + 已导入讨论会话 |
| GET | `/api/doc/discussions/available?path=` | 检测到的 Agent（Pi / Claude / Codex / Cursor）及可导入会话 |
| POST | `/api/doc/discussions` | 导入会话并提炼共识（path + sessionPath） |
| DELETE | `/api/doc/discussions?path=&id=` | 移除导入的讨论会话 |
| POST | `/api/doc/consensus/refresh` | 手动重新提炼文档共识 |
| GET | `/api/doc/agent-sessions?path=` | 文档的 Agent 会话列表 / 当前会话 |
| POST | `/api/doc/agent-sessions` | 新建文档 Agent 会话 |
| POST | `/api/doc/agent-sessions/active` | 切换文档当前 Agent 会话 |
| PATCH | `/api/doc/agent-sessions` | 重命名 Agent 会话 |
| GET | `/api/settings/defaults` | 默认 LSP 配置 |
| POST | `/api/settings/check-command` | 检查语言服务器 / Agent 命令是否存在 |
| DELETE | `/api/comments?path=&id=` | 删除评论 |
| POST | `/api/render` | 仅渲染，不写盘 |
| GET | `/api/highlight.css?theme=dark` | Chroma 主题 CSS |
| GET | `/raw/<path>` | 读取目录内的图片等附件（同样使用 `<rootID>/<相对路径>`） |
| GET | `/ws` | 实时预览 WebSocket |

## 安全说明

- 默认只监听 `127.0.0.1`，并拒绝非回环 Host；WebSocket 校验同源 Origin。
- 每个文件路径都必须带有目录 ID，并被限制在该目录内；`..`、绝对路径、指向目录外的软链接都会被拒绝。
- 通过界面 / `--add-dir` 添加目录等于显式扩大可访问范围；只添加你信任的目录。
- `/api/browse` 只返回目录名，不读取文件内容；它同样受到仅回环监听和同源校验保护。
- 远程模式会读取本机 SSH 私钥（或通过 ssh-agent 签名）；私钥内容不会发送给浏览器。远程主机指纹必须已在 `known_hosts` 中。
- 远程目录浏览使用内存中的临时会话（15 分钟过期），不写入磁盘；关闭弹窗时会主动释放。
- Agent CLI/MCP 只通过本机 API 访问工作区；discovery 文件仅包含地址/PID，不包含 SSH 凭据。
- Markdown 允许原始 HTML（这是本地笔记工具的预期行为），请不要把服务暴露到不可信网络。
- 如果确实需要局域网访问，可显式使用 `--addr 0.0.0.0:4571`；此时应用会关闭回环 Host 限制，请自行评估风险。

## 可调参数与已知限制

- 单文档上限 8 MiB；文件树最多展示 5000 个 Markdown 文件；远程附件读取上限 64 MiB。
- 评论是 mdlive 本地审阅数据，不随 Markdown 文件同步；换机器/清空配置后不会带上。评论高亮依赖浏览器 Custom Highlight API（Chrome / Safari 支持；不支持时仍可在评论面板查看原文）。
- 评论关联组目前只在同一个 Markdown 文档内建立；跨文档关联尚未支持。删除组内评论后，若组内只剩一条，关联会自动解除。
- 代码仓库支持本机 Git / 非 Git 目录和 GitHub（HTTPS / SSH 浅克隆缓存）；不支持远程 SSH/SFTP 工作区中的代码目录直接作为代码仓库。代码只读，单文件读取上限 1 MiB，搜索有结果数和扫描量上限。
- 本地 Git 仓库分支切换会改变本地工作区，并拒绝有未提交改动的仓库；只对代码仓库缓存执行分支切换。
- GitHub 私有仓库需要可用凭据（SSH key、`GITHUB_TOKEN` / `GH_TOKEN` 或 `gh auth login`）；首次克隆可能较慢，仓库缓存占用磁盘空间。
- LSP 跳转需要本机已安装对应语言服务器；CodeMirror 代码浏览本身不依赖 LSP。只支持跳到代码仓库内的文件，仓库外定义仅提示路径。
- 设置页中的 pi 自动运行会真实启动模型调用；其他 Agent 类型的命令目前只用于显示和检查，仍由外部客户端通过 MCP 接入。
- 自动运行默认关闭；启用前请确认本机 pi 的模型、MCP 配置可用。只支持 pi，且同一会话内多条评论串行处理。
- 每个使用过的 Agent 会话会保留一个 pi 进程；会话较多时资源占用会增加。切换会话不会中断已经运行的任务。
- 版本回滚是段落级别的：只替换该评论所在的 Markdown block（空行分隔），其他位置保留；关联组会恢复各成员对应段落。无法定位段落时会拒绝回滚，不会整篇覆盖。
- 每篇文档只保留最近 30 个完整快照；更早的版本不可回滚。新的 Agent 回复（包括 `write_file` 自动回复、手动 `reply_comment` 和旧版 MCP）会保存当前文档快照；升级前的旧回复没有回滚按钮。
- 评论卡片默认折叠；展开状态只在当前页面会话中保留，不会持久化。
- 远程目录每 15 秒轮询一次，不是 inotify 级别的实时监听；远程目录扫描与文档读写在独立 SSH 连接上进行，避免大目录扫描阻塞文件打开。
- 远程写盘采用临时文件 + rename（服务器不支持时降级）。
- 最多同时添加 32 个目录；目录列表保存在本地配置中。目录被移动 / 重命名或所在磁盘未挂载时，启动时会跳过并给出日志，需要在恢复后重新添加。
- 目录列表是一份全局工作区配置；多实例使用同一个 `--state-dir` 会共享并覆盖同一份列表。
- 忽略 `.git`、`node_modules`、隐藏目录、编辑器临时文件等。
- 不提供 WYSIWYG、图片上传、Mermaid 渲染；本项目的目标是“本地 Markdown 文件 + 可靠实时预览”。
- 删除文件使用浏览器原生确认框；文件恢复依赖系统回收站/版本控制。
