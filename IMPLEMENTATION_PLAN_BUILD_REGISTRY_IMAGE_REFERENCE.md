# 实施计划：更新 Git 源码、按 Commit 构建并推送镜像

## 1. 文档用途

本文档供后续 AI 开发模型直接执行。实施前必须完整阅读根目录的 `AGENTS.md`、`AI_POLICY.md` 和本文件，并先使用 CodeGraph 检查实际调用链。不得仅依据本文档中的文件名盲目修改代码；若当前代码已经存在等价实现，必须原地扩展和复用。

本功能是 Arcane 镜像构建页面 `/images/builds` 的定制增强。用户会预先在配置的 `buildsDirectory` 下手动 `git clone` 项目源码，源码目录中包含 Dockerfile。新增一个独立操作，在一次后端构建流程中完成：

1. 安全更新当前 Git 工作区；
2. 获取更新后的 HEAD commit；
3. 根据源码目录名称和 commit 自动生成镜像引用；
4. 构建镜像；
5. 推送到用户选择的已配置 Registry（首要目标为 AWS ECR）。

必须保留现有普通构建流程和交互，不得改变现有“构建”按钮的语义。

## 2. 已确认的产品流程

用户预先在 `buildsDirectory` 中准备源码，例如：

```bash
git clone git@github.com:example/arcane-test.git /builds/arcane-test
```

页面操作流程：

1. 在构建工作区中选择 `/builds/arcane-test`；
2. 选择已配置且启用的 AWS ECR Registry；
3. 选择或自动匹配 Repository `arcane-test`；
4. 点击新增按钮“更新源码并构建”；
5. 后端更新 Git 仓库并读取更新后的 HEAD；
6. 后端生成 `sha-<12位commit>` Tag；
7. 后端构建并推送镜像；
8. 页面和构建历史展示完整 commit 与最终镜像引用。

示例：

```text
Context:    /builds/arcane-test
Commit:     a1b2c3d4e5f6789012345678901234567890abcd
Image name: arcane-test
Tag:        sha-a1b2c3d4e5f6
Image:      145312557675.dkr.ecr.us-west-2.amazonaws.com/arcane-test:sha-a1b2c3d4e5f6
```

按钮的正式中文名称使用：

```text
更新源码并构建
```

辅助说明使用本地化文案表达“拉取 Git 最新代码，使用当前 commit 生成镜像版本，然后构建并推送”。

## 3. 范围和非目标

### 3.1 本次必须实现

- 新增独立的“更新源码并构建”按钮；
- 仅对 `buildsDirectory` 下的本地 Git 工作区启用；
- 更新 Git 后获取 HEAD commit；
- Tag 固定生成 `sha-` 加完整 commit 的前 12 位小写十六进制字符；
- 镜像名称默认来自所选构建上下文根目录的文件夹名称；
- 选择已配置 Registry 和 Repository；
- 自动构造最终镜像引用；
- 构建成功后使用现有 Registry 认证能力推送；
- 构建历史至少保存完整 source revision 和最终 tags；
- 同一源码目录的更新与构建必须互斥；
- 保持普通构建流程完全兼容。

### 3.2 本次不实现

- 不实现 Webhook；
- 不实现定时构建或 Git 轮询；
- 不在 Arcane 内提供 `git clone` UI；
- 不自动创建 AWS ECR Repository；
- 不实现从 Registry Tag 反向创建 Compose 项目；该需求作为后续独立阶段处理；
- 不改变普通构建按钮的 tags、push、load 或 provider 语义；
- 不自动 merge、rebase、reset、stash 或清理用户文件；
- 不把 Registry、Git 或 AWS 凭据返回浏览器；
- 不通过拼接用户输入执行 shell 命令；
- 不创建与现有 `BuildService`、`gitutil.Client` 或 Registry service 重复的包装层。

## 4. 兼容性契约

页面保留两个明确独立的操作：

| 操作 | 更新 Git | Tag 来源 | Push 行为 |
|---|---|---|---|
| 构建 | 否 | 现有手动输入 | 完全沿用现有配置 |
| 更新源码并构建 | 是 | 更新后的 HEAD commit | 必须推送到选定 Registry/Repository |

新增字段必须全部可选，并具有不改变旧行为的零值。建议在现有共享 Build request 中增加：

```go
type SourceUpdateMode string

const (
    SourceUpdateNone    SourceUpdateMode = "none"
    SourceUpdateGitPull SourceUpdateMode = "git-pull"
)
```

