# Arcane 告警功能设计与实施方案

## 1. 文档目的

本文记录 Arcane 管理界面的告警能力设计，供需求确认和后续实施使用。本阶段只形成方案，不修改业务代码。

本文由 OpenAI Codex 根据现有 Arcane 代码辅助整理。实施前需要人工复核，并按 `AI_POLICY.md` 完成前后端、真实 Docker 环境和通知渠道测试。

## 2. 需求结论

左侧主导航增加 **告警（Alerts）**，不建议命名为“警告（Warning）”。“告警”更适合作为包含配置、当前异常和历史记录的功能模块。

首期需求如下：

1. 默认通知渠道为飞书自定义机器人；
2. 渠道下拉框同时展示钉钉和 Telegram，但首期标记为“即将支持”并禁用选择，避免用户保存一个实际无法发送的配置；
3. 支持容器健康、OOM、期望运行状态、CPU、内存和磁盘使用率告警；
4. CPU、内存和磁盘规则可独立启用并配置阈值；容器默认行为不提供配置；
5. 支持配置检测间隔；
6. 告警产生后只发送一次，恢复后发送一次恢复通知，避免每次检测都重复发送；
7. 所有规则按环境生效，切换环境后展示和编辑当前环境的规则。

升级后告警总开关默认关闭。用户完成飞书配置并确认规则后再主动开启，避免升级即产生未配置渠道的告警或历史记录。下文所述“固定开启”均表示在环境告警总开关开启后生效。

本文将 CPU、内存和磁盘定义为 **当前环境所在主机的资源指标**。单个容器的 CPU/内存阈值建议作为第二阶段功能，否则首期需要为每个运行中容器持续建立 stats 采样，成本和配置复杂度都会明显增加。

## 3. 与现有 Arcane 能力的关系

当前代码已经具备以下可复用能力：

- `backend/internal/services/notification_service.go` 已统一处理通知提供商、事件订阅、凭据脱敏和 Agent 向 Manager 转发；
- `backend/pkg/utils/notifications/` 已有 Telegram 等发送器和通用 Webhook 发送器；
- `backend/pkg/scheduler/` 已提供 6 字段 cron、动态重排和条件任务；
- `backend/pkg/scheduler/auto_heal_job.go` 已检查容器 health 状态，告警实现需要与它共享判定语义，但不能把“自动重启”和“发送告警”绑定为同一个开关；
- `backend/api/ws/handler.go` 已采集系统 CPU、内存和磁盘指标；
- `backend/pkg/authz/access_policy.go` 已统一维护左侧导航对应的访问面；
- `/settings/notifications` 已有通知配置页面，Telegram 已经是真实可用的内置渠道。

实施时必须复用这些能力。特别是系统指标采集目前位于 WebSocket Handler 内，应该将现有采集逻辑下沉到可复用的服务，再由仪表盘 WebSocket 和告警任务共同调用；不能在告警任务中复制一套 CPU、内存、磁盘算法。

现有 `/settings/webhooks` 是“外部请求触发 Arcane 操作”的入站 Webhook，与飞书机器人这种“Arcane 主动发送消息”的出站通知不是同一概念，不应混用。

## 4. 推荐界面

### 4.1 左侧导航

在资源区与设置区之间增加顶层入口：

```text
仪表盘
项目
环境
...
卷
告警                 <- 新增
事件
设置
```

路由建议为 `/alerts`，并注册后端拥有的访问面 `route.alerts`。页面始终跟随当前选中的环境。

### 4.2 页面结构

首期页面分为三个区域：

1. **告警状态**：显示监控是否启用、最近检测时间、下次检测时间、当前活动告警数量；
2. **通知渠道**：渠道下拉框、飞书机器人配置、发送测试消息；
3. **告警规则**：期望持续运行的容器/Compose 服务、CPU、内存、磁盘规则和检测间隔。

渠道下拉框：

