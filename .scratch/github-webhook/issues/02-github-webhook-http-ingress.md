# 02 — 实现 GitHub Webhook HTTP 接入

**What to build:** 提供完整的 GitHub Webhook HTTP 接入路径，对请求进行 HMAC SHA-256 验签，只接受支持的 push 事件，识别与仓库对应的已配置 GitHub source，并把合法触发请求交给同步队列。

**Blocked by:** 01 — 提取可复用的 GitHub source 同步入口

**Status:** resolved

- [x] 正确签名的 GitHub push 能匹配配置的 GitHub source 并进入队列
- [x] 缺失或错误签名、无效请求体、超大请求、错误方法和无法匹配的 source 返回明确的 HTTP 状态
- [x] GitHub.com 与已配置的 GitHub Enterprise source 都能正确匹配
- [x] 非 push 事件不会触发同步
