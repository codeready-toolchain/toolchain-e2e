package e2e

import (
	"context"
	"fmt"
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

	t.Logf("Creating NSTmplSet with username:%s", username)
	nsTmplSet := newNSTmplSet(username, s.namespace)
	err := s.awaitility.Client.Create(context.TODO(), nsTmplSet, doubles.CleanupOptions(s.testCtx))
	require.NoError(t, err)

	err = s.memberAwait.WaitForNSTmplSet(nsTmplSet.Name)
	require.NoError(t, err)

	for _, ns := range nsTmplSet.Spec.Namespaces {
		err = s.memberAwait.WaitForNamespace(fmt.Sprintf("%s-%s", username, ns.Type))
		require.NoError(t, err)
	}
}

func (s *nsTemplateSetTest) TestDeleteOK() {
	t := s.T()
	username := "hemal"

	t.Logf("Creating NSTmplSet with username:%s", username)
	nsTmplSet := newNSTmplSet(username, s.namespace)
	err := s.awaitility.Client.Create(context.TODO(), nsTmplSet, doubles.CleanupOptions(s.testCtx))
	require.NoError(t, err)

	err = s.memberAwait.WaitForNSTmplSet(nsTmplSet.Name)
	require.NoError(t, err)

	for _, ns := range nsTmplSet.Spec.Namespaces {
		err = s.memberAwait.WaitForNamespace(fmt.Sprintf("%s-%s", username, ns.Type))
		require.NoError(t, err)
	}

	t.Logf("Deleting NSTmplSet with username:%s", username)
	err = s.awaitility.Client.Delete(context.TODO(), nsTmplSet)
	require.NoError(t, err)

	err = s.memberAwait.WaitForDeletedNSTmplSet(nsTmplSet.Name)
	require.NoError(t, err)

	for _, ns := range nsTmplSet.Spec.Namespaces {
		err = s.memberAwait.WaitForDeletedNamespace(fmt.Sprintf("%s-%s", username, ns.Type))
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
