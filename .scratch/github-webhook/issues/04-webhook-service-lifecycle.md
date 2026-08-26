# 04 — 接入 Webhook 服务生命周期

**What to build:** 将 Webhook 服务接入应用生命周期，使只配置 Webhook 时应用也能保持常驻，同时允许 Webhook 与 cron 同时工作，并保持既有一次性备份模式的语义。

**Blocked by:** 02 — 实现 GitHub Webhook HTTP 接入；03 — 实现有界 FIFO Webhook 同步 worker

**Status:** resolved

- [x] 仅启用 Webhook 时进程持续提供服务
- [x] Webhook 与 cron 可以同时启用且互不禁用
- [x] 两种常驻能力均未启用时仍保持既有一次性备份行为
- [x] 服务关闭时停止接受新 Webhook 并安全结束生命周期