请求契约建议：

```json
{
  "contextDir": "/builds/arcane-test",
  "sourceUpdateMode": "git-pull",
  "registryId": "configured-registry-id",
  "repositoryName": "arcane-test",
  "push": true
}
```

兼容性要求：

- `sourceUpdateMode` 缺失或为 `none` 时，执行路径必须与当前版本一致；
- 普通构建继续接受和使用现有 `tags: string[]`；
- Git 更新模式不得信任前端提交的最终自动 Tag；最终引用必须在 pull 和读取 HEAD 后由后端生成；
- 不得把 Git 更新拆成“前端先 pull、再发 build”两个请求，避免两次请求间的竞态；
- Depot 对该模式如无法保证本地工作区与远端构建上下文一致，第一版应明确禁用，而不是假装支持。首版优先支持 local provider + push。

实施 agent 必须先确认实际共享构建类型位于何处，并在现有类型中原地扩展。

## 5. Git 更新设计

### 5.1 复用现有 Git 能力

项目已有 `backend/pkg/gitutil/git.go`，其中 `Client.GetCurrentCommit()` 可读取本地仓库 HEAD。必须扩展现有 `gitutil.Client`，不得创建新的 Git service 或调用薄包装 helper。

优先使用 go-git 或当前 `gitutil` 已采用的实现方式。除非现有库确实无法安全完成所需操作，并经用户明确同意，否则不得退回 `sh -c`、`bash -c` 或字符串拼接的 `git pull` 脚本。

### 5.2 安全更新语义

“更新源码”必须等价于只允许 fast-forward 的 pull：

```text
fetch 当前分支 upstream
→ 验证可以 fast-forward
→ 更新工作区
→ 获取新的 HEAD
```

不得产生 Arcane 自己的 merge commit。以下情况必须中止，且不得自动修改用户工作区：

- 不是有效 Git 仓库；
- detached HEAD；
- 当前分支没有 upstream；
- 已跟踪文件有修改；
- 存在未跟踪文件；
- 存在冲突状态；
- 远端更新无法 fast-forward；
- Git 认证失败；
- context 已离开或通过符号链接逃逸 `buildsDirectory`。

未跟踪文件第一版选择“拒绝”，以确保镜像内容确实对应 commit。后续如要允许，应另行增加明确配置，不得在本 PR 中放宽。

如果远端没有新 commit，应继续构建当前 HEAD，而不是报错。

### 5.3 Git 凭据边界

第一版使用 Arcane 后端运行环境已经可用的 Git 认证环境，例如挂载的 SSH key、`known_hosts` 或既有 credential helper。不要在本功能中新增凭据管理标准。

错误、事件、构建历史和日志中的 remote URL 必须脱敏，移除 URL 中可能存在的用户名、密码或 Token。不得记录私钥、Registry token、AWS secret 或临时授权令牌。

## 6. 路径和镜像引用规则

### 6.1 构建路径

后端必须：

1. 从 Settings 获取真实 `buildsDirectory`；
2. 对 builds root 和 context 做绝对路径、clean 和符号链接解析；
3. 确认 context 严格位于 builds root 内，或在产品允许时等于 builds root；
4. 确认 context 是目录且包含有效 Git 工作区；
5. 禁止仅依赖字符串前缀判断路径归属。

前端传入的 `contextDir` 永远是不可信输入。

### 6.2 镜像名称

镜像名称来自选定 Git 工作区目录的 basename，而不是 Dockerfile 所在子目录的 basename。若 UI 未来允许选择仓库内子目录，仍应使用 Git 仓库根目录名称。

合法化规则：

- 转为小写；
- 将不允许的字符稳定替换为 `-`；
- 去除开头和结尾的分隔符；
- 合法化后为空则拒绝；
- 优先复用仓库已有 Docker image/reference helper，不自行实现不完整解析器。

### 6.3 Repository

Registry 配置中的 `repositoryNames` 是允许选择的完整 Repository 路径。Git 更新模式：

- 选择 Registry 后，如列表中存在与自动镜像名完全相同的 Repository，则自动选择；
- 不存在时允许用户从已配置列表显式选择；
- 不允许自由输入未配置 Repository；
- 后端必须再次验证 Repository 属于该 Registry 的已配置列表；
- Registry 必须已启用。

