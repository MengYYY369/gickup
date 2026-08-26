# 05 — 完成 Webhook 重载、可观测性与回归验证

**What to build:** 完成 Webhook 的生产运行闭环，使配置变更能够安全生效，提供健康检查、必要日志和 Prometheus 指标，并通过端到端回归验证确认 Webhook、cron 与既有备份行为能够可靠共存。

**Blocked by:** 04 — 接入 Webhook 服务生命周期

**Status:** resolved

- [x] 配置重载能够安全更新 Webhook 配置且不会遗留旧服务状态
- [x] 提供健康检查以及足以诊断验签、入队和同步结果的日志
- [x] 提供 Webhook 请求、队列和同步结果的基础 Prometheus 指标
- [x] 端到端及回归测试覆盖 Webhook 独立运行、与 cron 共存和配置重载
