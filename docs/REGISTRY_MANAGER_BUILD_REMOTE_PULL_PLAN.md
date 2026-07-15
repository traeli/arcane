# Registry 管理节点构建与远程环境拉取方案

## 1. 文档目的

本文记录 Arcane 多环境场景下的 Registry 定制方案，供需求确认和后续实施使用。本阶段只形成方案，不修改业务代码。

本文由 OpenAI Codex 根据现有 Arcane 代码调用链辅助整理，实施前需要人工复核并按 `AI_POLICY.md` 完成真实环境测试。

实施状态（2026-07-15）：核心需求已经实现，包括 Publisher/Consumer 凭据拆分、旧凭据兼容回退、环境同步版本与状态、真实同步错误、远程 Manifest Pull 测试、ECR Repository/Tag 浏览，以及 Direct/Edge Agent 路由支持。AWS 默认凭据链、AssumeRole 和每环境凭据覆盖仍属于第 12 节定义的可选增强，不属于本次核心交付。

## 2. 已确认的真实使用方式

本需求不是“所有环境都执行 Quick Build 和 Push”。实际职责划分如下：

| 节点 | 主要操作 | Registry 权限要求 |
| --- | --- | --- |
| Arcane 管理节点（环境 ID `0`） | Quick Build、生成镜像 Tag、推送 AWS ECR | Push 权限 |
| 其他 Direct / Edge 环境 | Build Project、Deploy、Update Service、拉取 Compose 中引用的镜像 | Pull 权限 |

目标流程：

```text
管理节点 Quick Build
  -> 使用管理节点的 ECR Push 凭据
  -> 推送 image:tag 到 AWS ECR

远程环境 Build/Deploy Project
  -> 请求被转发到目标 Agent
  -> Agent 使用本地已同步的 ECR Pull 凭据
  -> 从 AWS ECR 拉取 image:tag
  -> 启动或更新 Project
```

因此，远程环境不需要具备 Quick Build 或镜像 Push 能力，但必须能够可靠取得并使用 ECR Pull 凭据。

## 3. 当前实现与问题定位

### 3.1 当前已有能力

现有 Registry 配置由管理节点统一维护：

- 前端 Registry API 使用 `/container-registries`，不携带环境 ID；
- Registry 的新增、更新、删除首先写入管理节点数据库；
- 保存后异步调用 `EnvironmentService.SyncRegistriesToRemoteEnvironments`；
- 管理节点解密 Registry 凭据，然后发送到 Agent 的 `/api/container-registries/sync`；
- Agent 使用自己的加密环境重新加密并保存；
- 环境健康检查成功后也会再次同步 Registry；
- Project 部署和服务更新在目标 Agent 执行，并从 Agent 本地 `ContainerRegistryService` 读取认证信息。

相关现有实现：

- `frontend/src/lib/services/container-registry-service.ts`
- `backend/api/handlers/container_registries.go`
- `backend/internal/services/environment_service.go`
- `backend/internal/services/container_registry_service.go`
- `backend/internal/services/project_service.go`
- `backend/internal/middleware/environment_middleware.go`

### 3.2 当前问题

当前“测试 Registry 成功”只说明管理节点可用，因为测试请求固定在管理节点执行。它不能证明：

- Registry 凭据已经同步到目标 Agent；
- Agent 可以使用同步后的密文；
- Agent 可以访问 AWS ECR 网络端点；
- Agent 的凭据具备目标 Repository 的 Pull 权限；
- Project 中引用的具体 image:tag 存在且可读取。

另外还存在以下可观测性问题：

1. Registry 保存后的远程同步是异步任务，保存接口不会等待同步结果；
2. 异步同步失败只记录服务端日志，UI 没有环境级状态；
3. 当前手动环境同步接口即使 Registry 同步失败，仍可能返回 `Environment synced successfully`；
4. 同步失败后，Project 只有在实际 Pull 时才暴露 `no basic auth credentials`、`pull access denied` 等下游错误；
5. 当前一个 ECR 凭据同时用于管理节点 Push 和远程节点 Pull，权限边界过大。

## 4. 方案原则

1. Registry 继续由管理节点统一配置，不要求每个环境重复录入；
2. 明确区分“管理节点发布镜像”和“远程环境消费镜像”；
3. 管理节点的 Push 凭据不应下发到远程 Agent；
4. 远程 Agent 只获得 Pull 所需的最小权限；
5. 管理节点测试和远程 Pull 测试必须是两个不同概念；
6. 同步失败必须可见、可重试、可定位，不能返回假成功；
7. Project 部署仍在目标 Agent 执行，不把远程 Pull 改回管理节点代拉。