| 选项 | 首期状态 | 说明 |
| --- | --- | --- |
| 飞书机器人 | 可用、默认 | 使用飞书自定义机器人 Webhook，建议同时支持签名密钥 |
| 钉钉机器人 | 即将支持、禁用 | 只展示，不允许保存为活动渠道 |
| Telegram | 即将支持、禁用 | Arcane 已有 Telegram 发送器，后续只需接入告警事件路由 |

如果希望首期就开放 Telegram，可以复用现有 Telegram 配置和发送器，不需要重新实现 Bot API。上述禁用状态只是按“其他渠道之后再构建”的当前范围设计。

### 4.3 飞书配置项

| 配置项 | 必填 | 说明 |
| --- | --- | --- |
| 启用通知 | 是 | 总开关 |
| Webhook URL | 是 | 仅接受 HTTPS；保存后加密，读取时只返回脱敏占位值 |
| 签名密钥 | 否 | 推荐配置；保存后加密和脱敏 |
| 消息标题前缀 | 否 | 默认 `Arcane` |
| 测试通知 | - | 首期先保存配置再测试，复用现有通知测试接口 |

安全建议：默认只允许飞书/Lark 官方机器人域名；如果未来允许自定义网关，应增加显式的“允许自定义地址”高级选项，防止利用 Webhook URL 访问内网地址造成 SSRF。

### 4.4 规则配置

| 规则 | 默认值 | 可配置项 |
| --- | --- | --- |
| 容器默认检测 | 固定开启 | 连续 2 次 unhealthy 告警；OOMKilled 立即告警；普通 exited/stopped 忽略，不提供配置项 |
| 期望持续运行 | 未选择目标 | 选择需要持续运行的容器或 Compose 服务；目标停止时告警 |
| CPU 使用率 | 开启，85% | 触发阈值、恢复阈值、持续时间 |
| 内存使用率 | 开启，90% | 触发阈值、恢复阈值、持续时间 |
| 磁盘使用率 | 开启，85% | 触发阈值、恢复阈值、持续时间 |
| 检测间隔 | 60 秒 | 30 秒、60 秒、5 分钟、自定义 cron |

建议限制最短检测间隔为 30 秒。CPU 是采样值，低于 30 秒会增加噪声和任务压力。高级用户可以使用与现有任务一致的 6 字段 cron 表达式，但普通界面优先展示易懂的预设值。

磁盘首期监控 `diskUsagePath` 所在文件系统，该路径已经是 Arcane 仪表盘磁盘统计的口径。页面必须明确显示实际检测路径和文件系统容量，避免用户误以为已经覆盖所有挂载盘。多磁盘规则放到第二阶段。

## 5. 告警判定

### 5.1 容器默认行为与可配置范围

Docker 的 `stopped/exited` 只表示容器没有运行，无法单独说明它是用户主动停止还是异常退出。以下情况都可能是正常行为：

- 一次性任务正常执行完成，退出码为 0；
- 用户通过 Arcane、Docker CLI 或其他管理工具主动停止；
- Compose 服务被主动执行 `down`；
- 容器原本就是停止状态。

因此首期不推断“是否意外停止”，也不向用户暴露复杂的退出判定配置。系统固定采用以下三个默认行为：

#### A. `unhealthy` 默认告警

- 容器配置了 healthcheck；
- `State.Health.Status == unhealthy`；
- 连续 2 次检测为 `unhealthy` 后触发；
- `healthy` 连续 2 次后恢复；
- 没有 healthcheck 的容器不参与该规则。

连续次数是系统默认值，首期不提供配置项。这是误报最少、首期最可靠的判断方式，也与现有 Auto Heal 的 health 语义一致。

#### B. `OOMKilled` 默认告警

- Docker OOM 事件或 Inspect 的 `State.OOMKilled == true` 时立即触发；
- 容器恢复运行且不再处于 OOM 状态后发送恢复通知；
- OOM 检测固定开启，首期不提供配置项。

Docker EventBus 用于及时捕获 OOM，周期检测用于断线或进程重启后的状态校正，两者应共同使用。

#### C. 普通 `exited/stopped` 默认不告警

