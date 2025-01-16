package parallel

import (
	"context"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	openshiftappsv1 "github.com/openshift/api/apps/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestIdlerAndPriorityClass(t *testing.T) {
	t.Parallel()

	await := WaitForDeployments(t)
	hostAwait := await.Host()
	memberAwait := await.Member1()
	// Provision a user to idle with a short idling timeout
	hostAwait.UpdateToolchainConfig(t, testconfig.AutomaticApproval().Enabled(false))
	NewSignupRequest(await).
		Username("test-idler").
		Email("test-idler@redhat.com").
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait).
		SpaceTier("base"). // let's move it to base to have to namespaces to monitor
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)

	idler, err := memberAwait.WaitForIdler(t, "test-idler-dev", wait.IdlerConditions(wait.Running()))
	require.NoError(t, err)

	// Noise
	idlerNoise, err := memberAwait.WaitForIdler(t, "test-idler-stage", wait.IdlerConditions(wait.Running()))
	require.NoError(t, err)

	// Create payloads for both users
	podsToIdle := prepareWorkloads(t, await.Member1(), idler.Name, wait.WithSandboxPriorityClass())
	podsNoise := prepareWorkloads(t, await.Member1(), idlerNoise.Name, wait.WithSandboxPriorityClass())

	// Create another noise pods in non-user namespace
	memberAwait.CreateNamespace(t, "workloads-noise")
	externalNsPodsNoise := prepareWorkloads(t, await.Member1(), "workloads-noise", wait.WithOriginalPriorityClass())

	// Set a short timeout for one of the idler to trigger pod idling
	// The idler is currently updating its status since it's already been idling the pods. So we need to keep trying to update.
	idler, err = wait.For(t, memberAwait.Awaitility, &toolchainv1alpha1.Idler{}).
		Update(idler.Name, memberAwait.Namespace, func(i *toolchainv1alpha1.Idler) {
			i.Spec.TimeoutSeconds = 5
		})

	require.NoError(t, err)

	// Wait for the pods to be deleted
	for _, p := range podsToIdle {
		err := memberAwait.WaitUntilPodsDeleted(t, p.Namespace, wait.WithPodName(p.Name))
		require.NoError(t, err)
	}
	// check notification was created
	_, err = hostAwait.WaitForNotificationWithName(t, "test-idler-dev-idled", toolchainv1alpha1.NotificationTypeIdled, wait.UntilNotificationHasConditions(wait.Sent()))
	require.NoError(t, err)

	// make sure that "noise" pods are still there, and notification is not created for stage namespace
	_, err = memberAwait.WaitForPods(t, idlerNoise.Name, len(podsNoise), wait.PodRunning(), wait.WithPodLabel("idler", "idler"), wait.WithSandboxPriorityClass())
	require.NoError(t, err)
	_, err = memberAwait.WaitForPods(t, "workloads-noise", len(externalNsPodsNoise), wait.PodRunning(), wait.WithPodLabel("idler", "idler"), wait.WithOriginalPriorityClass())
	require.NoError(t, err)
	err = hostAwait.WaitForNotificationToNotBeCreated(t, "test-idler-stage-idled")
	require.NoError(t, err)

	// Check if notification has been deleted before creating another pod
	err = hostAwait.WaitUntilNotificationWithNameDeleted(t, "test-idler-dev-idled")
	require.NoError(t, err)

	// Create another pod and make sure it's deleted.
	// In the tests above the Idler reconcile was triggered after we changed the Idler resource (to set a short timeout).
	// Now we want to verify that the idler reconcile is triggered without modifying the Idler resource.
	// Notification shouldn't be created again.
	pod := createStandalonePod(t, await.Member1(), idler.Name, "idler-test-pod-2") // create just one standalone pod. No need to create all possible pod controllers which may own pods.
	_, err = memberAwait.WaitForPod(t, idler.Name, "idler-test-pod-2")             // pod was created
	require.NoError(t, err)
	time.Sleep(time.Duration(2*idler.Spec.TimeoutSeconds) * time.Second)
	err = memberAwait.WaitUntilPodDeleted(t, pod.Namespace, pod.Name)
	require.NoError(t, err)
	err = hostAwait.WaitForNotificationToNotBeCreated(t, "test-idler-dev-idled")
	require.NoError(t, err)

	// There should not be any pods left in the namespace
	err = memberAwait.WaitUntilPodsDeleted(t, idler.Name, wait.WithPodLabel("idler", "idler"))
	require.NoError(t, err)
}

func prepareWorkloads(t *testing.T, memberAwait *wait.MemberAwaitility, namespace string, additionalPodCriteria ...wait.PodWaitCriterion) []corev1.Pod {
	createStandalonePod(t, memberAwait, namespace, "idler-test-pod-1")
	d := createDeployment(t, memberAwait, namespace)
	n := 1 + int(*d.Spec.Replicas) // total number of created pods

	rs := createReplicaSet(t, memberAwait, namespace)
	n = n + int(*rs.Spec.Replicas)

	createDaemonSet(t, memberAwait, namespace)
	nodes := &corev1.NodeList{}
	err := memberAwait.Client.List(context.TODO(), nodes, client.MatchingLabels(map[string]string{"node-role.kubernetes.io/worker": ""}))
	require.NoError(t, err)
	n = n + len(nodes.Items) // DaemonSet creates N pods where N is the number of worker nodes in the cluster

	createJob(t, memberAwait, namespace)
	n++

	dc := createDeploymentConfig(t, memberAwait, types.NamespacedName{Namespace: namespace, Name: "idler-test-dc"})
	n = n + int(dc.Spec.Replicas)

	// create another DeploymentConfig that will be paused to test that the idler will unpause it when scaling it down
	dcPaused := createDeploymentConfig(t, memberAwait, types.NamespacedName{Namespace: namespace, Name: "idler-test-dc-paused"})
	n = n + int(dcPaused.Spec.Replicas)

	rc := createReplicationController(t, memberAwait, namespace)
	n = n + int(*rc.Spec.Replicas)

	pods, err := memberAwait.WaitForPods(t, namespace, n, append(additionalPodCriteria, wait.PodRunning(),
		wait.WithPodLabel("idler", "idler"))...)

	podNames := []string{}
	for _, p := range pods {
		podNames = append(podNames, p.Name)
	}
	t.Log("waited for pods: ", podNames)
	require.NoError(t, err)

	// pause the dcPaused DeploymentConfig now that its pods are running
	pauseDeploymentConfig(t, memberAwait, types.NamespacedName{Namespace: dcPaused.Namespace, Name: dcPaused.Name})
	return pods
}

