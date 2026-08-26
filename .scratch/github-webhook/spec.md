Status: ready-for-agent

## Problem Statement

Gickup currently relies on one-shot execution or cron scheduling to discover and synchronize repositories. Users who want backups to begin immediately after a GitHub repository changes must either wait for the next cron run or trigger Gickup externally. The project needs native GitHub push Webhook support that can trigger synchronization while preserving the existing GitHub discovery and backup behavior.

## Solution

Add an optional GitHub Webhook service. When enabled, Gickup remains running, accepts GitHub push Webhooks, verifies each request with HMAC SHA-256, identifies the configured GitHub source that owns the repository, and enqueues a synchronization of that source. A single worker consumes the bounded FIFO queue sequentially and reuses the existing GitHub repository discovery and backup pipeline. Cron scheduling and Webhook processing can operate together.

## User Stories

1. As a Gickup operator, I want a GitHub push Webhook to trigger synchronization, so that backups can begin shortly after repositories change.
2. As a Gickup operator, I want Webhook mode to keep Gickup running even without cron, so that event-driven synchronization works independently.
3. As a Gickup operator, I want Webhook and cron scheduling to work together, so that I can combine immediate and periodic synchronization.
4. As a Gickup operator, I want Webhook support to be optional, so that existing configurations retain their current behavior.
5. As a security-conscious operator, I want every accepted push request authenticated with `X-Hub-Signature-256`, so that unauthenticated callers cannot trigger backups.
6. As a security-conscious operator, I want Webhook startup rejected when the configured secret is empty, so that the endpoint cannot accidentally run without authentication.
7. As a Gickup operator, I want only GitHub push events to enqueue synchronization, so that unrelated events do not cause backups.
8. As GitHub, I want ping events to receive a successful no-content response, so that Webhook configuration can be validated without starting synchronization.
9. As a Gickup operator, I want each valid push delivery queued independently, so that accepted triggers are neither merged nor silently discarded.
10. As a Gickup operator, I want queued jobs processed FIFO by one worker, so that synchronization does not unexpectedly run concurrently.
11. As a Gickup operator, I want the queue to have a bounded capacity, so that an unbounded stream of events cannot consume unlimited memory.
12. As GitHub, I want an explicit unavailable response when the queue is full, so that rejected deliveries can be observed and retried according to operational policy.
13. As a Gickup operator, I want a push to trigger the matching GitHub source rather than every configured source type, so that unrelated Gitea, GitLab, Bitbucket, and other backups are not started.
14. As a Gickup operator, I want a trigger to synchronize all repositories belonging to the matched GitHub source, so that Webhook behavior reuses the source's configured discovery semantics.
15. As a Gickup operator with multiple configurations, I want the incoming repository to resolve to the appropriate GitHub source and configuration, so that only the intended backup scope runs.
16. As a GitHub Enterprise operator, I want Webhooks from configured GitHub Enterprise instances supported, so that the feature is not limited to github.com.
17. As a Gickup operator, I want repository and instance matching to normalize host and URL details safely, so that equivalent configured URLs resolve consistently.
18. As a Gickup operator, I want ambiguous source matches rejected rather than guessed, so that a delivery cannot silently trigger the wrong backup scope.
19. As a security-conscious operator, I want request bodies limited to a fixed maximum size of 1 MiB, so that the endpoint has a predictable memory boundary.
20. As GitHub, I want malformed JSON rejected, so that invalid payloads never reach the synchronization queue.
21. As an HTTP client, I want unsupported methods rejected, so that the Webhook contract is explicit.
22. As a Gickup operator, I want an unmatched repository or GitHub instance reported as not found, so that configuration mistakes are observable.
23. As a Gickup operator, I want failed synchronization jobs not to stop the worker, so that later accepted Webhooks can still be processed.
24. As a Gickup operator, I want Webhook requests and queue processing logged, so that deliveries and synchronization outcomes can be diagnosed.
25. As a Gickup operator, I want basic Prometheus metrics for Webhook activity and queue behavior, so that the service can be monitored.
26. As a Gickup operator, I want a health endpoint, so that an orchestrator or monitoring system can determine whether the HTTP service is alive.
27. As an existing Gickup user, I want the normal GitHub backup path to continue using the same discovery and backup behavior, so that enabling the new feature does not introduce a second implementation of synchronization.
28. As a Gickup operator, I want configuration reloads to handle Webhook lifecycle safely, so that old listeners and queued work are not abandoned unpredictably.

## Implementation Decisions