无论退出码是否为 0，普通 `exited/stopped` 默认都不触发告警。系统不再尝试通过 Docker Events、操作历史或退出码猜测用户意图，因此不需要维护“Arcane 主动停止豁免”等容易误判的逻辑。

#### D. 只有“期望持续运行”需要配置

用户可以显式选择必须保持运行的目标：

- 单个容器；
- Compose project 下的一个或多个 service；

只有被选择为“期望持续运行”的目标进入 `exited/stopped/dead` 时才告警。此处不再判断停止原因：用户已经声明该目标应该运行，因此即使它是通过外部 Docker CLI 主动停止，发送“未运行”告警也符合配置语义。

建议 Compose service 保存 project ID、Compose project name 和 service name，而不是当前容器 ID，因为 Compose 重建后容器 ID 会变化。独立容器保存容器名称作为期望目标，并保留当前容器 ID 作为运行时快照；独立容器重建且名称保持不变时仍可继续匹配。目标被删除时将配置标记为 unavailable，不应无限重复通知。

“整个 Compose project”首期不支持。项目可能包含一次性任务、profile、缩容为 0 的 service 或不同副本数，自动推导“所有长期运行 service”容易产生歧义；用户首期明确选择 service 更可靠。

### 5.2 CPU、内存和磁盘

计算口径：

```text
CPU：沿用仪表盘的系统 CPU 使用率
内存：(MemoryUsage / MemoryTotal) * 100
磁盘：(DiskUsage / DiskTotal) * 100
```

内存必须继续沿用现有 ZFS ARC 修正和 LXC cgroup 处理，磁盘必须继续沿用现有 `diskUsagePath` 回退规则，保证仪表盘与告警看到的是同一数值。

资源告警不应由单个瞬时采样触发。推荐默认规则为“连续 3 次检测超过阈值”，或者在 UI 中表达为“持续 3 分钟”，后端根据检测间隔换算所需连续次数。

### 5.3 恢复阈值与迟滞

如果 CPU 告警阈值是 85%，数值在 84.9% 和 85.1% 之间波动会反复触发和恢复。每条资源规则需要独立的恢复阈值：

```text
CPU >= 85% 持续 3 次 -> 触发
CPU < 80% 持续 2 次  -> 恢复
```

默认恢复阈值建议比触发阈值低 5 个百分点。UI 可先自动计算，在“高级设置”中允许修改。

### 5.4 状态机与防重复

每个“环境 + 规则 + 资源”维护独立状态：

```text
Normal -> Pending -> Firing -> Recovering -> Resolved
                 \-> NoData
```

- `Normal`：正常；
- `Pending`：已越过阈值，但尚未满足持续时间；
- `Firing`：首次满足触发条件，创建事件并发送一次告警；
- `Recovering`：已回到恢复阈值内，但尚未满足连续恢复次数；
- `Resolved`：发送一次恢复通知，然后回到正常监控；
- `NoData`：采集失败，不得错误地把正在告警的事件标记为恢复。

默认不周期重复发送同一个活动告警。后续可以增加“仍未恢复时每 30 分钟/1 小时提醒一次”，且必须有最短冷却时间。

## 6. 数据模型

复杂告警状态不适合全部塞入普通 settings 字符串。建议新增有明确类型的模型，并嵌入 `models.BaseModel`。

### 6.1 `AlertPolicy`

每个环境一条全局策略：

| 字段 | 说明 |
| --- | --- |
| `EnvironmentID` | 环境 ID，唯一索引的一部分 |
| `Enabled` | 当前环境告警总开关 |
| `Schedule` | 6 字段 cron |
| `Provider` | 首期为 `feishu` |
| `NotifyOnRecovery` | 是否发送恢复通知，默认 true |
| `ReminderInterval` | 重复提醒间隔，首期可为空 |

### 6.2 `AlertRule`

| 字段 | 说明 |
| --- | --- |
| `EnvironmentID` | 所属环境 |
| `Type` | `cpu` / `memory` / `disk` |
| `Enabled` | 单项开关 |
| `Threshold` | 触发阈值；容器规则可为空 |
| `RecoveryThreshold` | 恢复阈值 |
| `ForDurationSeconds` | 必须持续多久 |
| `Config` | 规则特有的小型 JSON 配置 |

