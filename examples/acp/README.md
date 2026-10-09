# Go 接入 cyber

本目录提供两个可运行的 Go client。请求字段、事件、错误和重连语义统一见[API 参考](../../docs/api.md)，其他语言的代码生成与接入见[第三方语言指南](../../docs/integration.md)。

| 示例 | 功能组 | 用途 |
| --- | --- | --- |
| [client](client/main.go) | Application WebSocket | 创建会话、发送自然语言并消费实时事件 |
| [connectrpc](connectrpc/main.go) | ConnectRPC | 查询会话列表与持久化历史 |

## 准备服务与节点

先[配置模型](../../docs/configuration.md)，再启动 Hub 和扫描节点。示例使用回环地址、测试 access key 和固定 node ID；正式部署请替换 access key，并按[节点指南](../../docs/user/web.md#远程节点)设置地址。

```sh
# 一个终端启动 Hub
cyber-web --addr 127.0.0.1:8080 --token demo

# 另一个终端连接执行节点
cyber-scan agent --server-url http://demo@127.0.0.1:8080 --node-id scan-1
```

Hub 不内嵌执行 Agent。节点上线后，从仓库根目录运行下列示例；使用已有节点时，将 `scan-1` 替换为它在 Hub 中的 node ID。

## 实时对话

```sh
go run ./examples/acp/client --server http://127.0.0.1:8080 --token demo --node scan-1 -p "你好，请介绍一下自己"
```

客户端按 OpenSession → WatchEvents → RunTurn 的顺序发起对话，打印消息增量与工具事件，收到 `turn_ended` 后结束本轮。运行回执不包含回答；完整调用和请求关联实现见[client.go](client/client.go)。

本示例演示最小实时订阅。生产客户端还需保存非空 `delivery_cursor`，断线后以 `after_cursor` 恢复；详见[持久化和断线续传](../../docs/api.md#10-持久化和断线续传)。

## 查询历史

```sh
# 列出会话
go run ./examples/acp/connectrpc --server http://127.0.0.1:8080 --token demo

# 查询指定会话的持久化事件
go run ./examples/acp/connectrpc --server http://127.0.0.1:8080 --token demo --session <session-id>
```

程序使用生成的 ConnectRPC client，并以标准 protobuf JSON 输出响应。ListEvents 不返回瞬时 delta；实时回答由 WebSocket WatchEvents 提供。

## 验证

```sh
go test ./examples/acp/client ./examples/acp/connectrpc
```

测试通过本地服务验证 WebSocket 鉴权、会话、事件与结束信号，以及 ConnectRPC Bearer header、历史查询和 protobuf JSON 输出，不调用真实模型。实际对话需要单独运行以上服务与节点。
