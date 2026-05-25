# ECSM Controller 测试报告与实验设计

报告日期：2026-05-22

## 1. 测试对象

本报告面向 `ecsm-controller` 的 desired state 控制链路：

- VSOA API：接收外部系统写入、删除、查询 desired state。
- Registry/RoseDB：持久化 desired state 与 service status，并通过 watch 暴露变更事件。
- Watcher：监听 desired state 变化，将服务名放入工作队列。
- Scanner：周期性读取 desired state 与 ECSM actual state，发现差异后入队。
- WorkQueue：按 `service_name` 去重并缓冲待处理任务。
- Reconciler：读取最新 desired state 与 ECSM actual state，计算并执行收敛动作。
- ECSM Client/Collector：采集 ECSM 服务列表，并调用 ECSM HTTP API 执行 create/start/stop/scale。

## 2. 测试目标

验证控制器能够在正常、异常和并发条件下，将 ECSM 服务实际状态稳定收敛到外部写入的 desired state。

核心验收点：

- desired state 写入、查询、删除、列表查询行为正确。
- RoseDB 持久化、watch 事件与 key 解析符合设计。
- Watcher 与 Scanner 都能把需要收敛的服务放入队列。
- WorkQueue 对同一服务的重复事件具备去重能力，处理完成后允许再次入队。
- Reconciler 对 create、start、stop、scale out、scale in、none 的判断正确。
- ECSM HTTP API 异常、超时、返回错误时不会导致控制器崩溃。
- 事件丢失或 Watcher 不可用时，Scanner 能作为兜底机制重新发现差异。
- 多服务、大量变更、重复更新场景下控制器无死锁、无任务永久丢失。

## 3. 测试环境

建议准备三类环境：

| 环境 | 用途 | ECSM 依赖 | RoseDB 目录 |
| --- | --- | --- | --- |
| 本地单元测试环境 | 测试纯函数、服务层、队列、存储接口 | 使用 mock/fake | 临时目录 |
| 本地集成环境 | 启动 controller、fake ECSM HTTP server、真实 RoseDB | fake HTTP server | `/tmp/ecsm-controller-test-*` |
| 真实联调环境 | 验证与真实 ECSM 平台兼容性 | `192.168.50.82:3001` 或实际环境 | 独立测试目录，避免复用生产数据 |

推荐配置：

```yaml
registry:
  rosedb:
    dir_path: /tmp/ecsm-controller-test
    sync: true
    watch_queue_size: 1024

vsoa:
  listen_addr: ":13447"
  password: ""

ecsm:
  scheme: http
  ip: 127.0.0.1
  port: 3001
  timeout_seconds: 5

scanner:
  interval_seconds: 2
```

## 4. 测试准入与准出

准入条件：

- `go test ./...` 编译通过。
- `make build` 或等价构建命令成功生成 `ecsm-controller`、`desired-vsoa-client`。
- 测试环境中的 VSOA 端口、RoseDB 目录、ECSM 地址彼此隔离。
- 若使用真实 ECSM，测试服务名必须使用专用前缀，例如 `test_ecsm_controller_*@版本号`。

准出条件：

- P0/P1 用例全部通过。
- P2 用例无阻塞性问题，遗留问题已记录。
- 异常与恢复类用例均能证明 controller 不中断主循环。
- 真实联调环境中 create/start/stop/scale 的 ECSM 实际状态与 desired state 一致。

## 5. 实验矩阵

### 5.1 单元实验

