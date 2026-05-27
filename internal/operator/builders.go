// Copyright 2026 Cluster Health Autopilot contributors
// SPDX-License-Identifier: Apache-2.0

// Package operator hosts the pure-function builders that translate a
// ClusterHealthAutopilot spec into the concrete Kubernetes manifests
// the controller reconciles toward.
//
// Phase 1 ships the builders only. The controller-runtime Reconcile
// loop that USES the builders lands in Phase 2 (next PR) — separating
// the two keeps this PR digestible and reviewable as pure functions
// without a manager / envtest dependency.
//
// Convention:
//   - Each builder is a pure func: (cr) -> *appsv1.Deployment etc.
//   - The CR's name + namespace are the only sources of object names.
//   - Defaults are applied centrally (in DefaultedSpec) so each
//     builder can assume validated input.
package operator

import (
	"fmt"

	chav1alpha1 "github.com/Bionic-AI-Solutions/cluster-health-autopilot/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	// FieldManager is the field-manager name the controller uses on
	// server-side-apply patches. Pinned so two replicas of the
	// operator (HA via lease-based leader election) agree on
	// ownership.
	FieldManager = "cha-operator"

	// defaultWatcherDebounce / defaultWatcherResync mirror the
	// existing Helm chart defaults — keeps the operator-managed
	// install behavior-identical to the chart-managed install.
	defaultWatcherDebounce = "10s"
	defaultWatcherResync   = "10m"

	defaultDiagnoseSchedule         = "0 9 * * *"
	defaultDiagnoseBackoffLimit     = 1
	defaultDiagnoseActiveDeadlineS  = int64(120)
	defaultRemediateSchedule        = "*/30 * * * *"
	defaultRemediateBackoffLimit    = 1
	defaultRemediateActiveDeadlineS = int64(120)

	defaultSlackSecretKey = "WEBHOOK_URL"

	// roleLabelKey + roleLabelValue match the chart's labels so
	// existing CronJob/Deployment selectors keep working when an
	// install migrates from Helm to the operator.
	roleLabelKey = "cha.bionicaisolutions.com/role"
)

// ResourceNames are the canonical names the controller manages.
type ResourceNames struct {
	ServiceAccount string
	Watcher        string
	Diagnose       string
	Remediate      string
}

// NamesFor returns the canonical resource names derived from the CR
// name. Pinned so renaming a CR is a forbidden operation (the
// controller refuses).
func NamesFor(cr *chav1alpha1.ClusterHealthAutopilot) ResourceNames {
	return ResourceNames{
		ServiceAccount: cr.Name + "-sa",
		Watcher:        cr.Name + "-watcher",
		Diagnose:       cr.Name + "-diagnose",
		Remediate:      cr.Name + "-remediate",
	}
}

// ServiceAccountNameFor returns the SA the controller wires into
// every workload. Honors the explicit spec.ServiceAccountName
// override.
func ServiceAccountNameFor(cr *chav1alpha1.ClusterHealthAutopilot) string {
	if cr.Spec.ServiceAccountName != "" {
		return cr.Spec.ServiceAccountName
	}
	return NamesFor(cr).ServiceAccount
}

// CommonLabels are stamped on every controller-managed object so the
// chart's label selectors continue to match.
func CommonLabels(cr *chav1alpha1.ClusterHealthAutopilot, role string) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       "cluster-health-autopilot",
		"app.kubernetes.io/instance":   cr.Name,
		"app.kubernetes.io/managed-by": FieldManager,
	}
	if role != "" {
		labels[roleLabelKey] = role
	}
	return labels
}

// imageRef returns the "<repo>:<tag>" reference for the workloads.
// Validation that Tag is non-empty happens in the controller before
// calling any builder.
func imageRef(spec chav1alpha1.ImageSpec) string {
	return fmt.Sprintf("%s:%s", spec.Repository, spec.Tag)
}