- Introduce a dedicated Webhook module responsible for HTTP handling, signature verification, event recognition, source matching, queue admission, health handling, logging, and Webhook metrics.
- Keep GitHub synchronization behind a shared GitHub-specific orchestration seam. Both the existing backup flow and the Webhook worker use this seam, which performs GitHub repository discovery and then invokes the existing backup capability.
- Do not invoke the all-source backup orchestration from a Webhook job because a GitHub push must not trigger unrelated source providers.
- A successful push identifies one configured GitHub source and enqueues a snapshot of the configuration needed to synchronize that source.
- Synchronization scope is the entire matched GitHub source, not just the repository named in the payload.
- Support both github.com and configured GitHub Enterprise instances.
- Reject ambiguous matches rather than selecting one implicitly.
- Require `X-Hub-Signature-256` for push requests. Compute HMAC SHA-256 over the original raw request body and use constant-time comparison. Missing or invalid signatures return HTTP 401.
- Limit the request body to 1 MiB. Oversized bodies return HTTP 413.
- Accept HTTP POST for the GitHub Webhook endpoint. Unsupported methods return HTTP 405.
- A valid GitHub `push` that is admitted to the queue returns HTTP 202.
- A GitHub `ping` event returns HTTP 204 and does not enqueue synchronization.
- Malformed payloads return HTTP 400.
- A payload that cannot be mapped to a configured GitHub source returns HTTP 404.
- A full queue returns HTTP 503 rather than blocking the request handler.
- Use one bounded in-memory FIFO queue and one worker. Accepted deliveries are not deduplicated, coalesced, or dropped. Multiple pushes for the same source therefore remain distinct jobs.
- Worker failures are isolated to the current job; one failed synchronization must not terminate processing of subsequent jobs.
- Webhook configuration includes, at minimum, an enablement mechanism, listening address, endpoint path, shared secret, and bounded queue capacity. Defaults should be conservative and preserve behavior when Webhooks are not configured.
- A configured empty Webhook secret is invalid and prevents the Webhook service from starting.
- Add a lightweight health endpoint that does not trigger synchronization.
- Keep cron scheduling operational when Webhooks are enabled. Either cron or the Webhook service is sufficient to make Gickup a long-running process; when neither long-running facility is active, existing one-shot behavior remains.
- Integrate Webhook lifecycle into configuration reload semantics. The first implementation should favor deterministic behavior: stop accepting new Webhook requests, gracefully finish already accepted queue work, and then recreate the service from the new configuration rather than introducing an elaborate live mutation model.
- Add basic Prometheus measurements for accepted/rejected deliveries, queue state, and synchronization outcomes without changing existing repository-discovery metrics semantics.
- Keep the feature focused on GitHub; other source providers remain unchanged.

## Testing Decisions

- Prefer the highest externally observable seam: exercise the Webhook HTTP handler/service with test requests and observe HTTP responses plus queued synchronization calls through a controllable synchronization seam. Do not test private helper implementation details when behavior can be verified through this boundary.
- Keep the number of new seams small. The primary new seam is the GitHub-source synchronization entry point shared by normal orchestration and the Webhook worker.
- Follow the project's existing Go test style for GitHub, configuration/type behavior, and top-level orchestration when adding coverage.
- Verify a correctly signed SHA-256 push is accepted and enqueued.
- Verify missing, incorrect, and body-mismatched signatures return HTTP 401 and enqueue nothing.
- Verify an empty secret is rejected during Webhook service configuration/startup validation.
- Verify push, ping, unsupported event, unsupported method, malformed JSON, oversized body, unmatched source, ambiguous source, and full-queue behavior through observable HTTP results.
- Verify github.com and GitHub Enterprise source matching, including relevant URL normalization and case handling.
- Verify multiple configurations and multiple GitHub sources select exactly one intended source.
- Verify the worker consumes jobs FIFO and serially.
- Verify repeated pushes for the same source remain separate jobs rather than being coalesced.
- Verify queue saturation does not block the HTTP handler and produces HTTP 503.
- Verify a failed or panicking synchronization job cannot permanently stop processing later jobs.
- Verify a Webhook-triggered synchronization calls GitHub discovery and the existing backup capability for the matched source only.
- Verify cron and Webhook modes can coexist.
- Verify Webhook-only configuration keeps the application running, while configurations with neither cron nor Webhook retain existing one-shot lifecycle behavior.
- Verify service/configuration reload behavior so accepted jobs have deterministic completion semantics and the replaced listener does not remain active.
- Existing tests around GitHub discovery, configuration parsing, and top-level backup lifecycle are prior art and should continue to pass unchanged unless behavior explicitly covered by this specification requires an update.

## Out of Scope

- Synchronizing only the repository named in a push payload.
- Parallel or multi-worker Webhook synchronization.
- Persistent queues or recovery of queued jobs across process restarts.
- Delivery ID persistence or long-term deduplication.
- Coalescing repeated push events.
- Automatically registering or updating Webhooks through the GitHub API.
- GitLab, Gitea, Bitbucket, or other providers' Webhook support.
- A Webhook management UI.
- A Webhook replay API.

## Further Notes

The deliberately conservative first version favors reliable semantics and reuse of the existing GitHub backup pipeline over optimization. Because every accepted push results in a separate full synchronization of its matched GitHub source, bursts can cause repeated work. Queue, latency, and synchronization metrics should provide evidence for deciding whether a later version needs event coalescing or narrower repository-level synchronization.

The intended high-level flow is: receive request, verify raw-body signature, recognize the GitHub event, resolve exactly one configured GitHub source, enqueue a source-scoped configuration snapshot, process jobs serially, discover repositories with the existing GitHub provider, and execute the existing backup capability.