| 编号 | 优先级 | 实验项 | 输入/步骤 | 预期结果 |
| --- | --- | --- | --- | --- |
| UT-01 | P0 | service name 校验 | 写入空字符串、无 `@`、`name@`、`@tag`、包含 `/`、包含多个 `@` | 返回非法服务名错误 |
| UT-02 | P0 | desired action 校验 | 写入 `start`、`stop`、`restart`、空 action | 仅 `start`、`stop` 成功 |
| UT-03 | P0 | replicas 校验 | 写入 `0`、`1`、`3`、`-1`，VSOA update 不传 replicas | 非负值允许；负值与缺失值拒绝 |
| UT-04 | P0 | Reconciler diff: create | desired=start，actual 不存在 | 返回 `need_create` |
| UT-05 | P0 | Reconciler diff: start | desired=start，actual 存在但 offline/stopped | 返回 `need_start` |
| UT-06 | P0 | Reconciler diff: stop | desired=stop，actual running | 返回 `need_stop` |
| UT-07 | P0 | Reconciler diff: scale out | desired replicas 大于 `instanceActive` | 返回 `need_scale_out` |
| UT-08 | P0 | Reconciler diff: scale in | desired replicas 小于 `instanceActive` | 返回 `need_scale_in` |
| UT-09 | P0 | Reconciler diff: none | desired 与 actual 一致 | 返回 `none` |
| UT-10 | P1 | stop 场景副本差异 | desired=stop 且 replicas>0，actual replicas 不一致 | 先返回扩缩容 diff，再停止 |
| UT-11 | P0 | WorkQueue 去重 | 连续 Add 同一个 serviceName 多次 | 只取出一次 |
| UT-12 | P0 | WorkQueue Done 后再次入队 | Get 后 Done，再 Add 同一 serviceName | 能再次取出 |
| UT-13 | P1 | WorkQueue context cancel | 空队列 Get，取消 context | 返回 `ok=false` |
| UT-14 | P0 | Scanner 差异入队 | fake desired 与 fake actual 不一致 | 对应 serviceName 入队 |
| UT-15 | P1 | Scanner 一致不入队 | fake desired 与 fake actual 一致 | 队列为空 |
| UT-16 | P1 | Collector 分页 | fake ECSM 返回超过 50 条服务 | CollectServices 拉取完整列表 |
| UT-17 | P1 | create image ref 规范化 | 输入 `name:1.0.0`、`name@1.0.0`、`name@1.0.0#sylixos` | 转为 ECSM create 所需 ref |

### 5.2 VSOA API 实验

| 编号 | 优先级 | 实验项 | 步骤 | 预期结果 |
| --- | --- | --- | --- | --- |
| API-01 | P0 | 健康检查 | 调用 `/desired_state/healthz` | 返回 `ok=true` |
| API-02 | P0 | 写入 desired state | update `c_worker@1.1.0 start replicas=3` | 返回成功，RoseDB 中可查询到相同值 |
| API-03 | P0 | 更新覆盖 | 同一服务先写 replicas=1，再写 replicas=5 | query 返回最新 replicas=5 |
| API-04 | P0 | 查询单服务 | query 已存在服务 | 返回 `data.service_name/action/replicas` |
| API-05 | P0 | 查询不存在服务 | query 不存在服务 | 返回 `ok=false`，message 为不存在 |
| API-06 | P0 | 删除 desired state | delete 已存在服务，再 query | 删除成功，后续 query 不存在 |
| API-07 | P1 | 删除不存在服务 | delete 不存在服务 | 返回 `ok=false`，错误为 not found |
| API-08 | P1 | 列表查询 | 写入多个服务后 list | total 与 data 数量一致 |
| API-09 | P0 | 非法 payload | 空 payload、非 JSON、缺少 replicas、非法 serviceName、非法 action | 返回 `ok=false`，错误信息明确 |
| API-10 | P1 | Param/Data 兼容 | 分别用 VSOA Param 和 Data 传 payload | 两种方式均能解析 |

### 5.3 Registry 与 Watcher 实验

| 编号 | 优先级 | 实验项 | 步骤 | 预期结果 |
| --- | --- | --- | --- | --- |
| REG-01 | P0 | RoseDB 持久化 | PutDesired 后关闭并重新打开 DB | desired state 仍存在 |
| REG-02 | P0 | key 前缀隔离 | 同时写 desired/status 数据 | ListDesired 只返回 desired；ListStatus 只返回 status |
| REG-03 | P0 | watch put 事件 | 启动 WatchDesired 后 PutDesired | 收到 serviceName 与 put action |
| REG-04 | P0 | watch delete 事件 | 启动 WatchDesired 后 DeleteDesired | 收到 serviceName 与 delete action |
| REG-05 | P1 | watch 关闭 | 取消 context | watch channel 关闭，goroutine 退出 |
| REG-06 | P1 | watch disabled | watch_queue_size=0 时运行 Watcher | 返回 watch disabled 错误，App 启动失败或记录明确错误 |
| REG-07 | P1 | Watcher 入队 | fake store 发送 desired event | 队列可取出对应 serviceName |

### 5.4 Reconciler 与 ECSM 操作实验

