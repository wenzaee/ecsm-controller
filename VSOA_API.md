# VSOA API

本文档整理 `pkg/vsoa` 暴露的 desired state VSOA RPC 路径、请求参数和响应参数。

## 通用约定

- 请求 payload 使用 JSON，服务端优先读取 VSOA `Param`，为空时读取 `Data`。
- 响应 payload 使用 JSON 写入 VSOA `Param`，`Data` 置空。
- `action` 只允许 `start`、`stop`。
- `service_name` 不能为空，且不能包含路径分隔符 `/`。
- `replicas` 不能小于 `0`；更新接口中该字段必须传入。

## 路径总览
ip:host为192.168.50.82:13447
| 路径 | RPC Method | 说明 |
| --- | --- | --- |
| `/desired_state/healthz` | GET | 健康检查 |
| `/desired_state/update` | SET | 写入或更新服务 desired state |
| `/desired_state/delete` | SET | 删除服务 desired state |
| `/desired_state/query` | GET | 查询单个服务 desired state |
| `/desired_state/list` | GET | 查询全部 desired state |

## `/desired_state/healthz`

RPC Method: `GET`

请求参数: 无。

响应参数:

```json
{
  "ok": true,
  "message": "desired state vsoa 服务运行正常"
}
```

字段说明:

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `ok` | boolean | 请求是否成功 |
| `message` | string | 成功提示，失败时可为空 |
| `error` | string | 错误信息，仅失败时返回 |

## `/desired_state/update`

RPC Method: `SET`

请求参数:

```json
{
  "service_name": "c_worker@1.1.0",
  "action": "start",
  "replicas": 1
}
```

字段说明:

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `service_name` | string | 是 | 服务名 |
| `action` | string | 是 | 期望动作，只允许 `start`、`stop` |
| `replicas` | integer | 是 | 期望副本数，不能小于 `0` |

响应参数:

```json
{
  "ok": true,
  "message": "desired state 修改成功"
}
```

失败响应:

```json
{
  "ok": false,
  "error": "replicas is required"
}
```

## `/desired_state/delete`

RPC Method: `SET`

请求参数:

```json
{
  "service_name": "c_worker@1.1.0"
}
```

字段说明:

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `service_name` | string | 是 | 要删除 desired state 的服务名 |

响应参数:

```json
{
  "ok": true,
  "message": "desired state 删除成功"
}
```

失败响应:

```json
{
  "ok": false,
  "message": "desired state 不存在",
  "error": "registry: not found"
}
```

## `/desired_state/query`

RPC Method: `GET`

请求参数:

```json
{
  "service_name": "c_worker@1.1.0"
}
```

字段说明:

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `service_name` | string | 是 | 要查询 desired state 的服务名 |

响应参数:

```json
{
  "ok": true,
  "message": "desired state 查询成功",
  "data": {
    "service_name": "c_worker@1.1.0",
    "action": "start",
    "replicas": 1
  }
}
```

字段说明:

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `ok` | boolean | 请求是否成功 |
| `message` | string | 成功提示；未找到时为 `desired state 不存在` |
| `data` | object | 单个 desired state，失败时省略 |
| `data.service_name` | string | 服务名 |
| `data.action` | string | 期望动作，取值为 `start` 或 `stop` |
| `data.replicas` | integer | 期望副本数 |
| `error` | string | 错误信息，仅失败时返回 |

失败响应:

```json
{
  "ok": false,
  "message": "desired state 不存在",
  "error": "registry: not found"
}
```

## `/desired_state/list`

RPC Method: `GET`

请求参数: 无。

响应参数:

```json
{
  "ok": true,
  "message": "desired state 列表查询成功",
  "total": 1,
  "data": [
    {
      "service_name": "c_worker@1.1.0",
      "action": "start",
      "replicas": 1
    }
  ]
}
```

字段说明:

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `ok` | boolean | 请求是否成功 |
| `message` | string | 成功提示，失败时可为空 |
| `total` | integer | 返回的 desired state 数量 |
| `data` | array | desired state 列表，失败或无数据时可为空 |
| `data[].service_name` | string | 服务名 |
| `data[].action` | string | 期望动作，取值为 `start` 或 `stop` |
| `data[].replicas` | integer | 期望副本数 |
| `error` | string | 错误信息，仅失败时返回 |

失败响应:

```json
{
  "ok": false,
  "total": 0,
  "error": "error message"
}
```
