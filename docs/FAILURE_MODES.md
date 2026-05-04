# Failure-Mode Catalog

The default catalog covers three auto-fixers and two analyzers. Each is a separate Go function with a published unit-test corpus under [`internal/`](../internal/). The "real example" column is a real incident from the cluster the product was built on.

---

## A. Auto-fixers (whitelist; default-OFF; opt-in via `remediation.enabled=true`)

Each fixer is gated by the **Mutator interface** at the type-system level: `snapshot.File` doesn't implement `Mutator`, so fixers physically cannot run against a captured snapshot. Live mode only. The remediator ClusterRole grants exactly three verbs (`pods/delete`, `jobs/delete`, `deployments/patch`); no Secret, ConfigMap, or CRD writes are possible regardless of how the binary is called.

### 1. `StaleErrorPods`

| | |
|---|---|
| **Symptom** | `kubectl get pods -A` shows `Error` / `Failed` pods sitting around for hours/days. |
| **Root cause class** | Manual `kubectl debug` leftovers, `kubectl run --restart=Never` scratch pods, Job pods that crashed without a retry policy. The pod IS terminal — its presence in listings is just visual noise, but it skews `Pods/cluster` metrics and clutters incident reviews. |
| **What it does** | Filters pods to `status.phase == "Failed"`, owned by a Job or unowned. Skips RS/Deployment/StatefulSet/DaemonSet-owned pods (let the controller recover). Skips protected namespaces. Calls `pods.delete`. |
| **Why it's safe** | Job-owned pods that were going to retry already have a successor; the dead one is a tombstone. Unowned pods don't have a controller to recover them — they're already lost. The only "data loss" is one pod's container logs, which the Job retains via `backoffLimit`. |
| **Real example** | 3× `node-debugger-gpu-*` pods stuck in `Error` for 11–12 days from a node debug session that was never cleaned up. Caught and removed on the first tick of the bash-version predecessor; would be the same outcome with this fixer. |
| **Source** | [`internal/fix/stale_error_pods.go`](../internal/fix/stale_error_pods.go) |

### 2. `StuckJobsWithBadSecretRef`

| | |
|---|---|
| **Symptom** | A pod has been in `CreateContainerConfigError` for days; the parent CronJob is `concurrencyPolicy: Forbid` so the next tick can't run; the cron's effect (Slack alert, backup, scrub) silently stops working. |
| **Root cause class** | Someone updated the CronJob template to reference a Secret key that was renamed (case mismatch, ESO refactor). The CronJob template is correct *now*, but the in-flight Job's pod template is **immutable** — Kubernetes never rewrites it. The Job retries CreateContainer forever. |
| **What it does** | Finds pods in CCE with kubelet event message containing "couldn't find key", confirms the parent is a Job whose owner is a still-existing CronJob, calls `jobs.delete` on the frozen Job. The CronJob's next scheduled tick spawns a fresh Job using the corrected template. Skips Jobs without a CronJob owner — one-off Jobs deserve human attention. |
| **Why it's safe** | Deleting the Job kills the dead pod and lets the cron schedule do its thing. The Job's history is gone, but the Job had been failing anyway — there was no successful run to lose. |
| **Real example** | `gpu-docker-monitor` cron's Slack-webhook Secret got renamed `webhook-url` → `WEBHOOK_URL`. The frozen Job retried for **26 days** before being caught. With this fixer wired, it would be resolved on the next tick after the rename. |
| **Source** | [`internal/fix/stuck_jobs.go`](../internal/fix/stuck_jobs.go) |

### 3. `StuckRSPods`

| | |
|---|---|
| **Symptom** | Deployment shows `1/2 ready`. One pod is in `CreateContainerConfigError`. The "1" pod is from an older ReplicaSet revision; the "0/1" pod is the new revision that can't start. Rolling update is wedged. |
| **Root cause class** | A transient condition that has since cleared (image pull retry, scheduler quirk) prevented the new pod from starting at the moment of the rollout, and the ReplicaSet hasn't retried because `progressDeadlineSeconds` hasn't fired. |
| **What it does** | Detects a CCE pod owned by a ReplicaSet whose `revision` annotation differs from the live Deployment's `revision`. Patches the Deployment template's `kubectl.kubernetes.io/restartedAt` annotation — the same patch `kubectl rollout restart` produces. Kubernetes spawns a new ReplicaSet, the old wedged one ages out. **Refuses** to restart when the failure is "couldn't find key" — rollout would reproduce the same error against the same Secret; that needs human action and is handled by the analyzer below. |
| **Why it's safe** | `kubectl rollout restart` is the standard SRE remedy for stuck rollouts; the patched annotation is the documented mechanism. Kubernetes itself decides which pods get replaced and in what order. |
| **Real example** | `frontend-deploy` revision 7 stuck because revision 6's pod template had a different env layout that took longer than `progressDeadlineSeconds=600` to converge. Manual `kubectl rollout restart` from on-call cleared it; this fixer does the same automatically when the revision diff says it's safe. |
| **Source** | [`internal/fix/stuck_rs_pods.go`](../internal/fix/stuck_rs_pods.go) |