`unhealthy` 和 `OOMKilled` 属于固定的系统检测行为，不创建用户可编辑的 AlertRule。“期望持续运行”直接由 AlertTarget 表达，也不需要额外的 AlertRule。它们都受当前环境告警总开关控制。

### 6.3 `AlertTarget`

保存用户配置的“期望持续运行”对象：

| 字段 | 说明 |
| --- | --- |
| `EnvironmentID` | 所属环境 |
| `TargetType` | `container` / `compose_service` |
| `TargetID` | 独立容器当前 ID 或 project ID |
| `TargetName` | 独立容器名称或 Compose project name，用于容器重建后的稳定匹配 |
| `ServiceName` | Compose service 名称；其他目标为空 |
| `Enabled` | 是否参与检测 |
| `DisplayName` | 展示名称快照 |

### 6.4 `AlertIncident`

该表是防重复和恢复通知的持久化依据，不能只放在进程内存中，否则 Arcane 重启后会重复告警。

OOM EventBus 和周期检测可能同时观察到同一个异常，因此 incident 写入必须是幂等 upsert，并以“环境 + 规则类型 + 资源稳定标识 + 活动状态”保证同一时刻只有一个活动 incident。AlertEvaluationJob 还需要进程内 non-overlap 保护，避免上一次检测未完成时被 cron 再次启动。

| 字段 | 说明 |
| --- | --- |
| `EnvironmentID` | 所属环境 |
| `RuleType` | 规则类型 |
| `ResourceType` | `system` / `container` / `filesystem` |
| `ResourceID` | 容器 ID 或磁盘路径的稳定标识 |
| `ResourceName` | 展示名称 |
| `Status` | `pending` / `firing` / `resolved` / `no_data` |
| `StartedAt` | 首次异常时间 |
| `FiredAt` | 首次发送时间 |
| `ResolvedAt` | 恢复时间 |
| `LastObservedAt` | 最近成功采样时间 |
| `LastValue` | 最近指标值 |
| `Threshold` | 触发时阈值快照 |
| `FailureCount` | 连续失败次数 |
| `RecoveryCount` | 连续恢复次数 |
| `LastNotificationAt` | 最近通知时间 |
| `Metadata` | 退出码、OOM、health 输出等脱敏信息 |

Provider 凭据继续由现有 `NotificationSettings` 保存、加密和脱敏，不在 `AlertPolicy` 中复制 Webhook URL 或密钥。飞书应作为新的通知 provider 接入现有通知服务，而不是为告警单独写 HTTP 发送逻辑。

## 7. 后端设计

### 7.1 服务职责

建议职责拆分如下：

- `SystemService`：承接现有系统指标采集，返回 `types/system.SystemStats`；
- `AlertService`：规则 CRUD、状态机、incident 持久化、告警/恢复事件；
- `NotificationService`：增加飞书 provider 和结构化 alert 通知内容；
- `AlertEvaluationJob`：按计划获取 Docker/系统指标并调用 `AlertService`，自身不承载业务判定；
- `EventService`：在触发和恢复时记录可审计事件；
- `JobScheduler`：继续负责调度和配置变更后的重排。

不要新建 WebSocket 采集器、HTTP 透传 helper 或第二套通知服务。

### 7.2 Manager、Direct Agent 与 Edge Agent

最可靠的执行位置是指标所在环境：

- 本地环境 ID `0`：Manager 本地的告警任务执行；
- Direct/Edge 环境：Agent 本地的告警任务执行；
- Agent 使用现有 Manager dispatch 通道发送结构化告警；
- Manager 根据全局保存的飞书配置真正发送消息。

需要扩展 `types/notification.DispatchKind`，增加结构化 `alert_firing` 和 `alert_resolved`（或一个带状态的 `alert`）载荷，不能从 Agent 发送已经拼好的飞书 JSON。

