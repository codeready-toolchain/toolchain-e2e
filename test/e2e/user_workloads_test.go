package e2e

import (
	"context"
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestUserWorkloads(t *testing.T) {
	suite.Run(t, &userWorkloadsTestSuite{})
}

type userWorkloadsTestSuite struct {
	baseUserIntegrationTest
}

func (s *userWorkloadsTestSuite) SetupSuite() {
	s.ctx, s.hostAwait, s.memberAwait = WaitForDeployments(s.T(), &v1alpha1.UserSignupList{})
	s.setApprovalPolicyConfig("automatic")
}

func (s *userWorkloadsTestSuite) TearDownTest() {
	s.ctx.Cleanup()
}

func (s *userWorkloadsTestSuite) TestIdler() {
	// Provision a user to idle with a short idling timeout
	s.createAndCheckUserSignup(true, "test-idler", "test-idler@redhat.com", ApprovedByAdmin()...)
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
		err := s.memberAwait.WaitUntilPodDeleted(p.Namespace, p.Name)
		require.NoError(s.T(), err)
	}

	// Noise pods are still there
	labels := map[string]string{"app": "idler-hello-openshift"}
	_, err = s.memberAwait.WaitForPods(idlerNoise.Name, labels, len(podsNoise), wait.UntilPodRunning())
	require.NoError(s.T(), err)
}

func (s *userWorkloadsTestSuite) prepareWorkloads(idler *v1alpha1.Idler) []*corev1.Pod {
	// Create a Deployment with two pods
	var err error
	replicas := int32(2)
	labels := map[string]string{"app": "idler-hello-openshift"}
	require.NoError(s.T(), err)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "idler-test-deployment", Namespace: idler.Name},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "hello-openshift",
						Image: "openshift/hello-openshift",
						Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
					}},
				},
			},
		},
	}
	err = s.memberAwait.Client.Create(context.TODO(), deployment)
	require.NoError(s.T(), err)

	// Create a standalone Pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "idler-test-pod",
			Namespace: idler.Name,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "hello-openshift",
				Image: "openshift/hello-openshift",
				Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
			}},
		},
	}
	err = s.memberAwait.Client.Create(context.TODO(), pod)
	require.NoError(s.T(), err)

	pods, err := s.memberAwait.WaitForPods(idler.Name, labels, 3, wait.UntilPodRunning())
	require.NoError(s.T(), err)

	return pods
}