// pullSecretRefs converts a list of pull-secret names to the
// corev1.LocalObjectReference shape Kubernetes expects.
func pullSecretRefs(names []string) []corev1.LocalObjectReference {
	if len(names) == 0 {
		return nil
	}
	out := make([]corev1.LocalObjectReference, len(names))
	for i, n := range names {
		out[i] = corev1.LocalObjectReference{Name: n}
	}
	return out
}

// pullPolicy returns the PullPolicy to stamp on the container.
// Honors an explicit spec value; otherwise infers IfNotPresent for
// semver tags and Always for mutable tags (latest / main / dev).
func pullPolicy(spec chav1alpha1.ImageSpec) corev1.PullPolicy {
	if spec.PullPolicy != "" {
		return corev1.PullPolicy(spec.PullPolicy)
	}
	switch spec.Tag {
	case "latest", "main", "dev":
		return corev1.PullAlways
	}
	return corev1.PullIfNotPresent
}

// BuildServiceAccount returns the ServiceAccount the controller owns.
func BuildServiceAccount(cr *chav1alpha1.ClusterHealthAutopilot) *corev1.ServiceAccount {
	names := NamesFor(cr)
	if cr.Spec.ServiceAccountName != "" {
		names.ServiceAccount = cr.Spec.ServiceAccountName
	}
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ServiceAccount"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.ServiceAccount,
			Namespace: cr.Namespace,
			Labels:    CommonLabels(cr, ""),
		},
	}
}

// BuildWatcherDeployment returns the watcher Deployment for the CR,
// or nil when the watcher is disabled.
func BuildWatcherDeployment(cr *chav1alpha1.ClusterHealthAutopilot) *appsv1.Deployment {
	if cr.Spec.Watcher == nil || !cr.Spec.Watcher.Enabled {
		return nil
	}
	names := NamesFor(cr)
	labels := CommonLabels(cr, "watcher")

	replicas := cr.Spec.Watcher.Replicas
	if replicas == 0 {
		replicas = 1
	}
	debounce := cr.Spec.Watcher.Debounce
	if debounce == "" {
		debounce = defaultWatcherDebounce
	}
	resync := cr.Spec.Watcher.ResyncPeriod
	if resync == "" {
		resync = defaultWatcherResync
	}

	args := []string{
		"watch",
		"--debounce=" + debounce,
		"--resync=" + resync,
	}
	args = append(args, alertingArgs(cr.Spec.Alerting)...)

	env := []corev1.EnvVar{
		{
			Name: "MY_POD_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
			},
		},
		{
			Name: "MY_POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.Watcher,
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
				RollingUpdate: &appsv1.RollingUpdateDeployment{
					MaxSurge:       intstrPtr(1),
					MaxUnavailable: intstrPtr(0),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					ServiceAccountName: ServiceAccountNameFor(cr),
					ImagePullSecrets:   pullSecretRefs(cr.Spec.Image.PullSecrets),
					Containers: []corev1.Container{
						{
							Name:            "watcher",
							Image:           imageRef(cr.Spec.Image),
							ImagePullPolicy: pullPolicy(cr.Spec.Image),
							Args:            args,
							Env:             env,
						},
					},
				},
			},
		},
	}
}

// BuildDiagnoseCronJob returns the diagnose CronJob for the CR, or
// nil when diagnose is disabled.
func BuildDiagnoseCronJob(cr *chav1alpha1.ClusterHealthAutopilot) *batchv1.CronJob {
	if cr.Spec.Diagnose == nil || !cr.Spec.Diagnose.Enabled {
		return nil
	}
	return buildCronJobCommon(cr, "diagnose", NamesFor(cr).Diagnose, cr.Spec.Diagnose.Schedule,
		cr.Spec.Diagnose.BackoffLimit, cr.Spec.Diagnose.ActiveDeadlineSeconds,
		[]string{"diagnose"},
		defaultDiagnoseSchedule, defaultDiagnoseBackoffLimit, defaultDiagnoseActiveDeadlineS,
	)
}

