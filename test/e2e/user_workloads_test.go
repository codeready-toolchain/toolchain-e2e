package e2e

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
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestUserWorkloads(t *testing.T) {
	suite.Run(t, &userWorkloadsTestSuite{})
}

type userWorkloadsTestSuite struct {
	suite.Suite
	wait.Awaitilities
}

func (s *userWorkloadsTestSuite) SetupSuite() {
	s.Awaitilities = WaitForDeployments(s.T())
}

func (s *userWorkloadsTestSuite) TestIdlerAndPriorityClass() {
	hostAwait := s.Host()
	memberAwait := s.Member1()
	// Provision a user to idle with a short idling timeout
	hostAwait.UpdateToolchainConfig(s.T(), testconfig.AutomaticApproval().Enabled(false))
	NewSignupRequest(s.Awaitilities).
		Username("test-idler").
		Email("test-idler@redhat.com").
		ManuallyApprove().
		EnsureMUR().
		TargetCluster(memberAwait.ClusterName).
		RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
		Execute(s.T())

	idler, err := memberAwait.WaitForIdler(s.T(), "test-idler-dev", wait.IdlerConditions(Running()))
	require.NoError(s.T(), err)

	// Noise
	idlerNoise, err := memberAwait.WaitForIdler(s.T(), "test-idler-stage", wait.IdlerConditions(Running()))
	require.NoError(s.T(), err)

	// Create payloads for both users
	podsToIdle := s.prepareWorkloads(idler.Name, wait.WithSandboxPriorityClass())
	podsNoise := s.prepareWorkloads(idlerNoise.Name, wait.WithSandboxPriorityClass())

	// Create another noise pods in non-user namespace
	memberAwait.CreateNamespace(s.T(), "workloads-noise")
	externalNsPodsNoise := s.prepareWorkloads("workloads-noise", wait.WithOriginalPriorityClass())

	// Set a short timeout for one of the idler to trigger pod idling
	idler.Spec.TimeoutSeconds = 5
	idler, err = memberAwait.UpdateIdlerSpec(s.T(), idler) // The idler is currently updating its status since it's already been idling the pods. So we need to keep trying to update.
	require.NoError(s.T(), err)

	// Wait for the pods to be deleted
	for _, p := range podsToIdle {
		memberAwait.WaitUntilPodsDeleted(s.T(), p.Namespace, wait.WithPodName(p.Name))
	}
	// check notification was created
	hostAwait.WaitForNotificationWithName(s.T(), "test-idler-dev-idled", toolchainv1alpha1.NotificationTypeIdled, wait.UntilNotificationHasConditions(Sent()))

	// make sure that "noise" pods are still there, and notification is not created for stage namespace
	memberAwait.WaitForPods(s.T(), idlerNoise.Name, len(podsNoise), wait.PodRunning(), wait.WithPodLabel("idler", "idler"), wait.WithSandboxPriorityClass())
	memberAwait.WaitForPods(s.T(), "workloads-noise", len(externalNsPodsNoise), wait.PodRunning(), wait.WithPodLabel("idler", "idler"), wait.WithOriginalPriorityClass())
	require.NoError(s.T(), err)
	hostAwait.WithRetryOptions(wait.TimeoutOption(10*time.Second)).WaitForNotificationWithName(s.T(), "test-idler-stage-idled", toolchainv1alpha1.NotificationTypeIdled, wait.UntilNotificationHasConditions(Sent()))

	// Check if notification has been deleted before creating another pod
	hostAwait.WaitUntilNotificationWithNameDeleted(s.T(), "test-idler-dev-idled")

	// Create another pod and make sure it's deleted.
	// In the tests above the Idler reconcile was triggered after we changed the Idler resource (to set a short timeout).
	// Now we want to verify that the idler reconcile is triggered without modifying the Idler resource.
	// Notification shouldn't be created again.
	pod := s.createStandalonePod(idler.Name, "idler-test-pod-2")  // create just one standalone pod. No need to create all possible pod controllers which may own pods.
	memberAwait.WaitForPod(s.T(), idler.Name, "idler-test-pod-2") // pod was created
	require.NoError(s.T(), err)
	time.Sleep(time.Duration(2*idler.Spec.TimeoutSeconds) * time.Second)
	memberAwait.WaitUntilPodDeleted(s.T(), pod.Namespace, pod.Name)
	hostAwait.WithRetryOptions(wait.TimeoutOption(10*time.Second)).WaitForNotificationWithName(s.T(), "test-idler-dev-idled", toolchainv1alpha1.NotificationTypeIdled, wait.UntilNotificationHasConditions(Sent()))

	// There should not be any pods left in the namespace
	memberAwait.WaitUntilPodsDeleted(s.T(), idler.Name, wait.WithPodLabel("idler", "idler"))
}

