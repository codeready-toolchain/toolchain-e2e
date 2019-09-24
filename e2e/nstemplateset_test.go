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
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	t := s.T()
	username := "amar"

	// create NSTmplSet
	t.Logf("Creating NSTmplSet with username:%s", username)
	nsTmplSet := newNSTmplSet(username, s.namespace)
	err := s.awaitility.Client.Create(context.TODO(), nsTmplSet, doubles.CleanupOptions(s.testCtx))
	require.NoError(t, err)

	// wait for NSTmplSet
	err = s.memberAwait.WaitForNSTmplSet(nsTmplSet.Name)
	require.NoError(t, err)

	// wait for Namespace
	for _, ns := range nsTmplSet.Spec.Namespaces {
		err = s.memberAwait.WaitForNamespace(username, ns.Type)
		require.NoError(t, err)
	}
}

func (s *nsTemplateSetTest) TestDeleteNamespaceOK() {
	t := s.T()
	username := "amit"

	// create NSTmplSet
	t.Logf("Creating NSTmplSet with username:%s", username)
	nsTmplSet := newNSTmplSet(username, s.namespace)
	err := s.awaitility.Client.Create(context.TODO(), nsTmplSet, doubles.CleanupOptions(s.testCtx))
	require.NoError(t, err)

	// wait for NSTmplSet
	err = s.memberAwait.WaitForNSTmplSet(nsTmplSet.Name)
	require.NoError(t, err)

	// wait for Namespace
	for _, ns := range nsTmplSet.Spec.Namespaces {
		err = s.memberAwait.WaitForNamespace(username, ns.Type)
		require.NoError(t, err)
	}

	// delete Namespace dev
	typeName := "dev"
	t.Logf("Deleting Namespace type :%s", typeName)
	devNs := s.memberAwait.GetNamespace(username, typeName)
	err = s.awaitility.Client.Delete(context.TODO(), devNs)
	require.NoError(t, err)

	// wait for Namespace dev to recreate
	err = s.memberAwait.WaitForNamespace(username, typeName)
	require.NoError(t, err)
}

func (s *nsTemplateSetTest) TestDeleteOK() {
	t := s.T()
	username := "hemal"

	// create NSTmplSet
	t.Logf("Creating NSTmplSet with username:%s", username)
	nsTmplSet := newNSTmplSet(username, s.namespace)
	err := s.awaitility.Client.Create(context.TODO(), nsTmplSet, doubles.CleanupOptions(s.testCtx))
	require.NoError(t, err)

	// wait for NSTmplSet
	err = s.memberAwait.WaitForNSTmplSet(nsTmplSet.Name)
	require.NoError(t, err)

	// wait for Namespace
	for _, ns := range nsTmplSet.Spec.Namespaces {
		err = s.memberAwait.WaitForNamespace(username, ns.Type)
		require.NoError(t, err)
	}

	// delete NSTmplSet
	t.Logf("Deleting NSTmplSet with username:%s", username)
	err = s.awaitility.Client.Delete(context.TODO(), nsTmplSet)
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

func newNSTmplSet(username, namespace string) *toolchainv1alpha1.NSTemplateSet {
	nsTmplSet := &toolchainv1alpha1.NSTemplateSet{
		ObjectMeta: v1.ObjectMeta{
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