// BuildRemediateCronJob returns the remediate CronJob for the CR, or
// nil when remediate is disabled.
func BuildRemediateCronJob(cr *chav1alpha1.ClusterHealthAutopilot) *batchv1.CronJob {
	if cr.Spec.Remediate == nil || !cr.Spec.Remediate.Enabled {
		return nil
	}
	args := []string{"remediate"}
	if cr.Spec.Remediate.DryRun {
		args = append(args, "--dry-run=true")
	}
	return buildCronJobCommon(cr, "remediate", NamesFor(cr).Remediate, cr.Spec.Remediate.Schedule,
		0, 0, args,
		defaultRemediateSchedule, defaultRemediateBackoffLimit, defaultRemediateActiveDeadlineS,
	)
}

// buildCronJobCommon factors the shared shape between diagnose and
// remediate. Each caller supplies its own arg list and defaults so
// the resulting object reflects the role correctly.
func buildCronJobCommon(
	cr *chav1alpha1.ClusterHealthAutopilot,
	role, name, schedule string,
	backoffLimit int32, activeDeadline int64,
	args []string,
	defaultSchedule string, defaultBackoff int32, defaultDeadline int64,
) *batchv1.CronJob {
	if schedule == "" {
		schedule = defaultSchedule
	}
	if backoffLimit == 0 {
		backoffLimit = defaultBackoff
	}
	if activeDeadline == 0 {
		activeDeadline = defaultDeadline
	}
	labels := CommonLabels(cr, role)
	args = append(args, alertingArgs(cr.Spec.Alerting)...)

	successfulJobsHistoryLimit := int32(3)
	failedJobsHistoryLimit := int32(3)

	return &batchv1.CronJob{
		TypeMeta: metav1.TypeMeta{APIVersion: "batch/v1", Kind: "CronJob"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cr.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   schedule,
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			SuccessfulJobsHistoryLimit: &successfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     &failedJobsHistoryLimit,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: batchv1.JobSpec{
					BackoffLimit:          &backoffLimit,
					ActiveDeadlineSeconds: &activeDeadline,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec: corev1.PodSpec{
							ServiceAccountName: ServiceAccountNameFor(cr),
							RestartPolicy:      corev1.RestartPolicyOnFailure,
							ImagePullSecrets:   pullSecretRefs(cr.Spec.Image.PullSecrets),
							Containers: []corev1.Container{
								{
									Name:            role,
									Image:           imageRef(cr.Spec.Image),
									ImagePullPolicy: pullPolicy(cr.Spec.Image),
									Args:            args,
								},
							},
						},
					},
				},
			},
		},
	}
}

// alertingArgs turns the AlertingSpec into CLI flags the existing
// `cha` binary already accepts. Keeping the wire format identical to
// the chart means the chart's tested behavior carries over verbatim.
func alertingArgs(a *chav1alpha1.AlertingSpec) []string {
	if a == nil {
		return nil
	}
	var out []string
	if a.Alertmanager != nil && a.Alertmanager.URL != "" {
		out = append(out,
			"--alertmanager-url="+a.Alertmanager.URL,
			"--cluster-name="+a.Alertmanager.ClusterName,
		)
	}
	if a.Slack != nil {
		if c := a.Slack.Alerts; c != nil && c.SecretName != "" {
			out = append(out, slackChannelFlag("alerts", c))
		}
		if c := a.Slack.Critical; c != nil && c.SecretName != "" {
			out = append(out, slackChannelFlag("critical", c))
		}
		if c := a.Slack.HealthInfo; c != nil && c.SecretName != "" {
			out = append(out, slackChannelFlag("healthinfo", c))
		}
	}
	return out
}

func slackChannelFlag(channel string, c *chav1alpha1.SlackChannelSpec) string {
	key := c.SecretKey
	if key == "" {
		key = defaultSlackSecretKey
	}
	return fmt.Sprintf("--slack-%s-webhook-secret=%s:%s", channel, c.SecretName, key)
}

func intstrPtr(i int) *intstr.IntOrString {
	v := intstr.FromInt(i)
	return &v
}