## 5. 推荐总体方案

### 5.1 一个 Registry，区分两类凭据

对 AWS ECR Registry 增加两套用途明确的认证配置：

#### Publisher Credential

- 只保存在管理节点；
- 供环境 ID `0` 的 Quick Build / Push 使用；
- 不参与远程同步；
- 具备 ECR Push 所需权限。

#### Consumer Credential

- 在管理节点统一配置一份；
- 同步到需要消费镜像的远程 Agent；
- 只供 Project Pull 使用；
- 使用只读 IAM 权限；
- 不具备上传 Layer、PutImage 或删除镜像的权限。

第一版可以让所有远程环境共用一套 Consumer Credential。后续如有隔离要求，再支持按环境覆盖，不把“每环境重复配置”作为默认流程。

### 5.2 兼容现有单凭据配置

为了兼容已有数据，建议采用渐进规则：

```text
存在 Publisher + Consumer：
  管理节点 Push 使用 Publisher
  远程同步只发送 Consumer

只有现有 Credential：
  管理节点 Push 使用现有 Credential
  远程同步暂时也发送现有 Credential
  UI 标记“远程环境正在使用发布凭据，权限范围过大”
```

迁移完成后，可以由用户主动补充 Consumer Credential，而不是升级后立即破坏现有部署。

### 5.3 AWS IAM 权限边界

Publisher 至少需要获取 ECR Token 和推送镜像所需的 Layer / PutImage 权限。

如果启用管理节点的 Repository / Tag 浏览，Publisher 还需要 `ecr:DescribeRepositories` 和 `ecr:DescribeImages`。

Consumer 只保留拉取能力，例如：

- `ecr:GetAuthorizationToken`
- `ecr:BatchGetImage`
- `ecr:GetDownloadUrlForLayer`
- `ecr:BatchCheckLayerAvailability`

Repository 级权限应限制到 Arcane Project 实际使用的 ECR Repository。`GetAuthorizationToken` 通常仍需要资源 `*`，具体策略由部署账户复核。

## 6. 环境级同步状态

增加 Registry 与 Environment 的同步状态记录。建议最少包含：

| 字段 | 说明 |
| --- | --- |
| `registryId` | Registry ID |
| `environmentId` | 目标环境 ID |
| `desiredVersion` | 管理节点期望的配置版本或稳定哈希 |
| `appliedVersion` | Agent 已确认落库的版本 |
| `syncStatus` | `pending` / `syncing` / `synced` / `failed` |
| `lastSyncAt` | 最后一次同步时间 |
| `lastSyncError` | 脱敏后的同步错误 |
| `pullTestStatus` | `unknown` / `success` / `failed` |
| `lastPullTestAt` | 最后一次远程 Pull 测试时间 |
| `lastPullTestError` | 脱敏后的远程测试错误 |

配置版本只比较非敏感字段和凭据版本，不保存或返回明文 Secret。

管理节点只有在收到 Agent 成功响应后，才将 `appliedVersion` 更新为期望版本。

## 7. 同步行为调整

### 7.1 自动同步时机

保留并强化现有同步时机：

- 新增、更新、删除 Registry；
- 新增或配对环境；
- 环境从离线恢复为在线；
- 周期性环境健康检查；
- 用户点击“同步到当前环境”；
- 用户点击“同步到全部环境”。

同步内容只包含远程消费镜像所需的数据：

- Registry URL；
- Registry 类型；
- Region；
- Repository 配置；
- Consumer Access Key ID；
- Consumer Secret Access Key；
- Enabled 等非敏感设置。

不得同步 Publisher Credential 和管理节点缓存的临时 ECR Token。ECR Token 应由各 Agent 使用 Consumer Credential 自行申请和缓存。

### 7.2 修复假成功

同步接口必须返回真实结果：

```json
{
  "success": false,
  "data": {
    "environmentId": "production",
    "registries": {
      "success": false,
      "error": "failed to send registry sync request: agent unavailable"
    },
    "gitRepositories": {
      "success": true
    }
  }
}
```

异步全量同步可以继续后台运行，但必须把每个环境的最终状态持久化，不能只写日志。

### 7.3 同步范围

同步应支持：