| 编号 | 优先级 | 实验项 | ECSM fake 初始状态 | desired state | 预期 ECSM 请求 |
| --- | --- | --- | --- | --- | --- |
| REC-01 | P0 | 创建服务 | 服务不存在 | start replicas=3 | GET image config，POST `/api/v1/service`，factor=3 |
| REC-02 | P0 | 启动服务 | 服务存在，id 存在，offline | start replicas=0 | POST `/api/v1/service/start/ids` |
| REC-03 | P0 | 停止服务 | 服务存在，running | stop replicas=0 | POST `/api/v1/service/stop/ids` |
| REC-04 | P0 | 扩容服务 | active=1，factor=1 | start replicas=3 | GET detail，PUT `/api/v1/service` factor=3 |
| REC-05 | P0 | 缩容服务 | active=5，factor=5 | start replicas=2 | GET detail，PUT `/api/v1/service` factor=2 |
| REC-06 | P1 | 扩缩容后启动 | offline 且 replicas 不一致 | start replicas=3 | 先 PUT 更新副本，再 POST start |
| REC-07 | P1 | stop 前调整副本 | running 且 replicas 不一致 | stop replicas=2 | 先 PUT 更新副本，再 POST stop |
| REC-08 | P0 | 状态一致 | running active=3 | start replicas=3 | 不发写操作 |
| REC-09 | P0 | desired 已删除 | 队列里有服务名，但 desired 不存在 | 无 | Reconciler 跳过，不调用 ECSM |
| REC-10 | P1 | ECSM 查询失败 | CollectServices 返回 500/超时 | 任意 | Reconcile 返回错误，worker 继续处理后续任务 |
| REC-11 | P1 | ECSM 写失败 | Apply 返回 500 | 任意需要操作状态 | Reconcile 返回错误，controller 不崩溃 |
| REC-12 | P1 | actual 缺少 ID | 需要 start/stop/scale，但 actual ID 空 | start/stop/scale | 返回明确错误，不发送无效请求 |

### 5.5 端到端实验

| 编号 | 优先级 | 实验项 | 步骤 | 预期结果 |
| --- | --- | --- | --- | --- |
| E2E-01 | P0 | 写入后由 Watcher 触发创建 | 启动 controller + fake ECSM，VSOA update 新服务 start replicas=3 | fake ECSM 收到 create 请求，factor=3 |
| E2E-02 | P0 | 写入后由 Watcher 触发扩容 | fake ECSM 已有 active=1，update replicas=3 | fake ECSM 收到 update factor=3 |
| E2E-03 | P0 | 写入 stop 触发停止 | fake ECSM running，update stop | fake ECSM 收到 stop 请求 |
| E2E-04 | P0 | Scanner 兜底 | 不启动 Watcher 或丢弃 watch 事件，仅依赖 Scanner | 扫描周期内发现差异并入队收敛 |
| E2E-05 | P1 | 多服务混合收敛 | 同时写入 create/start/stop/scale 多种 desired | 每个服务最终达到对应 actual state |
| E2E-06 | P1 | 快速覆盖更新 | 同一服务连续写 replicas=1、5、2 | Reconciler 执行时读取最新 desired，最终收敛到 replicas=2 |
| E2E-07 | P1 | 删除 desired 后队列仍有旧事件 | update 后立即 delete | Reconciler 查询不到 desired 后跳过 |
| E2E-08 | P1 | controller 重启恢复 | 写入 desired 后停止 controller，修改 fake actual，再重启 | Scanner 发现差异并收敛 |
| E2E-09 | P2 | 真实 ECSM 联调 create/start/stop/scale | 在测试命名空间写入真实 desired | ECSM 页面/API 显示实际状态符合 desired |

### 5.6 异常与稳定性实验

