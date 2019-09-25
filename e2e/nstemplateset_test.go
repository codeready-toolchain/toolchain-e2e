package e2e

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/doubles"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type nsTemplateSetTest struct {
	suite.Suite
	namespace   string
	testCtx     *framework.TestCtx
	awaitility  *wait.Awaitility
	memberAwait *wait.MemberAwaitility
}

func TestNSTemplateSet(t *testing.T) {
	suite.Run(t, &nsTemplateSetTest{})
}

func (s *nsTemplateSetTest) SetupSuite() {
	nsTmplSetList := &toolchainv1alpha1.NSTemplateSetList{}
	s.testCtx, s.awaitility = doubles.InitializeOperators(s.T(), nsTmplSetList)
	s.memberAwait = s.awaitility.Member()
	s.namespace = s.awaitility.MemberNs
}

func (s *nsTemplateSetTest) TestCreateOK() {
	username := "amar"

	s.createAndVerifyNSTmplSet(username)
}

func (s *nsTemplateSetTest) TestDeleteNamespaceOK() {
	t := s.T()
	username := "amit"

	s.createAndVerifyNSTmplSet(username)

	// delete Namespace dev
	typeName := "dev"
	t.Logf("Deleting Namespace type :%s", typeName)
	devNs := s.memberAwait.GetNamespace(username, typeName)
	err := s.awaitility.Client.Delete(context.TODO(), devNs)
	require.NoError(t, err)

	// wait for Namespace dev to recreate
	err = s.memberAwait.WaitForNamespace(username, typeName)
	require.NoError(t, err)
}

func (s *nsTemplateSetTest) TestDeleteOK() {
	t := s.T()
	username := "hemal"

	nsTmplSet := s.createAndVerifyNSTmplSet(username)

	// delete NSTmplSet
	t.Logf("Deleting NSTmplSet with username:%s", username)
	err := s.awaitility.Client.Delete(context.TODO(), nsTmplSet)
	require.NoError(t, err)

	// wait for NSTmplSet
	err = s.memberAwait.WaitForDeletedNSTmplSet(nsTmplSet.Name)
	require.NoError(t, err)

	// wait for Namespace
	for _, ns := range nsTmplSet.Spec.Namespaces {
		err = s.memberAwait.WaitForDeletedNamespace(username, ns.Type)
		require.NoError(t, err)
	}
}

func (s *nsTemplateSetTest) createAndVerifyNSTmplSet(username string) *toolchainv1alpha1.NSTemplateSet {
	t := s.T()

	// create NSTmplSet
	t.Logf("Creating NSTmplSet with username:%s", username)
	nsTmplSet := newNSTmplSet(username, s.namespace)
	err := s.awaitility.Client.Create(context.TODO(), nsTmplSet, doubles.CleanupOptions(s.testCtx))
	require.NoError(t, err)

	// wait for NSTmplSet
	readyCond := toolchainv1alpha1.Condition{Type: toolchainv1alpha1.ConditionReady, Status: corev1.ConditionTrue, Reason: "Provisioned"}
	err = s.memberAwait.WaitForNSTmplSetWithConditions(nsTmplSet.Name, readyCond)
	require.NoError(t, err)

	// wait for Namespace
	for _, ns := range nsTmplSet.Spec.Namespaces {
		err = s.memberAwait.WaitForNamespace(username, ns.Type)
		require.NoError(t, err)
	}

	return nsTmplSet
}

func newNSTmplSet(username, namespace string) *toolchainv1alpha1.NSTemplateSet {
	nsTmplSet := &toolchainv1alpha1.NSTemplateSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      username,
			Namespace: namespace,
		},
		Spec: toolchainv1alpha1.NSTemplateSetSpec{
			TierName: "basic",
			Namespaces: []toolchainv1alpha1.Namespace{
				{Type: "dev", Revision: "abcde01"},
				{Type: "code", Revision: "abcde11"},
				{Type: "stage", Revision: "abcde21"},
			},
		},
	}
	return nsTmplSet
}
