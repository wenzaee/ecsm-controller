# Desired VSOA Client

`desired-vsoa-client` 是 Desired State VSOA 服务的命令行客户端，用于通过 VSOA RPC 修改、删除、查询 RoseDB 中的服务期望状态。

它不会直接调用 ECSM 的启动、停止、扩缩容接口，只会调用 desired state API。真正的执行应由后续 Reconciler 统一读取最新 desired state 并完成收敛。
```
GOOS=sylixos GOARCH=arm64 go build -o ecsm-controller ./cmd/ecsm-controller
```
## 前置条件

先启动工程入口或 VSOA demo：

```bash
go run ./cmd/ecsm-controller -config configs/demo.yaml
```

编译成可执行文件后：

```bash
./ecsm-controller -config configs/demo.yaml
```

默认配置文件是：

```yaml
registry:
  rosedb:
    dir_path: /tmp/ecsm-controller-demo
    sync: false
    watch_queue_size: 1024

vsoa:
  listen_addr: ":18082"
  password: ""
```

客户端默认读取 `configs/demo.yaml`，也可以通过 `-addr` 和 `-password` 覆盖连接配置。

## 参数

```text
-config          YAML 配置文件路径，默认 configs/demo.yaml
-addr            VSOA 服务地址，优先级高于配置文件
-password        VSOA 密码，优先级高于配置文件
-action          操作类型：healthz、update、query、delete、list
-service         服务名，必须是 name@tag 格式，例如 c_worker@1.1.0
-desired-action  期望动作，仅 update 使用，只允许 start、stop，默认 start
-replicas        期望副本数，仅 update 使用
-payload         原始 JSON 请求体，仅 update 使用；设置后会忽略 update 的其他构造参数
-timeout         请求超时时间，默认 5s
```

## 健康检查

```bash
go run ./cmd/desired-vsoa-client -action healthz
```

编译成可执行文件后：

```bash
./desired-vsoa-client -action healthz
```

## 写入 Desired State

使用命令行参数构造请求：

```bash
go run ./cmd/desired-vsoa-client \
  -action update \
  -service c_worker@1.1.0 \
  -desired-action start \
  -replicas 3
```

编译成可执行文件后：

```bash
./desired-vsoa-client \
  -action update \
  -service c_worker@1.1.0 \
  -desired-action start \
  -replicas 3
```
```bash
./desired-vsoa-client \
  -action update \
  -service c_worker_3@1.1.0 \
  -desired-action start \
  -replicas 3
```
```bash
go run ./cmd/desired-vsoa-client \
  -action update \
  -service c_worker@1.1.0 \
  -desired-action stop \
  -replicas 3
```

编译成可执行文件后：

```bash
./desired-vsoa-client \
  -action update \
  -service c_worker@1.1.0 \
  -desired-action stop \
  -replicas 3
```

```bash
go run ./cmd/desired-vsoa-client \
  -action update \
  -service c_worker@1.0.0 \
  -desired-action start \
  -replicas 2
```

编译成可执行文件后：

```bash
./desired-vsoa-client \
  -action update \
  -service c_worker@1.0.0 \
  -desired-action start \
  -replicas 2
```


等价的 VSOA payload 结构：

```json
{
  "service_name": "c_worker@1.1.0",
  "action": "start",
  "replicas": 3
}
```

也可以直接传原始 JSON：

```bash
go run ./cmd/desired-vsoa-client \
  -action update \
  -payload '{"service_name":"c_worker@1.1.0","action":"start","replicas":3}'
```

编译成可执行文件后：

```bash
./desired-vsoa-client \
  -action update \
  -payload '{"service_name":"c_worker@1.1.0","action":"start","replicas":3}'
```

```bash
go run ./cmd/desired-vsoa-client \
  -action update \
  -payload '{"service_name":"c_worker@1.0.0","action":"start","replicas":3}'
```

编译成可执行文件后：

```bash
./desired-vsoa-client \
  -action update \
  -payload '{"service_name":"c_worker@1.1.0","action":"start","replicas":5}'
```
```bash
./desired-vsoa-client \
  -action update \
  -payload '{"service_name":"c_worker@1.1.0","action":"stop","replicas":5}'
```
```bash
go run ./cmd/desired-vsoa-client \
  -action update \
  -payload '{"service_name":"test_python@latest","action":"start","replicas":3}'
```

编译成可执行文件后：

```bash
./desired-vsoa-client \
  -action update \
  -payload '{"service_name":"test_python@latest","action":"start","replicas":3}'
```

## 查询单个服务
test_python@latest
```bash
go run ./cmd/desired-vsoa-client \
  -action query \
  -service c_worker@1.1.0
```

编译成可执行文件后：

```bash
./desired-vsoa-client \
  -action query \
  -service c_worker@1.1.0
```

## 查询全部服务

```bash
go run ./cmd/desired-vsoa-client -action list
```

编译成可执行文件后：

```bash
./desired-vsoa-client -action list
```

## 删除 Desired State

```bash
go run ./cmd/desired-vsoa-client \
  -action delete \
  -service c_worker@1.1.0
```

编译成可执行文件后：

```bash
./desired-vsoa-client \
  -action delete \
  -service c_worker@1.1.0
```

```bash
go run ./cmd/desired-vsoa-client \
  -action delete \
  -service c_worker_3@1.1.0
```

编译成可执行文件后：

```bash
./desired-vsoa-client \
  -action delete \
  -service c_worker_3@1.1.0
```
test_python@latest

```bash
./desired-vsoa-client \
  -action update \
  -payload '{"service_name":"test_python@latest","action":"start","replicas":4}'
```

## 使用其他地址

```bash
go run ./cmd/desired-vsoa-client \
  -addr 127.0.0.1:18082 \
  -action healthz
```

编译成可执行文件后：

```bash
./desired-vsoa-client \
  -addr 127.0.0.1:18082 \
  -action healthz
```

如果服务端配置了密码：

```bash
go run ./cmd/desired-vsoa-client \
  -addr 127.0.0.1:18082 \
  -password your-password \
  -action list
```

编译成可执行文件后：

```bash
./desired-vsoa-client \
  -addr 127.0.0.1:18082 \
  -password your-password \
  -action list
```

## 常用联调流程

启动服务端：

```bash
go run ./cmd/ecsm-controller -config configs/demo.yaml
```

编译成可执行文件后：

```bash
./ecsm-controller -config configs/demo.yaml
```

另开一个终端写入 desired state：

```bash
go run ./cmd/desired-vsoa-client \
  -action update \
  -service c_worker@1.1.0 \
  -desired-action start \
  -replicas 3
```

编译成可执行文件后：

```bash
./desired-vsoa-client \
  -action update \
  -service c_worker@1.1.0 \
  -desired-action start \
  -replicas 3
```

查询确认：

```bash
go run ./cmd/desired-vsoa-client -action query -service c_worker@1.1.0
go run ./cmd/desired-vsoa-client -action list
```

编译成可执行文件后：

```bash
./desired-vsoa-client -action query -service c_worker@1.1.0
./desired-vsoa-client -action list
```
