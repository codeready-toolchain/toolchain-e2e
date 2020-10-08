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

func (s *userWorkloadsTestSuite) TestIdler() {
	// Provision a user to idle with a short idling timeout
	s.hostAwait.UpdateHostOperatorConfig(test.AutomaticApproval().Enabled())
	s.createAndCheckUserSignup(true, "test-idler", "test-idler@redhat.com", true, ApprovedByAdmin()...)
	idler, err := s.memberAwait.WaitForIdler("test-idler-dev")
	require.NoError(s.T(), err)

	// Noise
	idlerNoise, err := s.memberAwait.WaitForIdler("test-idler-stage")
	require.NoError(s.T(), err)

	// Create payloads for both users
	podsToIdle := s.prepareWorkloads(idler)
	podsNoise := s.prepareWorkloads(idlerNoise)

	// Set a short timeout for one of the idler to trigger pod idling
	idler.Spec.TimeoutSeconds = 3
	err = s.memberAwait.Client.Update(context.TODO(), idler)
	require.NoError(s.T(), err)

	// Wait for the pods to be deleted
	for _, p := range podsToIdle {
		err := s.memberAwait.WaitUntilPodsDeleted(p.Namespace, wait.WithPodName(p.Name))
		require.NoError(s.T(), err)
	}

	// Noise pods are still there
	_, err = s.memberAwait.WaitForPods(idlerNoise.Name, len(podsNoise), wait.UntilPodRunning(), wait.WithPodLabels(labels()))
	require.NoError(s.T(), err)

	// Create another pod and make sure it's deleted.
	// In the tests above the Idler reconcile was triggered after we changed the Idler resource (to set a short timeout).
	// Now we want to verify that the idler reconcile is triggered without modifying the Idler resource.
	pod := s.createStandalonePod(idler) // create just one standalone pod. No need to create all possible pod controllers which may own pods.
	// We can't really make sure that the pod was created first before the idler deletes it.
	// So, let's just wait for three seconds assuming it will be enough for the API server to create a pod before start waiting for the Pod to be deleted.
	time.Sleep(3 * time.Second)
	err = s.memberAwait.WaitUntilPodDeleted(pod.Namespace, pod.Name)
	require.NoError(s.T(), err)

	// There should not be any pods left in the namespace
	err = s.memberAwait.WaitUntilPodsDeleted(idler.Name, wait.WithPodLabels(labels()))
	require.NoError(s.T(), err)
}

func (s *userWorkloadsTestSuite) prepareWorkloads(idler *v1alpha1.Idler) []*corev1.Pod {
	s.createStandalonePod(idler)
	d := s.createDeployment(idler)
	n := 1 + int(*d.Spec.Replicas) // total number of created pods

	rs := s.createReplicaSet(idler)
	n = n + int(*rs.Spec.Replicas)

	s.createDaemonSet(idler)
	nodes := &corev1.NodeList{}
	err := s.memberAwait.Client.List(context.TODO(), nodes, client.MatchingLabels(map[string]string{"node-role.kubernetes.io/worker": ""}))
	require.NoError(s.T(), err)
	n = n + len(nodes.Items) // DaemonSet creates N pods where N is the number of worker nodes in the cluster

	s.createJob(idler)
	n++

	dc := s.createDeploymentConfig(idler)
	n = n + int(dc.Spec.Replicas)

	rc := s.createReplicationController(idler)
	n = n + int(*rc.Spec.Replicas)

	pods, err := s.memberAwait.WaitForPods(idler.Name, n, wait.UntilPodRunning(), wait.WithPodLabels(labels()))
	require.NoError(s.T(), err)
	return pods
}

func (s *userWorkloadsTestSuite) createDeployment(idler *v1alpha1.Idler) *appsv1.Deployment {
	// Create a Deployment with two pods
	replicas := int32(2)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-deployment", Namespace: idler.Name},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels()},
			Replicas: &replicas,
			Template: podTemplateSpec(),
		},
	}
	err := s.memberAwait.Client.Create(context.TODO(), deployment)
	require.NoError(s.T(), err)

	return deployment
}

func (s *userWorkloadsTestSuite) createReplicaSet(idler *v1alpha1.Idler) *appsv1.ReplicaSet {
	// Standalone ReplicaSet
	replicas := int32(1)
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-replicaset", Namespace: idler.Name},
		Spec: appsv1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels()},
			Replicas: &replicas,
			Template: podTemplateSpec(),
		},
	}
	err := s.memberAwait.Client.Create(context.TODO(), rs)
	require.NoError(s.T(), err)

	return rs
}

func (s *userWorkloadsTestSuite) createDaemonSet(idler *v1alpha1.Idler) *appsv1.DaemonSet {
	// Standalone ReplicaSet
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-daemonset", Namespace: idler.Name},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels()},
			Template: podTemplateSpec(),
		},
	}
	err := s.memberAwait.Client.Create(context.TODO(), ds)
	require.NoError(s.T(), err)

	return ds
}

func (s *userWorkloadsTestSuite) createJob(idler *v1alpha1.Idler) *batchv1.Job {
	// Job
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-job", Namespace: idler.Name},
		Spec: batchv1.JobSpec{
			Template: podTemplateSpec(),
		},
	}
	job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
	err := s.memberAwait.Client.Create(context.TODO(), job)
	require.NoError(s.T(), err)

	return job
}

func (s *userWorkloadsTestSuite) createDeploymentConfig(idler *v1alpha1.Idler) *openshiftappsv1.DeploymentConfig {
	// Create a Deployment with two pods
	spec := podTemplateSpec()
	replicas := int32(2)
	dc := &openshiftappsv1.DeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-dc", Namespace: idler.Name},
		Spec: openshiftappsv1.DeploymentConfigSpec{
			Selector: labels(),
			Replicas: replicas,
			Template: &spec,
		},
	}
	err := s.memberAwait.Client.Create(context.TODO(), dc)
	require.NoError(s.T(), err)

	return dc
}

func (s *userWorkloadsTestSuite) createReplicationController(idler *v1alpha1.Idler) *corev1.ReplicationController {
	// Standalone ReplicationController
	spec := podTemplateSpec()
	replicas := int32(2)
	rc := &corev1.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-rc", Namespace: idler.Name},
		Spec: corev1.ReplicationControllerSpec{
			Selector: labels(),
			Replicas: &replicas,
			Template: &spec,
		},
	}
	err := s.memberAwait.Client.Create(context.TODO(), rc)
	require.NoError(s.T(), err)

	return rc
}

func (s *userWorkloadsTestSuite) createStandalonePod(idler *v1alpha1.Idler) *corev1.Pod {
	// Create a Deployment with two pods
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idler-test-pod",
			Namespace: idler.Name,
			Labels:    labels(),
		},
		Spec: podSpec(),
	}
	err := s.memberAwait.Client.Create(context.TODO(), pod)
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

func podTemplateSpec() corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: labels()},
		Spec:       podSpec(),
	}
}

func labels() map[string]string {
	return map[string]string{"app": "idler-sleep"}
}