告警配置接口属于环境资源接口：本地环境直接落在 Manager 数据库，远程环境请求沿用现有 Environment Middleware 转发到 Agent，并落在 Agent 本地数据库。这里不需要额外设计配置同步协议，也不应在 Manager 和 Agent 各保存一份可产生冲突的规则副本。

通知渠道凭据仍由 Manager 全局保存。若 Manager 暂时不可达，Agent 不应阻塞检测；发送失败至少写入 Agent 日志和 incident 状态。带上限和过期时间的通知 outbox 属于第二阶段，连接恢复后再提供可靠重试。

### 7.3 Huma API

保持现有 Huma v2 typed handler 模式，建议接口：

```text
GET  /environments/{id}/alerts/config
PUT  /environments/{id}/alerts/config
GET  /environments/{id}/alerts/incidents
POST /environments/{id}/alerts/run
```

`PUT config` 使用完整、带版本的 typed DTO，服务内使用事务同时更新 policy 和 rules。`run` 用于“立即检测”，应复用同一个 Job/Service 执行入口。

飞书测试不新增 `/alerts/test`。它应扩展现有 Manager 本地接口：

```text
POST /environments/{id}/notifications/test/feishu?type=alert
```

`/notifications` 已被 Environment Middleware 明确保留在 Manager，不会错误代理到 Agent；测试使用已保存并解密的全局飞书配置，同时用 `{id}` 解析测试消息中的环境名称。

首期如果不展示历史列表，可以先不开放 incidents API，但数据库状态仍然需要存在以实现防重复。

### 7.4 权限

建议增加：

- `alerts:read`：查看当前状态和历史；
- `alerts:manage`：编辑规则、立即检测；
- 通知渠道密钥仍由现有 `notifications:manage` 控制。

`route.alerts` 对拥有 `alerts:read` 或 `alerts:manage` 的用户可见。编辑飞书配置时还要检查 `notifications:manage`，不能只依赖前端隐藏按钮。

## 8. 飞书通知格式

首期建议使用结构化卡片或富文本，但业务内容先保持简洁：

```text
[告警] production CPU 使用率过高
环境：production
主机：docker-host-01
当前值：92.4%
触发阈值：85%
持续时间：3 分钟
开始时间：2026-07-20 14:32:00 Asia/Shanghai
```

恢复消息：

```text
[恢复] production CPU 使用率已恢复
峰值：94.1%
恢复值：76.8%
持续时间：12 分钟
```

容器 health、OOM 或期望运行状态消息至少包含环境、容器名称、容器 ID 短值、Compose project/service 标签、状态、health、退出码、OOMKilled 和最近一次状态变化时间。不得把环境变量、完整日志或 Secret 放入通知。

时间使用 Arcane 配置的时区，同时保留机器可解析的 UTC 时间用于 API。

## 9. 更有效的增强建议

### 9.1 告警与自动修复解耦

“发现 unhealthy”与“自动重启 unhealthy 容器”必须是两个独立动作：

- 告警可以开启但不自动修复；
- Auto Heal 可以执行后发送现有 auto-heal 通知；
- 第二阶段再将同一故障的告警和 Auto Heal 关联为同一个 incident，并在恢复消息注明“由 Auto Heal 重启后恢复”。首期两套通知各自保持幂等即可，不把跨任务关联作为验收阻塞项。

### 9.2 增加数据不可用告警

远程环境离线、Docker API 超时、指标采集失败都不等于资源恢复。建议连续多次采集失败后产生 `monitor_no_data` 告警，并在恢复采集后关闭。该规则可在第二阶段默认开启。

### 9.3 告警静默与维护窗口

部署、批量更新或维护期间容器状态会频繁变化。第二阶段建议支持：

- 静默当前环境 30 分钟/1 小时；
- 定时维护窗口；
- 对单个容器或 Compose project 静默；
- 部署动作触发短暂的自动静默。

### 9.4 告警历史与确认

左侧“告警”最终不应只是设置表单。建议展示：

