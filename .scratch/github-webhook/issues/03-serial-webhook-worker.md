# 03 — 实现有界 FIFO Webhook 同步 worker

**What to build:** 让每一个通过验证并成功入队的 push 事件按照 FIFO 顺序由单 worker 串行处理，每次触发对应 GitHub source 的全量同步，不合并合法事件，也不会因单次同步失败而停止后续任务。

**Blocked by:** 01 — 提取可复用的 GitHub source 同步入口；02 — 实现 GitHub Webhook HTTP 接入

**Status:** resolved

- [x] 合法 push 按入队顺序逐个执行且不会合并
- [x] worker 串行触发对应 GitHub source 的全量同步
- [x] 队列达到容量上限时 HTTP 请求快速失败而不是无限阻塞
- [x] 单个同步任务失败后 worker 仍继续消费后续任务