| 编号 | 优先级 | 实验项 | 注入故障 | 预期结果 |
| --- | --- | --- | --- | --- |
| FAULT-01 | P0 | ECSM HTTP 500 | fake ECSM list 返回 500 | Scanner/Reconciler 记录错误，进程继续运行 |
| FAULT-02 | P0 | ECSM 超时 | fake ECSM 延迟超过 timeout | 返回超时错误，后续扫描仍继续 |
| FAULT-03 | P1 | ECSM 返回非 JSON | fake ECSM 返回非法 body | Collector 返回 decode 错误 |
| FAULT-04 | P1 | RoseDB 目录不可写 | 配置不可写目录 | App 启动失败，错误明确 |
| FAULT-05 | P1 | VSOA 端口冲突 | 端口已被占用 | App 启动失败或 server 错误通道返回错误 |
| FAULT-06 | P1 | context 取消 | 运行中取消主 context | Watcher、Scanner、Worker、VSOA server 退出，无 goroutine 泄漏 |
| FAULT-07 | P2 | 大量重复事件 | 对同一服务快速写入 1000 次 | 队列去重，最终按最新 desired 收敛 |
| FAULT-08 | P2 | 大量服务扫描 | RoseDB 写入 1000 个 desired，fake ECSM 返回 1000 个 actual | Scanner 在可接受时间内完成，无 panic |

### 5.7 性能与容量实验

| 编号 | 优先级 | 指标 | 实验方法 | 建议目标 |
| --- | --- | --- | --- | --- |
| PERF-01 | P1 | 单服务收敛延迟 | 记录 VSOA update 到 ECSM 写请求发出的耗时 | Watcher 路径 P95 < 1s |
| PERF-02 | P1 | Scanner 发现延迟 | 关闭 Watcher，仅靠 Scanner | P95 < `scanner.interval + 1s` |
| PERF-03 | P2 | 批量写入吞吐 | 1 分钟内写入 100/500/1000 个 desired | 无任务永久丢失，错误率 0 |
| PERF-04 | P2 | 队列内存增长 | 同服务重复写入 10000 次 | 队列长度保持去重，不随重复事件线性增长 |
| PERF-05 | P2 | ECSM 分页采集 | fake ECSM 返回 50、51、100、101 条服务 | 页数和总数计算正确 |

## 6. 推荐执行顺序

1. 先执行单元实验 `UT-01` 到 `UT-17`，保证核心判断逻辑稳定。
2. 执行 VSOA API 与 Registry/Watcher 实验，验证外部入口和持久化。
3. 使用 fake ECSM HTTP server 执行 Reconciler 与端到端实验。
4. 注入 ECSM/RoseDB/VSOA 异常，确认控制器可观测、可恢复。
5. 最后在真实 ECSM 环境执行 `E2E-09`，仅使用测试服务名和测试 RoseDB 目录。

## 7. 测试记录模板

| 字段 | 内容 |
| --- | --- |
| 用例编号 | 例如 `E2E-01` |
| 测试日期 |  |
| 测试人员 |  |
| 代码版本/commit |  |
| 配置文件 |  |
| ECSM 环境 | fake/真实地址 |
| 前置数据 |  |
| 执行步骤 |  |
| 实际结果 |  |
| 是否通过 | PASS/FAIL/BLOCKED |
| 日志与证据 | controller 日志、fake ECSM 请求记录、RoseDB dump、ECSM 截图/API 响应 |
| 问题单 |  |

## 8. 关键观测点

执行测试时建议固定采集以下证据：

- controller 日志中的 watcher/scanner/reconciler/queue 记录。
- VSOA client 返回的 JSON。
- RoseDB 中 `desired/services/` 与 `status/services/` 前缀数据。
- fake ECSM server 收到的 HTTP method、path、query、body。
- 真实 ECSM 服务列表中的 `name`、`status`、`factor`、`instanceOnline`、`instanceActive`。
- 进程退出码与是否存在 goroutine 卡死。

## 9. 当前风险与建议补充

- `ServiceStatus` 目前已有存储结构，但主收敛链路未明显写入状态；若后续需要审计，应补充 Reconciler status 写入和对应测试。
- `LogClient` 名称仍像只打日志，但当前已经会调用 ECSM 写接口；建议后续重命名或在文档中明确，避免测试人员误判风险。
- 创建服务时默认节点、VSOA 密码、健康检查路径和默认镜像配置是硬编码策略，应在真实 ECSM 联调中重点验证。
- Scanner 只扫描 desired 列表，不会处理 ECSM 中存在但 desired 已删除的服务；如果期望删除 desired 后自动停止或删除实际服务，需要先明确产品语义，再增加实验。
- WorkQueue 当前是内存队列，controller 进程重启后队列事件会丢失；依赖 Scanner 兜底，因此 `E2E-08` 必须作为回归用例保留。
