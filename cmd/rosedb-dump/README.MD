# RoseDB Dump

`rosedb-dump` 是本项目的 RoseDB KV 调试工具，可以查看、添加、修改、删除指定 key，也可以清空全部或指定前缀下的 key。

注意：RoseDB 同一个目录同一时间只能被一个进程打开。如果 `ecsm-controller` 正在运行并占用同一个 RoseDB 目录，本工具会打开失败。需要先停止 controller，或指定另一个 `-dir`。

## 参数

```text
-config       YAML 配置文件路径，默认 configs/demo.yaml
-dir          RoseDB 数据目录，优先级高于配置文件 registry.rosedb.dir_path
-action       操作：list、get、put、delete、clear，默认 list
-key          get、put、delete 使用的 key
-value        put 使用的 value
-value-file   put 使用的 value 文件路径
-prefix       list、clear 时按 key 前缀过滤
-raw          原样输出 value 字符串，不尝试解析 JSON
-yes          确认 clear 删除操作
```

## 查看所有 KV

```bash
go run ./cmd/rosedb-dump -config configs/demo.yaml
```

编译成可执行文件后：

```bash
./rosedb-dump -config configs/demo.yaml
```

等价于：

```bash
go run ./cmd/rosedb-dump -action list -config configs/demo.yaml
```

编译成可执行文件后：

```bash
./rosedb-dump -action list -config configs/demo.yaml
```

## 按前缀查看

查看所有 desired state：

```bash
go run ./cmd/rosedb-dump \
  -action list \
  -prefix desired/services/
```

编译成可执行文件后：

```bash
./rosedb-dump \
  -action list \
  -prefix desired/services/
```

查看所有 status：

```bash
go run ./cmd/rosedb-dump \
  -action list \
  -prefix status/services/
```

编译成可执行文件后：

```bash
./rosedb-dump \
  -action list \
  -prefix status/services/
```

## 查看指定 Key

```bash
go run ./cmd/rosedb-dump \
  -action get \
  -key desired/services/c_worker@1.1.0
```

编译成可执行文件后：

```bash
./rosedb-dump \
  -action get \
  -key desired/services/c_worker@1.1.0
```

## 添加或修改指定 Key

`put` 是 upsert：key 不存在时创建，存在时覆盖。

```bash
go run ./cmd/rosedb-dump \
  -action put \
  -key desired/services/c_worker@1.0.0 \
  -value '{"service_name":"c_worker@1.0.0","action":"start","replicas":5}'
```

编译成可执行文件后：

```bash
./rosedb-dump \
  -action put \
  -key desired/services/c_worker@1.0.0 \
  -value '{"service_name":"c_worker@1.0.0","action":"start","replicas":5}'
```

也可以从文件读取 value：

```bash
go run ./cmd/rosedb-dump \
  -action put \
  -key desired/services/c_worker@1.1.0 \
  -value-file /tmp/desired.json
```

编译成可执行文件后：

```bash
./rosedb-dump \
  -action put \
  -key desired/services/c_worker@1.1.0 \
  -value-file /tmp/desired.json
```

## 删除指定 Key

```bash
go run ./cmd/rosedb-dump \
  -action delete \
  -key desired/services/c_worker@1.1.0
```

编译成可执行文件后：

```bash
./rosedb-dump \
  -action delete \
  -key desired/services/c_worker@1.1.0
```

## 删除所有 Key

这是危险操作，必须加 `-yes`：

```bash
go run ./cmd/rosedb-dump \
  -action clear \
  -yes
```

编译成可执行文件后：

```bash
./rosedb-dump \
  -action clear \
  -yes
```

## 删除指定前缀下的 Key

只删除 desired state：

```bash
go run ./cmd/rosedb-dump \
  -action clear \
  -prefix desired/services/ \
  -yes
```

编译成可执行文件后：

```bash
./rosedb-dump \
  -action clear \
  -prefix desired/services/ \
  -yes
```

## 输出格式

`list` 输出：

```json
{
  "ok": true,
  "count": 1,
  "data": [
    {
      "key": "desired/services/c_worker@1.1.0",
      "value": {
        "service_name": "c_worker@1.1.0",
        "action": "start",
        "replicas": 5
      }
    }
  ]
}
```

`get` 输出：

```json
{
  "ok": true,
  "key": "desired/services/c_worker@1.1.0",
  "value": {
    "service_name": "c_worker@1.1.0",
    "action": "start",
    "replicas": 5
  }
}
```