---

## B. Analyzers (read-only; never act; surface diagnostics for human action)

Analyzers run in both `cha diagnose` and `cha remediate`. They produce structured diagnostics that go into the Slack report's `Diagnostics` section, alongside any auto-fixes that were applied.

### 1. `SecretKeyMissing`

| | |
|---|---|
| **Symptom** | `couldn't find key X in Secret ns/name` on pod events. Pod stuck in CCE. The Deployment's env wires a key that doesn't exist on the live Secret. |
| **What it produces** | One Diagnostic line per `(secret_ns, secret_name, missing_key)` tuple, naming all four entities involved: missing key + Secret + consuming Deployment (resolved via `Pod → ReplicaSet → Deployment` owner chain) + owning ExternalSecret (resolved by matching `spec.target.name`). |
| **Why no auto-fix** | The fix is to either (a) edit the ExternalSecret template to expose the key, or (b) edit the Deployment env to reference an existing key. Both are git-side changes; an automated edit would conflict with version control and could fan out to other Deployments referencing the same Secret. |
| **Output (verbatim)** | `🔎 Secret mcp/mcp-openproject-secrets missing key openproject-url (referenced by Deployment/mcp-openproject-server in ns mcp). Owning ExternalSecret: mcp/mcp-openproject-secrets — add data/template entry exposing openproject-url, or remove the env reference if unused.` |
| **Source** | [`internal/diagnose/secret_key_missing.go`](../internal/diagnose/secret_key_missing.go) |

### 2. `FailingExternalSecrets`

| | |
|---|---|
| **Symptom** | An ExternalSecret has `Ready=False`. The Kubernetes Secret it owns still exists with a stale-but-cached value. Running pods don't notice; the next pod restart will. |
| **What it produces** | One Diagnostic line per failing ESO, with the most recent `UpdateFailed` event message — which carries the precise missing Vault property name. Falls back to the condition message when no events are in the snapshot. |
| **Why no auto-fix** | Resolution requires writing to Vault — not a place an automated tool should reach. The diagnostic surfaces the exact path + property name so the operator can `vault kv put` in seconds. |
| **Output (verbatim)** | `🔎 ExternalSecret mail/mail-service-api-key not Ready: error processing spec.data[0] (key: t6-apps/mail/config), err: cannot find secret data for key: "mail_service_api_key". Check Vault path / property names.` |
| **Source** | [`internal/diagnose/failing_externalsecrets.go`](../internal/diagnose/failing_externalsecrets.go) |

---

## How the catalog grows

New entries land via PR. Two design rules:

- **A fixer must be reversible.** `kubectl delete pod` reschedules within seconds; `kubectl rollout restart` is part of every operator's vocabulary. We don't add fixers whose mistake recovery requires more than a minute of human action.
- **A fixer must be idempotent.** Running the same fixer against a cluster that already had it run produces no actions and no errors. Repeated execution is a no-op when nothing matches.

Analyzers have one rule: **the diagnostic must name the resolution.** A line that says "X is broken" without telling the operator *what to do about it* is alert-fatigue fuel. Every analyzer in the catalog produces a hint that ends in the kubectl/vault/git command that fixes it.

## Failure-mode tracking for product evolution

The Verified Signature Library — the commercial wedge per the product's pricing model — grows by codifying recurring incident patterns from real customer fleets. Each new failure mode added to the library:

1. Has a unit test against an anonymized fixture from the original incident
2. Has a documented "what an SRE would do manually" runbook
3. Either ships as an auto-fixer (when reversible + idempotent + safe) OR an analyzer (when human judgment is needed)

The goal is a defensible breadth of failure-mode coverage that is hard for a competitor to match without operating real production fleets — the catalog **is** the moat.