- 当前活动告警；
- 最近 7/30 天历史；
- 已确认/未确认；
- 触发次数、首次发生、最近发生、恢复耗时；
- 按环境、规则、严重级别过滤。

### 9.5 严重级别

首期可将 OOM、期望持续运行目标停止、磁盘接近满设为 Critical，CPU/内存和 unhealthy 设为 Warning。第二阶段支持同一指标两级阈值，例如磁盘 85% Warning、95% Critical。

### 9.6 可行性与难度审查

文档中的功能没有技术上完全不可行的要求，但如果一次性完成原“阶段 1”，改动面较大。以下是结合当前代码后的真实难度：

| 能力 | 难度 | 结论 |
| --- | --- | --- |
| 左侧入口、规则表单、渠道下拉框 | 低 | 可直接实施 |
| 飞书文本通知、签名、凭据加密/脱敏 | 中 | 复用现有 NotificationService，首期可做 |
| unhealthy 连续检测 | 中 | 复用 Auto Heal 的 Inspect 方式，首期可做 |
| OOM EventBus + Inspect 校正 | 中 | 已有 Docker EventBus，首期可做 |
| 期望持续运行的独立容器/Compose service | 中 | 已有 Compose labels helper；首期可做 |
| incident 持久化、防重复、恢复通知 | 中高 | 是可靠告警的必要成本，不建议删除 |
| CPU/内存/磁盘采集下沉 | 中高 | 需要重构 WebSocket Handler 并保持现有仪表盘测试通过；可行但回归风险最高 |
| Direct/Edge Agent 全链路 | 中高 | 现有代理和 notification dispatch 可扩展，但需要多进程、断线和权限集成测试；可拆为 1B |
| 新增独立 `alerts:read/manage` RBAC | 中 | 可行；若需缩短首期，可暂时复用 `notifications:manage`，以后再细分 |
| 飞书富文本卡片 | 中 | 不是核心能力；首期使用纯文本更稳妥 |
| 整个 Compose project 自动推导长期服务 | 高且语义不可靠 | 首期不做，只允许明确选择 service |
| Auto Heal 与 incident 自动关联 | 高 | 涉及两个任务的时序和去重，第二阶段再做 |
| Agent 断线 outbox 与重试 | 高 | 需要队列、幂等和过期策略，第二阶段再做 |
| 自定义飞书网关且同时防 SSRF | 高 | 首期只允许官方 HTTPS 域名，不开放自定义网关 |
| 多磁盘、单容器资源阈值、静默、升级策略 | 高 | 全部放到第二阶段 |

最值得考虑延期的是 Direct/Edge 全链路和 CPU/内存/磁盘采集重构。前者可以先只支持本地环境，后者如果也延期，首期就只剩容器 health、OOM 和期望运行状态告警。incident 持久化虽然工作量不小，但删除后会出现重启重复通知和无法可靠恢复的问题，不建议用内存状态替代。

## 10. 实施阶段

### 阶段 1A：本地环境可靠 MVP

1. 将现有系统指标采集从 WebSocket Handler 下沉到 `SystemService`，保持仪表盘行为不变；
2. 增加 AlertPolicy、AlertRule、AlertTarget、AlertIncident 及 SQLite/PostgreSQL migrations；
3. 增加告警服务、状态机和调度 Job；
4. 实现固定的 health/OOM 检测、期望持续运行目标、CPU、内存和磁盘规则；
5. 将飞书纯文本通知接入现有 provider、凭据加密/脱敏和测试通知；
6. 增加 Huma typed API、RBAC permission 和 access surface；
7. 增加 `/alerts` Svelte 5 页面及 `frontend/messages/en.json` 文案；
8. 下拉框展示禁用的钉钉和 Telegram；
9. 记录告警和恢复事件。

如果希望进一步缩短首期，可以先延期第 1 项及 CPU/内存/磁盘规则，只交付容器 health、OOM、期望持续运行和飞书通知；数据模型和 API 不需要推倒重来。

### 阶段 1B：多环境