func (s *userWorkloadsTestSuite) prepareWorkloads(namespace string, additionalPodCriteria ...wait.PodWaitCriterion) []corev1.Pod {
	memberAwait := s.Member1()
	s.createStandalonePod(namespace, "idler-test-pod-1")
	d := s.createDeployment(namespace)
	n := 1 + int(*d.Spec.Replicas) // total number of created pods

	rs := s.createReplicaSet(namespace)
	n = n + int(*rs.Spec.Replicas)

	s.createDaemonSet(namespace)
	nodes := &corev1.NodeList{}
	err := memberAwait.Client.List(context.TODO(), nodes, client.MatchingLabels(map[string]string{"node-role.kubernetes.io/worker": ""}))
	require.NoError(s.T(), err)
	n = n + len(nodes.Items) // DaemonSet creates N pods where N is the number of worker nodes in the cluster

	s.createJob(namespace)
	n++

	dc := s.createDeploymentConfig(namespace)
	n = n + int(dc.Spec.Replicas)

	rc := s.createReplicationController(namespace)
	n = n + int(*rc.Spec.Replicas)

	pods := memberAwait.WaitForPods(s.T(), namespace, n, append(additionalPodCriteria, wait.PodRunning(),
		wait.WithPodLabel("idler", "idler"))...)
	return pods
}

func (s *userWorkloadsTestSuite) createDeployment(namespace string) *appsv1.Deployment {
	memberAwait := s.Member1()
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
	memberAwait.Create(s.T(), deployment)
	return deployment
}

func (s *userWorkloadsTestSuite) createReplicaSet(namespace string) *appsv1.ReplicaSet {
	memberAwait := s.Member1()
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
	memberAwait.Create(s.T(), rs)
	return rs
}

func (s *userWorkloadsTestSuite) createDaemonSet(namespace string) *appsv1.DaemonSet {
	memberAwait := s.Member1()
	// Standalone ReplicaSet
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-daemonset", Namespace: namespace},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels("idler-ds")},
			Template: podTemplateSpec("idler-ds"),
		},
	}
	memberAwait.Create(s.T(), ds)
	return ds
}

func (s *userWorkloadsTestSuite) createJob(namespace string) *batchv1.Job {
	memberAwait := s.Member1()
	// Job
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-job", Namespace: namespace},
		Spec: batchv1.JobSpec{
			Template: podTemplateSpec(""),
		},
	}
	job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
	memberAwait.Create(s.T(), job)
	return job
}

func (s *userWorkloadsTestSuite) createDeploymentConfig(namespace string) *openshiftappsv1.DeploymentConfig {
	memberAwait := s.Member1()
	// Create a Deployment with two pods
	spec := podTemplateSpec("idler-dc")
	replicas := int32(2)
	dc := &openshiftappsv1.DeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-dc", Namespace: namespace},
		Spec: openshiftappsv1.DeploymentConfigSpec{
			Selector: selectorLabels("idler-dc"),
			Replicas: replicas,
			Template: &spec,
		},
	}
	memberAwait.Create(s.T(), dc)
	return dc
}

func (s *userWorkloadsTestSuite) createReplicationController(namespace string) *corev1.ReplicationController {
	memberAwait := s.Member1()
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
	memberAwait.Create(s.T(), rc)
	return rc
}

func (s *userWorkloadsTestSuite) createStandalonePod(namespace, name string) *corev1.Pod {
	memberAwait := s.Member1()
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
	memberAwait.Create(s.T(), pod)
	return pod
}

func podSpec() corev1.PodSpec {
	zero := int64(0)
	return corev1.PodSpec{
		TerminationGracePeriodSeconds: &zero,
		Containers: []corev1.Container{{
			Name:    "sleep",
			Image:   "busybox",
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