如果产品最终要求 Repository 必须与文件夹名称完全一致，只需在实现时收紧为“不匹配则禁用按钮”；不得静默改名。本计划默认允许用户显式选择其他已配置 Repository，以适配 ECR namespace。

### 6.4 Tag

版本格式固定：

```text
sha-<HEAD commit 前 12 位>
```

后端同时保存完整 40 位 commit。不得使用 pull 前的 commit，不得使用时间戳替代 commit，也不得在发生 dirty worktree 时生成该 Tag。

完整引用：

```text
<normalized-registry-host>/<repository-name>:sha-<12-char-commit>
```

Registry host 的规范化优先复用现有实现。不得丢弃 URL 中可能具有语义的路径，也不得自己实现一个不完整的 Docker reference parser。

## 7. 原子流程和并发控制

Git 更新模式必须在一次后端 streaming build 请求中完成：

```text
验证请求和权限
→ 解析并验证 buildsDirectory/context
→ 获取该 Git 仓库的互斥锁
→ 检查工作区状态
→ fast-forward 更新
→ 获取 HEAD、branch、脱敏 remote
→ 生成并验证最终镜像引用
→ 调用现有 builder 构建并 push
→ 写入构建历史/事件
→ 释放锁
```

锁的 key 应使用解析符号链接后的 Git 仓库根目录。相同仓库不得同时执行普通构建和 Git 更新构建，否则一个任务可能在另一个任务读取构建上下文时修改文件。不同仓库应允许并行。

优先复用项目已有 keyed lock/singleflight/缓存设施。如果没有合适实现，可在 `BuildService` 内增加最小的 keyed mutex 生命周期管理，但不得创建只做转发的新 service。

锁必须覆盖 Git 更新和 Docker 读取上下文的整个阶段，并保证所有错误、取消和 panic-safe 路径释放。

## 8. 后端改动要求

实施 agent 应通过 CodeGraph 定位准确符号，预计涉及：

- `backend/pkg/gitutil/git.go`
  - 在现有 Client 中增加工作区状态、当前分支/upstream 和 fast-forward 更新能力；
  - 复用 `GetCurrentCommit()`；
  - 添加单元测试。
- 现有共享 build request/result 类型
  - 增加可选 `sourceUpdateMode`、`registryId`、`repositoryName`；
  - 如现有 result/record 类型适合，增加 source revision/branch/remote；
  - 所有零值保持兼容。
- `backend/internal/services/build_service.go`
  - 在现有 `BuildImage` 流程中原地加入 Git 模式分支；
  - 复用现有 Registry service 取得认证和 Registry 数据；
  - 生成最终 tags；
  - 增加按仓库路径的互斥；
  - 保持 handler 薄、业务逻辑在 service。
- `backend/api/handlers/images.go`
  - 只承担 typed input/output、权限和 streaming 连接；
  - 不把 Git 或引用组装业务放入 handler。
- `backend/internal/models/image_build.go` 和已有 build history 映射
  - 仅在需要持久化 source metadata 时扩展；
  - 如需数据库字段，必须同时提供 SQLite/PostgreSQL goose migrations 和真实 down migration。

Registry 验证必须走现有 `ContainerRegistryService`。如果现有 service 已能按 ID 获取 Registry、解析 ECR token、提供 builder auth，则直接调用；禁止复制解密或 ECR token 刷新逻辑。

### 8.1 构建历史建议字段

至少保存：

```text
sourceUpdateMode
sourceRevision       // 完整 commit
sourceBranch
sourceRepository     // 脱敏后，可选
tags                 // 已有最终镜像引用
```

如果减少数据库变更更符合现有模型，可将可选 source metadata 放入项目已有的结构化 metadata 字段，但必须保证详情 API 能稳定返回，不能只写日志。实施前先检查现有模型模式。

## 9. 前端改动要求

预计涉及：

- `frontend/src/routes/(app)/images/builds/+page.svelte`
- `frontend/src/routes/(app)/images/builds/components/build-config-panel.svelte`
- `frontend/src/routes/(app)/images/builds/components/build-form.types.ts`
- `frontend/src/lib/services/image-service.ts`（仅当现有方法类型需要扩展）
- `frontend/src/lib/types/docker.ts`（同步 API 类型）
- `frontend/messages/en.json`

必须使用现有 Svelte 5 runes、form、query、service 和 Registry selector 模式。

### 9.1 启用条件

“更新源码并构建”按钮只有在以下条件全部满足时启用：