1. 在 Direct/Edge Agent 运行同一 AlertEvaluationJob；
2. 告警配置和 incident API 通过现有环境代理读写 Agent 本地数据库；
3. 扩展 Agent -> Manager 的结构化 alert dispatch；
4. 验证 Manager 离线时失败状态可见，首期不承诺自动补发；
5. 完成至少一个 Direct Agent 和一个 Edge Agent 的真实端到端测试。

### 阶段 2：可运维性

1. 活动告警和历史列表；
2. 确认、静默、维护窗口；
3. Agent 通知 outbox 和断线重试；
4. 重复提醒和升级策略；
5. 钉钉与 Telegram 告警路由；
6. 多磁盘监控；
7. 单容器 CPU/内存阈值；
8. no-data 和 restart-loop 告警；
9. 整个 Compose project 的监控范围；
10. Auto Heal 与 alert incident 关联。

## 11. 验收标准

### 11.1 规则行为

- 阈值短暂超过但未达到持续时间时不发送；
- 连续达到条件时只创建一个活动 incident、只发送一次；
- Arcane 重启后不会为同一活动异常重复发送；
- 指标下降到恢复阈值并持续满足恢复条件后只发送一次恢复通知；
- 指标采集失败不会被当作恢复；
- 修改检测间隔后任务无需重启进程即可重排；
- 环境切换后读取和保存的是对应环境规则。

### 11.2 容器行为

- 没有 healthcheck 的运行中容器不会被判为 unhealthy；
- health 为 starting 时不触发，连续 2 次 unhealthy 后触发；
- OOMKilled 立即触发；
- 未配置为“期望持续运行”的普通 exited/stopped 容器不触发，不考虑退出码；
- 配置为“期望持续运行”的容器或 Compose 服务停止后触发；
- Compose 重建导致容器 ID 变化后，service 级目标仍然生效；
- 目标删除后被标记为不可用，不无限重复通知；
- 同一个 alert incident 不会重复发送；首期不要求与现有 Auto Heal 通知合并。

### 11.3 通知与安全

- 飞书测试消息、告警消息和恢复消息均在真实飞书群验证；
- Webhook URL 和签名密钥数据库加密、API 返回脱敏；
- 日志、事件和错误响应不泄露 Webhook 或签名密钥；
- 非 HTTPS URL、非允许域名和内网地址按策略拒绝；
- Agent 发送的结构化 payload 在 Manager 侧重新校验，不能指定任意目标 URL；
- 没有 `alerts:manage` 的用户不能更新规则，没有 `notifications:manage` 的用户不能更新渠道凭据。

### 11.4 质量验证

实施完成后按仓库要求至少执行：

```bash
cd backend && go test ./...
just lint frontend
just test e2e
```

并启动真实开发环境，手工验证本地 Docker、至少一个 Direct 或 Edge Agent、飞书机器人、容器 health 变化、OOM、普通停止、期望持续运行目标停止、Compose 重建、CPU/内存/磁盘阈值和配置热更新。

## 12. 需要产品确认的默认项

编码前建议最终确认以下默认值；如果没有额外意见，可直接采用本文推荐值：

| 项目 | 推荐默认值 |
| --- | --- |
| 页面名称 | 告警 / Alerts |
| 规则作用域 | 当前环境主机 |
| 默认渠道 | 飞书机器人 |
| 钉钉、Telegram | 展示但禁用，标记即将支持 |
| 检测间隔 | 60 秒，最短 30 秒 |
| CPU | 85% 触发，80% 恢复，持续 3 次 |
| 内存 | 90% 触发，85% 恢复，持续 3 次 |
| 磁盘 | 85% 触发，80% 恢复，持续 3 次 |
| Health | 系统固定：连续 2 次 unhealthy 触发，连续 2 次 healthy 恢复，不提供配置 |
| OOMKilled | 系统固定：立即触发，不提供配置 |
| 普通 stopped/exited | 系统固定：不告警，不分析退出码或停止原因 |
| 期望持续运行 | 用户选择容器或 Compose 服务；选中的目标停止即告警 |
| 恢复通知 | 开启 |
| 重复提醒 | 首期关闭 |