- 单个 Registry -> 单个环境；
- 全部 Registry -> 单个环境；
- 单个 Registry -> 全部环境；
- 全部 Registry -> 全部环境。

Project Pull 故障时优先重试“单个 Registry -> 当前环境”，避免无关配置扩大失败范围。

## 8. 测试能力调整

### 8.1 管理节点发布测试

保留当前管理节点测试，但在 UI 中明确命名为：

```text
测试管理节点发布权限
```

该测试验证：

- Publisher Credential 能获取 ECR Authorization Token；
- 管理节点 Docker 可以登录 Registry；
- 可选：验证目标 Repository 的 Push 前置权限。

不得将此结果展示为所有环境通用的“Registry 正常”。

### 8.2 远程环境拉取测试

新增环境级接口，语义为测试消费权限，例如：

```text
POST /environments/{environmentId}/container-registries/{registryId}/test-pull
```

请求由管理节点转发到目标 Agent。Agent 必须读取自己的本地 Registry 配置，而不是接收浏览器传入的 AWS Secret。

测试分两级：

1. Registry 级认证：使用 Consumer Credential 获取 ECR Token；
2. 镜像级读取：对一个明确的 `repository:tag` 或 digest 请求 Manifest。

只执行 Registry Login 不能证明 Repository 级 Pull 权限，因此最终结果应以具体镜像读取验证为准。测试不应默认下载完整镜像 Layer。

建议请求：

```json
{
  "repository": "smart-ring-health-api",
  "tag": "79b007bf1234"
}
```

响应需要区分：

- Registry 配置未同步；
- Consumer Credential 缺失；
- AWS Token 获取失败；
- Repository 不存在；
- Tag 不存在；
- Repository AccessDenied；
- Agent 到 ECR 网络连接失败。

## 9. Project Build / Deploy 行为

### 9.1 保持现有执行节点

切换到远程环境后，Project Build / Deploy 请求继续由环境代理转发到目标 Agent。目标 Agent：

1. 解析 Compose 中非 `build:` 服务使用的镜像；
2. 根据镜像 host 匹配本地 Registry；
3. 使用本地 Consumer Credential 获取 ECR Token；
4. 执行镜像 Pull；
5. 执行 Compose Up。

管理节点不替远程环境下载镜像，也不把镜像通过 Arcane 中转。

### 9.2 部署前检查

在 Agent 开始 Pull 前增加明确检查：

- 镜像 host 是否匹配已启用 Registry；
- Consumer Credential 是否存在；
- Agent 应用的 Registry 配置版本是否有效；
- ECR Region 与 Registry URL 是否一致；
- 失败时返回结构化的 Registry / Repository / Image 错误。

第一阶段不建议每次 Project 部署前都执行额外完整 Pull 测试，因为真实部署本身已经会 Pull。重点是提前识别“配置未同步”和提供准确错误。

如果发现配置不存在或过期，推荐行为是：

```text
阻止当前部署
  -> 返回 registry_credentials_not_synced
  -> UI 提供“同步并重试”
  -> 管理节点同步 Consumer Credential
  -> Agent 确认应用版本
  -> 用户或 UI 重试 Project 部署
```

不建议 Agent 在请求中向管理节点临时索取明文 Secret，也不建议把 AWS Secret 附加在每次 Project 部署请求中。

## 10. UI 调整建议

### 10.1 Container Registries 页面

Registry 主列表继续表示管理节点的全局配置。新增环境选择器后，展示选中环境的消费状态：

| Registry | 管理节点发布 | 当前环境同步 | 当前环境 Pull | 最后错误 |
| --- | --- | --- | --- | --- |
| AWS ECR | 成功 | 已同步 | 成功 | - |
| GHCR | 未测试 | 同步失败 | 未测试 | Agent unavailable |

提供操作：

- 测试管理节点发布权限；
- 同步到当前环境；
- 测试当前环境拉取权限；
- 同步到全部环境；
- 查看同步详情。

环境 ID `0` 不展示“同步到当前环境”，因为它就是配置源。

### 10.2 Project 页面

Project 部署失败且错误属于 Registry 同步问题时，显示：

```text
当前环境尚未取得 AWS ECR 拉取凭据。
[同步凭据并重试]
```

如果凭据已同步但 AWS 拒绝访问，显示具体 Repository：

```text
当前环境无权读取：
145312557675.dkr.ecr.us-west-2.amazonaws.com/smart-ring-health-api:79b007bf1234
```