- context mode 为本地 workspace；
- 已选择 builds root 下的源码目录；
- provider 为首版支持的 local provider；
- 已选择启用 Registry；
- 已选择有效 Repository；
- 当前没有构建任务；
- 用户具有现有镜像构建权限，并能读取所需 Registry 配置。

前端可以做友好预检，但 Git 仓库、dirty 状态、路径和 Repository 合法性必须以后端结果为准。

### 9.2 页面展示

Git 更新模式提交前展示：

```text
源码目录    /builds/arcane-test
镜像名称    arcane-test
镜像版本    更新源码后根据 Git commit 自动生成
目标仓库    <registry>/<repository>
```

更新成功后从 streaming 输出或最终 build record 展示：

```text
Commit      a1b2c3d4e5f67890...
Image       <registry>/<repository>:sha-a1b2c3d4e5f6
```

不要让前端在 pull 前猜测最终 Tag。所有用户可见文本必须只新增到 `frontend/messages/en.json` 并通过 `m.*()` 使用，不修改其他语言文件。

### 9.3 错误文案

至少覆盖：

- 当前目录不是 Git 仓库；
- 工作区存在未提交或未跟踪文件；
- 当前分支没有 upstream；
- 无法 fast-forward；
- Git 认证失败；
- Registry/Repository 未选择或已失效；
- 同一源码目录正在构建；
- Git 已是最新版本但构建失败；
- 构建成功但 push 失败。

错误应告诉用户可采取的下一步，但不得泄露内部绝对路径以外的敏感信息或凭据。

## 10. Streaming、活动记录和失败语义

沿用现有 JSON streaming 协议和 activity writer，不创建第二套日志标准。建议阶段信息：

```text
Checking Git workspace
Updating source from upstream
Source is up to date / Source updated
Resolved revision <short-sha>
Building image
Pushing image
Completed
```

前端显示的文字必须本地化；后端结构化 phase 可使用稳定英文标识。

失败语义：

- Git 更新失败：不得开始 Docker build；
- 镜像引用生成失败：不得开始 build；
- build 失败：不得声称 push 成功；
- push 失败：构建记录标记失败，并保留 commit、最终引用和安全日志以便重试；
- 客户端取消：传递 context cancellation，释放锁并正确结束 activity/build history；
- pull 成功但后续失败：不回退 Git 工作区，因为 fast-forward 回退会修改用户源码且具有破坏性。

## 11. 测试计划

### 11.1 Git 单元测试

使用临时本地 bare remote 和工作仓库，不依赖公网：

1. 有新 commit 时成功 fast-forward；
2. 已是最新时成功且 commit 不变；
3. dirty tracked file 时拒绝；
4. untracked file 时拒绝；
5. detached HEAD 时拒绝；
6. 无 upstream 时拒绝；
7. 本地/远端分叉时拒绝；
8. context cancellation；
9. 获取完整 commit 正确。

### 11.2 BuildService 单元测试

1. `sourceUpdateMode=none` 完全沿用现有 tags；
2. Git 模式 pull 后再读取 commit；
3. 使用更新后 commit 生成 12 位 Tag；
4. Repository 和 Registry 验证；
5. 最终 BuildRequest 的 tags 只有自动生成的完整引用；
6. Git 失败时 builder 未被调用；
7. build/push 失败记录完整 source metadata；
8. 同仓库并发被阻止或串行化；
9. 不同仓库可并行；
10. 路径越界和符号链接逃逸被拒绝；
11. Registry/ECR 凭据不进入日志或响应。

### 11.3 前端测试/检查

1. 普通构建按钮和手动 tags 行为无变化；
2. Git 按钮启用条件正确；
3. Registry 切换时清空不再有效的 Repository；
4. 同名 Repository 自动匹配；
5. Git 模式不要求用户输入 Tag；
6. streaming 状态和错误正确展示；
7. 构建历史能显示 source revision；
8. 所有文案均来自 Paraglide。

### 11.4 必须运行的验证

至少执行项目当前等价命令：

```bash
cd backend && go test ./...
just lint frontend
```

并按照 `AI_POLICY.md` 完成：

1. `./scripts/development/dev.sh start`；
2. 访问前端 `http://localhost:3000`；
3. 验证后端 `http://localhost:3552`；
4. 验证前后端热更新；
5. 手动测试普通构建回归；
6. 使用本地测试 Git remote 手动验证更新后构建；
7. 使用真实可访问的 AWS ECR 验证 push。

