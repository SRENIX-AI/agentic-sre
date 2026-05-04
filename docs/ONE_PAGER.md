# Cluster Health Autopilot — One-Page Brief

*Self-healing operations for Kubernetes — detect, fix, re-verify, report.*

---

## The problem

Every Kubernetes cluster accumulates a tail of silently-degraded state — a Secret-key rename nobody noticed until the next pod eviction, an ExternalSecret quietly failing to sync for weeks, a CronJob that locked itself out a month ago. Existing dashboards (Datadog, Grafana, Prometheus alerts) detect *metric* moves but are blind to this *configuration drift* class across the Vault → ExternalSecret → Deployment chain.

## The wedge

**Zero-trust diagnose, no install, no RBAC, no write access.**

```sh
cha diagnose --snapshot ./your-kubectl-export.json
```

Names the exact Secret + missing key + consuming Deployment + owning ExternalSecret in one line. Run it on your laptop in 30 seconds. Nothing leaves your machine.

## The product

A single Helm chart. Seven Kubernetes objects. Two CronJobs:

- **diagnose** — read-only, daily, posts to Slack. Always enabled.
- **remediate** — opt-in, runs whitelisted auto-fixers, also posts to Slack.

A 16 MB distroless container image. Runs nonroot. Negligible footprint (<100 m CPU request, <100 MB RAM). No long-running pod, no Service, no inbound traffic.

| What it sees | What it does |
|---|---|
| Ceph health (CephCluster CRD) | **Auto-fix (whitelisted, audited):** |
| CloudNativePG / Spilo (Patroni) Postgres | • delete stale `Error`/`Failed` pods |
| Nodes, PVCs | • delete frozen CronJob Jobs |
| ~30 critical Deployments (configurable) | • rollout-restart wedged ReplicaSets |
| Failing ExternalSecrets | **Diagnose (never acts):** |
| Pods stuck in CCE on missing Secret keys | • Secret-key drift across the chain |
|  | • ESO sync errors with Vault hint |

## RBAC discipline

Read role and Write role are **separate** ClusterRoles. The Write role grants exactly:
`pods/delete`, `jobs/delete`, `deployments/patch`. Never Secret/ConfigMap/CRD writes.

Six platform namespaces are always skipped — `kube-system`, `kube-public`, `kube-node-lease`, `rook-ceph`, `vault`, `external-secrets`, `cnpg-system`. Enforced both **in code** AND **by RBAC**. The fix list is the source code; an SRE can audit every action the tool will ever take in one afternoon.

## Zero AI in the hot path

Every probe, analyzer, and fixer is deterministic Go. Same input → same diagnosis, every time, auditably. No LLM call on cluster data. No customer state leaves the cluster. (See [docs/AI_USAGE.md](AI_USAGE.md) for the full position on where AI does and doesn't enter the product.)

## Pricing (open-core)

| Component | License | Tier |
|---|---|---|
| CLI engine + default catalog + Helm chart | Apache 2.0 | Free / OSS |
| **Verified Signature Library** (curated, regression-tested patterns added monthly) | Commercial | Paid (per-cluster sub) |
| **Hosted Fleet Console** (multi-cluster aggregation, history, SLO dashboards) | Commercial | SaaS |
| **SOC 2 / private deployment / SLA support** | Commercial | Enterprise |

## Real-world validation

Built on a 6-node production GPU/AI cluster running 30+ services. The default catalog was derived from incidents that were already in flight when the project started — including a SIP server stuck 33 days, a CronJob frozen 26 days, and an ExternalSecret silently failing for 6 days. All three are now caught on the next scheduled tick.

## Want the demo?

The 30-second snapshot demo + curated example fixture lives in [`examples/sample-cluster/`](../examples/sample-cluster/). One git clone + one command shows three live diagnostics on a hand-crafted snapshot — without a Kubernetes cluster on your laptop.

For the deeper version of what each fixer does and the failure modes it catches, see [docs/FAILURE_MODES.md](FAILURE_MODES.md).
