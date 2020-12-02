package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
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
	baseUserIntegrationTest
}

func (s *userWorkloadsTestSuite) SetupSuite() {
	s.ctx, s.hostAwait, s.memberAwait = WaitForDeployments(s.T(), &v1alpha1.UserSignupList{})
}

func (s *userWorkloadsTestSuite) TearDownTest() {
	s.ctx.Cleanup()
}

func (s *userWorkloadsTestSuite) TestIdlerAndPriorityClass() {
	// Provision a user to idle with a short idling timeout
	s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())
	s.createAndCheckUserSignup(true, "test-idler", "test-idler@redhat.com", true, ApprovedByAdmin()...)
	idler, err := s.memberAwait.WaitForIdler("test-idler-dev", wait.IdlerConditions(Running()))
	require.NoError(s.T(), err)

	// Noise
	idlerNoise, err := s.memberAwait.WaitForIdler("test-idler-stage", wait.IdlerConditions(Running()))
	require.NoError(s.T(), err)

	// Create payloads for both users
	podsToIdle := s.prepareWorkloads(idler.Name, wait.WithSandboxPriorityClass())
	podsNoise := s.prepareWorkloads(idlerNoise.Name, wait.WithSandboxPriorityClass())

	// Create another noise pods in non-user namespace
	s.memberAwait.CreateNamespace("workloads-noise")
	externalNsPodsNoise := s.prepareWorkloads("workloads-noise", wait.WithOriginalPriorityClass())

	// Set a short timeout for one of the idler to trigger pod idling
	idler.Spec.TimeoutSeconds = 5
	idler, err = s.memberAwait.UpdateIdlerSpec(idler) // The idler is currently updating its status since it's already been idling the pods. So we need to keep trying to update.
	require.NoError(s.T(), err)

	// Wait for the pods to be deleted
	for _, p := range podsToIdle {
		err := s.memberAwait.WaitUntilPodsDeleted(p.Namespace, wait.WithPodName(p.Name))
		require.NoError(s.T(), err)
	}

	// make sure that "noise" pods are still there
	_, err = s.memberAwait.WaitForPods(idlerNoise.Name, len(podsNoise), wait.PodRunning(), wait.WithPodLabel("idler", "idler"), wait.WithSandboxPriorityClass())
	require.NoError(s.T(), err)
	_, err = s.memberAwait.WaitForPods("workloads-noise", len(externalNsPodsNoise), wait.PodRunning(), wait.WithPodLabel("idler", "idler"), wait.WithOriginalPriorityClass())
	require.NoError(s.T(), err)

	// Create another pod and make sure it's deleted.
	// In the tests above the Idler reconcile was triggered after we changed the Idler resource (to set a short timeout).
	// Now we want to verify that the idler reconcile is triggered without modifying the Idler resource.
	pod := s.createStandalonePod(idler.Name, "idler-test-pod-2")      // create just one standalone pod. No need to create all possible pod controllers which may own pods.
	_, err = s.memberAwait.WaitForPod(idler.Name, "idler-test-pod-2") // pod was created
	require.NoError(s.T(), err)
	time.Sleep(time.Duration(2*idler.Spec.TimeoutSeconds) * time.Second)
	err = s.memberAwait.WaitUntilPodDeleted(pod.Namespace, pod.Name)
	require.NoError(s.T(), err)

	// There should not be any pods left in the namespace
	err = s.memberAwait.WaitUntilPodsDeleted(idler.Name, wait.WithPodLabel("idler", "idler"))
	require.NoError(s.T(), err)
}

func (s *userWorkloadsTestSuite) prepareWorkloads(namespace string, additionalPodCriteria ...wait.PodWaitCriterion) []corev1.Pod {
	s.createStandalonePod(namespace, "idler-test-pod-1")
	d := s.createDeployment(namespace)
	n := 1 + int(*d.Spec.Replicas) // total number of created pods

	rs := s.createReplicaSet(namespace)
	n = n + int(*rs.Spec.Replicas)

	s.createDaemonSet(namespace)
	nodes := &corev1.NodeList{}
	err := s.memberAwait.Client.List(context.TODO(), nodes, client.MatchingLabels(map[string]string{"node-role.kubernetes.io/worker": ""}))
	require.NoError(s.T(), err)
	n = n + len(nodes.Items) // DaemonSet creates N pods where N is the number of worker nodes in the cluster

	s.createJob(namespace)
	n++

	dc := s.createDeploymentConfig(namespace)
	n = n + int(dc.Spec.Replicas)

	rc := s.createReplicationController(namespace)
	n = n + int(*rc.Spec.Replicas)

	pods, err := s.memberAwait.WaitForPods(namespace, n, append(additionalPodCriteria, wait.PodRunning(),
		wait.WithPodLabel("idler", "idler"))...)
	require.NoError(s.T(), err)
	return pods
}

func (s *userWorkloadsTestSuite) createDeployment(namespace string) *appsv1.Deployment {
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
	err := s.memberAwait.Create(deployment)
	require.NoError(s.T(), err)

	return deployment
}

func (s *userWorkloadsTestSuite) createReplicaSet(namespace string) *appsv1.ReplicaSet {
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
	err := s.memberAwait.Create(rs)
	require.NoError(s.T(), err)

	return rs
}

func (s *userWorkloadsTestSuite) createDaemonSet(namespace string) *appsv1.DaemonSet {
	// Standalone ReplicaSet
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-daemonset", Namespace: namespace},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: selectorLabels("idler-ds")},
			Template: podTemplateSpec("idler-ds"),
		},
	}
	err := s.memberAwait.Create(ds)
	require.NoError(s.T(), err)

	return ds
}

func (s *userWorkloadsTestSuite) createJob(namespace string) *batchv1.Job {
	// Job
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-job", Namespace: namespace},
		Spec: batchv1.JobSpec{
			Template: podTemplateSpec(""),
		},
	}
	job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
	err := s.memberAwait.Create(job)
	require.NoError(s.T(), err)

	return job
}

func (s *userWorkloadsTestSuite) createDeploymentConfig(namespace string) *openshiftappsv1.DeploymentConfig {
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
	err := s.memberAwait.Create(dc)
	require.NoError(s.T(), err)

	return dc
}

func (s *userWorkloadsTestSuite) createReplicationController(namespace string) *corev1.ReplicationController {
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
	err := s.memberAwait.Create(rc)
	require.NoError(s.T(), err)

	return rc
}

func (s *userWorkloadsTestSuite) createStandalonePod(namespace, name string) *corev1.Pod {
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
	err := s.memberAwait.Create(pod)
	require.NoError(s.T(), err)
	return pod
}

func podSpec() corev1.PodSpec {
	return corev1.PodSpec{
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