没有真实 AWS ECR 环境时，不得宣称 ECR 已完成人工验证；必须在交接中明确限制。AI 不能代替贡献者完成项目政策要求的人工确认。

## 12. 分阶段实施顺序

### 阶段 A：调查和契约确认

- 用 CodeGraph 获取 build request、builder、history、Registry auth、settings 和 streaming 的准确调用链；
- 检查是否已有 keyed lock、Docker reference validator、路径 containment helper 和 Git pull 能力；
- 输出拟修改文件清单和兼容性说明；
- 不在调查完成前创建新 helper/service。

完成条件：旧构建路径、Git 新路径和数据落点已明确。

### 阶段 B：Git 更新能力

- 原地扩展 `gitutil.Client`；
- 实现状态检查、upstream 检查和 fast-forward 更新；
- 编写完整临时仓库测试。

完成条件：Git 单测全部通过，且不调用不安全 shell。

### 阶段 C：后端构建编排

- 扩展共享 Build request 和 history 类型；
- 在 `BuildService` 中加入 Git 模式；
- 加入路径安全和仓库互斥；
- 复用 Registry service 生成认证；
- 后端生成最终镜像引用；
- 扩展 history/activity；
- 添加 service/handler 测试。

完成条件：普通模式回归测试通过，Git 模式单测覆盖成功及失败路径。

### 阶段 D：前端独立按钮

- 复用现有 Registry/Repository 选择器；
- 新增“更新源码并构建”；
- 保留普通构建按钮；
- 展示自动命名规则和后端返回的实际 commit/image；
- 补充 `en.json` 文案。

完成条件：两个构建入口行为独立，Svelte/TypeScript 检查通过。

### 阶段 E：完整验证和交接

- 执行后端测试、前端 lint、格式检查和 `git diff --check`；
- 启动完整开发环境；
- 人工验证普通构建、Git 更新构建、失败场景和 ECR push；
- 记录所有命令、结果和未覆盖限制；
- 不在未经用户授权时提交、推送或创建 PR。

## 13. 验收标准

功能只有在以下条件全部满足时才算完成：

- 原“构建”按钮行为没有变化；
- 新按钮名称为“更新源码并构建”；
- 非 Git 目录不能执行新流程；
- dirty/untracked/diverged 仓库不会被 Arcane 自动修改；
- pull 和 commit 解析在同一次后端构建请求内完成；
- Tag 来自更新后 HEAD，格式严格为 `sha-<12位>`；
- 构建历史保存完整 commit；
- 最终镜像引用由后端生成并验证；
- AWS 凭据和 Git 凭据不会出现在日志、历史或响应中；
- 同一仓库不存在 pull/build 竞态；
- 自动化检查通过；
- 按 `AI_POLICY.md` 完成人工验证并记录证据。

## 14. 实施禁止事项

- 禁止为了赶进度绕开 Huma typed handler；
- 禁止把业务逻辑放进 handler 或 Svelte 页面；
- 禁止创建只转发现有方法的 helper/service；
- 禁止复制 Registry 解密、ECR token 或 Git commit 逻辑；
- 禁止使用 Svelte 4 语法或 TypeScript `any`；
- 禁止在 `.svelte` 中硬编码用户可见文案；
- 禁止用前端计算的 Tag 作为权威值；
- 禁止自动执行 merge、reset、clean、stash 或 force push；
- 禁止扩展到 Webhook、定时任务或 Compose 自动创建；
- 禁止修改无关文件或覆盖用户已有变更；
- 禁止在未完成真实人工测试时宣称功能完整可用。

## 15. AI 执行与交接要求

实施 AI 应持续保留现有用户改动，分阶段验证，不得在测试失败时隐藏结果。完成后必须交付：

- 实际修改文件列表；
- 关键设计决定及与本文档不同之处；
- `git diff --check` 和 `git status --short`；
- 每条自动化命令及完整结果摘要；
- 人工测试步骤、操作者确认和结果；
- 未覆盖的平台、Registry 或凭据环境；
- 建议的 Conventional Commit 和 PR 文案；
- AI 使用披露。

根据 `AI_POLICY.md`，任何对外贡献必须披露 AI 使用。建议披露：

> This change was developed with assistance from OpenAI Codex for code analysis, implementation, and test planning. All changes were reviewed and the required runtime behavior was manually tested by a human contributor.

只有在人工贡献者确实完成复核和手动测试后，才能使用上述完整表述。否则必须如实说明尚未完成的验证。