不得笼统提示用户重新配置整个 Registry，也不得引导用户登录服务器执行 `aws configure`。

## 11. Repository / Tag 浏览功能的位置

Repository / Tag 浏览仍属于全局 Registry 能力，默认由管理节点访问 AWS ECR：

- Repository 列表：ECR `DescribeRepositories`；
- Tag / Image 列表：ECR `DescribeImages` 或对应 Registry API；
- 切换 Arcane 环境不会改变仓库内容；
- 环境选择只改变“该环境是否已同步、能否 Pull”的状态展示。

因此页面上要明确分开：

```text
Registry 中实际存在的 Repository / Tag
              与
当前环境是否有权限 Pull
```

这两个状态不能混为一谈。

## 12. 分阶段实施建议

### 阶段一：修复可靠性和可见性

- 修复手动同步接口吞掉错误的问题；
- 保存每个环境的同步结果；
- 增加“同步到当前环境”；
- 增加目标 Agent 的 Pull 测试；
- Project 返回可识别的 Registry 同步错误。

阶段一可以继续兼容现有单凭据结构，优先解决“管理节点正常、远程节点无权限但无法定位”的问题。

### 阶段二：最小权限凭据拆分

- 增加 Publisher Credential；
- 增加 Consumer Credential；
- Quick Build 只使用 Publisher；
- 远程同步只下发 Consumer；
- 为旧 Registry 提供兼容和迁移提示。

### 阶段三：仓库与 Tag 浏览

- 增加 Repository 列表接口；
- 增加 Tag / digest 列表接口；
- 增加分页、搜索和缓存；
- 与环境级 Pull 状态组合展示。

### 阶段四：可选增强

- 支持每个环境覆盖 Consumer Credential；
- 支持 AWS Instance Role / ECS Task Role 等默认凭据链；
- 支持跨 AWS Account 的 AssumeRole；
- 支持 Registry 配置漂移检测和告警。

## 13. 不建议采用的方案

### 每个环境手工重复配置 Registry

会产生多份配置、轮换困难和环境漂移，只适合作为临时故障绕过方式，不应成为正式产品流程。

### 将 Publisher Credential 同步到所有 Agent

虽然改动最小，但所有远程环境都会获得镜像 Push 权限，违反最小权限原则。只能作为旧数据兼容模式。

### 每次 Project 部署都通过请求传递 AWS Secret

会扩大 Secret 暴露面，并增加日志、代理、审计和重试过程中的泄漏风险。

### 只使用 Registry Login 作为 Pull 测试

Registry Login 成功不代表目标 ECR Repository 允许 `BatchGetImage`，必须增加具体 Repository / Tag 的读取测试。

### 依赖服务器 AWS CLI 登录状态

Arcane 当前使用 AWS SDK 和数据库凭据。AWS CLI 登录状态不是可靠的应用认证来源，也不适合多环境同步。

## 14. 验收标准

1. 管理节点 Quick Build 能使用 Publisher Credential 推送 ECR；
2. Publisher Credential 不会出现在远程同步 payload 和 Agent 数据库；
3. Consumer Credential 可以一次配置并同步到所有启用的远程环境；
4. 用户可以看到每个环境的同步成功或失败状态；
5. 手动同步失败时 API 和 UI 必须返回真实错误；
6. 目标 Agent 可以对指定 `repository:tag` 完成 Pull 权限测试；
7. 远程 Project 能使用 Agent 本地 Consumer Credential 拉取 ECR 镜像；
8. Agent 无权限时，错误必须指出环境、Registry 和目标镜像，且不得泄漏 Secret；
9. 管理节点测试成功不能覆盖或伪造远程环境测试状态；
10. Direct 与 Edge 环境均通过同一业务语义验证；
11. 旧的单凭据 Registry 在迁移期仍可工作，并显示权限风险提示；
12. Registry 配置、同步、远程测试及 Project Pull 必须有后端单元测试和真实多环境人工验证。

## 15. 最终推荐

正式方案采用：

```text
管理节点统一配置 Registry
  + 管理节点专用 Publisher Credential
  + 远程环境专用 Consumer Credential
  + Consumer 自动同步到 Agent
  + 环境级同步状态
  + 环境级实际 Pull 测试
  + Project Pull 结构化错误和同步重试入口
```

这样既满足“管理节点统一 Quick Build 并推送”的工作方式，也能保证其他环境只获得从 AWS ECR 拉取 Project 镜像所需的最小权限。
