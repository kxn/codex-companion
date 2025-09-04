# 测试方案

依据 `DESIGN.md` 描述的功能，为各内部包设计单元测试，尽量覆盖主要逻辑。

## internal/account
- **添加与获取**：`AddAPIKey`、`AddChatGPT` 后通过 `Get` 验证数据写入。
- **列表排序**：`List` 应按 `Priority` 升序返回。
- **更新与删除**：`Update` 修改字段；`Delete` 删除后 `Get` 返回 `nil`。
- **耗尽与恢复**：`MarkExhausted` 设置 `Exhausted` 与 `ResetAt`；`Reactivate` 取消耗尽。

## internal/auth
- **ExchangeRefreshToken 成功**：模拟令牌端点返回 token 与 expires_in。
- **ExchangeRefreshToken 失败**：非 200 状态返回错误。
- **Refresh 更新**：`TokenExpiresAt` 过期时刷新并写回数据库。
- **Refresh 无需刷新**：未过期或 API key 账号不应发起网络请求。

## internal/log
- **Insert/List**：插入多条日志后按 id 降序返回，并验证 Header 与 Body 反序列化。
- **限制数量**：`List` 的限制参数应只返回指定条数。

## internal/scheduler
- **Next 选择**：返回优先级最高且未耗尽的账号。
- **跳过耗尽账号**：`MarkExhausted` 后 `Next` 应跳过。
- **刷新失败回退**：ChatGPT 账号刷新失败时使用后备账号。
- **reactivate**：当 `ResetAt` 已过期时，`reactivate` 重新激活账号。
- **StartReactivator**：调用 `StartReactivator` 后后台任务会自动激活到期账号。

## internal/proxy
- **转发授权头与日志**：`ServeHTTP` 将请求转发到上游并记录日志。
- **429 耗尽处理**：上游返回 429 时将账号标记为耗尽并返回 503。
- **失败回退**：首个账号返回 429 时应切换到下一个账号并重试成功。
- **ChatGPT 账号授权**：ChatGPT 账号应使用 `AccessToken` 作为 `Authorization` 头。

## internal/webui
- **ImportAuth**：读取 `CODEX_HOME/auth.json` 并创建 ChatGPT 账号；缺失文件时报错。
- **Accounts API**：`GET/POST/PUT/DELETE /api/accounts` 实现账号的增删改查。
- **Logs API**：`GET /api/logs` 返回持久化的请求日志。

## 运行方式
执行 `go test ./...` 运行全部单元测试。

