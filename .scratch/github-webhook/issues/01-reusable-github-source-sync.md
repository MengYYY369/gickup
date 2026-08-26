# 01 — 提取可复用的 GitHub source 同步入口

**What to build:** 提供一个可被现有备份流程和未来 Webhook worker 共用的 GitHub source 同步入口，在不改变当前定时/一次性备份行为的前提下，使单个 GitHub source 的发现与同步能够独立触发。

**Blocked by:** None — can start immediately

**Status:** resolved

- [x] 现有 GitHub 备份流程改为通过可复用入口执行，现有行为保持不变
- [x] 可独立触发指定配置中的 GitHub source 同步，而不会触发其他来源
- [x] 相关自动化测试通过
