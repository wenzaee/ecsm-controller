# ECSM Controller KV Storage Demo
```
GOOS=sylixos GOARCH=arm64 go build -o ecsm-controller ./cmd/ecsm-controller
```
本文先定义一版 demo 规范，用来描述外部 RPC 参数如何转换为本地 RoseDB KV。当前目标是把 desired state 的写入格式稳定下来，让 watcher、scanner、reconciler 都只依赖同一份本地状态。

## 设计目标

- RPC 入参和落库 desired state 都保持简单，只保存服务名、动作和副本数。
- RoseDB key 必须稳定、可按前缀扫描、可从 key 反解 service name。
- RoseDB desired value 使用 JSON，字段只保留 `service_name`、`action`、`replicas`。
- watcher 只监听 key 变化并入队，scanner 定时扫描 key 并比对 actual state，reconciler 最终执行收敛。
- 删除语义只删除 desired state，不直接调用 ECSM；后续由队列和 reconciler 统一处理。

## RPC 路由

### 更新 desired state
RoseSEDB ----> watcher -> workQueue -> reconciler
            |
Ecsm实际状态 ---> scanner -> reconciler

Route:

```text
/desired_state/update
```

RPC payload:

```json
{
  "service_name": "c_worker@1.1.0",
  "action": "start",
  "replicas": 5
}
```

规范化规则:

```text
service_name 必填，格式必须是 `name@tag`，例如 `c_worker@1.1.0`
name 和 tag 都不能为空
service_name trim 后不能包含 /
action 只允许 start/stop
replicas 必填；start 时应大于 0，stop 时会忽略 replicas
```

落库 key:

```text
desired/services/{service_name}
```

落库 value:

```json
{
  "service_name": "c_worker@1.1.0",
  "action": "start",
  "replicas": 5
}
```

### 查询单个 desired state

Route:

```text
/desired_state/query
```

RPC payload:

```json
{
  "service_name": "c_worker@1.1.0"
}
```

读取 key:

```text
desired/services/c_worker@1.1.0
```

响应 data 直接返回 desired value。

### 查询全部 desired state

Route:

```text
/desired_state/list
```

扫描前缀:

```text
desired/services/
```

返回按 key 字典序扫描到的全部 desired value。调用方不应该依赖顺序；如果需要稳定排序，由服务端按 `service_name` 排序。

### 删除 desired state

Route:

```text
/desired_state/delete
```

RPC payload:

```json
{
  "service_name": "c_worker@1.1.0"
}
```

删除 key:

```text
desired/services/c_worker@1.1.0
```

说明: 删除 desired key 表示 controller 不再维护这个服务的期望状态，不会直接调用 ECSM。

## Status KV

reconciler 每次处理完一个 service 后可以写 status，用于审计和排查。status 不属于期望状态，可保留更多运行时字段。

key:

```text
status/services/{service_name}
```

value:

```json
{
  "api_version": "ecsm.io/v1alpha1",
  "kind": "ServiceStatus",
  "metadata": {
    "service_name": "c_worker@1.1.0",
    "updated_at": "2026-05-14T10:00:05Z"
  },
  "status": {
    "phase": "ready",
    "last_action": "need_create",
    "last_error": ""
  }
}
```

phase 允许值:

```text
pending
reconciling
ready
failed
```

## 工作流 Demo

1. 外部系统调用 `/desired_state/update`。
2. VSOA service 解码 payload，只校验三个 desired 字段。
3. registry 写入 `desired/services/c_worker@1.1.0`。
4. RoseDB watch 触发 desired watcher，watcher 将 `c_worker@1.1.0` 放入 work queue。
5. scanner 每隔固定周期扫描 `desired/services/`，和 ECSM actual state 比对；如果发现 diff，也将 `c_worker@1.1.0` 放入 work queue。
6. worker 从 queue 取出 `c_worker@1.1.0`。
7. reconciler 重新读取 `desired/services/c_worker@1.1.0`，重新采集 actual state，计算 diff。
8. 如果 desired action 是 `start`，reconciler 会先区分服务是否已存在：不存在时产生 `need_create`，已存在但未运行时产生 `need_start`，已运行但副本数不一致时产生扩缩容 diff。
9. ECSM client 执行动作。
10. reconciler 可写入 `status/services/c_worker@1.1.0`。

## Open Questions

- `service_name` 需要直接对应 ECSM 服务名，格式固定为 `name@tag`。
- `node_names`、`vsoa`、`policy` 不进入 desired state，是否全部由 controller 默认值决定。
- `delete desired` 是否需要保留 tombstone，用来记录是谁删除以及删除时间。