func createDeployment(t *testing.T, memberAwait *wait.MemberAwaitility, namespace string) *appsv1.Deployment {
	// Create a Deployment with two pods
	replicas := int32(3)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-deployment", Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels("idler-deployment")},
			Replicas: &replicas,
			Template: podTemplateSpec("idler-deployment"),
		},
	}
	err := memberAwait.Create(t, deployment)
	require.NoError(t, err)

	return deployment
}

func createReplicaSet(t *testing.T, memberAwait *wait.MemberAwaitility, namespace string) *appsv1.ReplicaSet {
	// Standalone ReplicaSet
	replicas := int32(2)
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-replicaset", Namespace: namespace},
		Spec: appsv1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels("idler-rs")},
			Replicas: &replicas,
			Template: podTemplateSpec("idler-rs"),
		},
	}
	err := memberAwait.Create(t, rs)
	require.NoError(t, err)

	return rs
}

func createDaemonSet(t *testing.T, memberAwait *wait.MemberAwaitility, namespace string) *appsv1.DaemonSet {
	// Standalone ReplicaSet
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-daemonset", Namespace: namespace},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels("idler-ds")},
			Template: podTemplateSpec("idler-ds"),
		},
	}
	err := memberAwait.Create(t, ds)
	require.NoError(t, err)

	return ds
}

func createJob(t *testing.T, memberAwait *wait.MemberAwaitility, namespace string) *batchv1.Job {
	// Job
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-job", Namespace: namespace},
		Spec: batchv1.JobSpec{
			Template: podTemplateSpec(""),
		},
	}
	job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
	err := memberAwait.Create(t, job)
	require.NoError(t, err)

	return job
}

func createDeploymentConfig(t *testing.T, memberAwait *wait.MemberAwaitility, namespacedName types.NamespacedName) *openshiftappsv1.DeploymentConfig {
	// Create a DeploymentConfig with two pods
	spec := podTemplateSpec("idler-dc")
	replicas := int32(2)
	dc := &openshiftappsv1.DeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{Name: namespacedName.Name, Namespace: namespacedName.Namespace},
		Spec: openshiftappsv1.DeploymentConfigSpec{
			Selector: selectorLabels("idler-dc"),
			Replicas: replicas,
			Template: &spec,
		},
	}
	err := memberAwait.Create(t, dc)
	require.NoError(t, err)

	return dc
}

func pauseDeploymentConfig(t *testing.T, memberAwait *wait.MemberAwaitility, namespacedName types.NamespacedName) *openshiftappsv1.DeploymentConfig {
	dc, err := wait.For(t, memberAwait.Awaitility, &openshiftappsv1.DeploymentConfig{}).
		Update(namespacedName.Name, namespacedName.Namespace,
			func(dc *openshiftappsv1.DeploymentConfig) {
				dc.Spec.Paused = true
			})
	require.NoError(t, err)
	return dc
}

func createReplicationController(t *testing.T, memberAwait *wait.MemberAwaitility, namespace string) *corev1.ReplicationController {
	// Standalone ReplicationController
	spec := podTemplateSpec("idler-rc")
	replicas := int32(2)
	rc := &corev1.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-rc", Namespace: namespace},
		Spec: corev1.ReplicationControllerSpec{
			Selector: selectorLabels("idler-rc"),
			Replicas: &replicas,
			Template: &spec,
		},
	}
	err := memberAwait.Create(t, rc)
	require.NoError(t, err)

	return rc
}

func createStandalonePod(t *testing.T, memberAwait *wait.MemberAwaitility, namespace, name string) *corev1.Pod {
	// Create a Deployment with two pods
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    map[string]string{"idler": "idler"},
		},
		Spec: podSpec(),
	}
	pod.Spec.PriorityClassName = "system-cluster-critical"
	err := memberAwait.Create(t, pod)
	require.NoError(t, err)
	return pod
}

func podSpec() corev1.PodSpec {
	zero := int64(0)
	return corev1.PodSpec{
		TerminationGracePeriodSeconds: &zero,
		Containers: []corev1.Container{{
			Name:    "sleep",
			Image:   "quay.io/prometheus/busybox:latest",
			Command: []string{"sleep", "36000"}, // 10 hours
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"cpu":    resource.MustParse("1m"),
					"memory": resource.MustParse("8Mi"),
				},
				Limits: corev1.ResourceList{
					"cpu":    resource.MustParse("50m"),
					"memory": resource.MustParse("80Mi"),
				},
			},
		}},
	}
}

func podTemplateSpec(app string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
			"idler": "idler",
			"app":   app,
		}},
		Spec: podSpec(),
	}
}

func selectorLabels(app string) map[string]string {
	return map[string]string{"app": app}
}
