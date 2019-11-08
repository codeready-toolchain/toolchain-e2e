package e2e

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type nsTemplateSetTest struct {
	suite.Suite
	namespace   string
	testCtx     *framework.TestCtx
	awaitility  *wait.Awaitility
	memberAwait *wait.MemberAwaitility
	basicTier   *toolchainv1alpha1.NSTemplateTier
}

func TestNSTemplateSet(t *testing.T) {
	suite.Run(t, &nsTemplateSetTest{})
}

func (s *nsTemplateSetTest) SetupTest() {
	nsTmplSetList := &toolchainv1alpha1.NSTemplateSetList{}
	s.testCtx, s.awaitility = testsupport.WaitForDeployments(s.T(), nsTmplSetList)
	s.memberAwait = s.awaitility.Member()
	s.namespace = s.awaitility.MemberNs
	s.basicTier = getBasicTier(s.T(), s.awaitility.Client, s.awaitility.HostNs)
}

func (s *nsTemplateSetTest) TearDownTest() {
	if s.testCtx != nil {
		s.testCtx.Cleanup()
	}
}

func (s *nsTemplateSetTest) TestCreateOK() {
	username := "amar"

	s.createAndVerifyNSTmplSet(username)
}

func (s *nsTemplateSetTest) TestDeleteNamespaceOK() {
	t := s.T()
	username := "amit"
	nsTmplSet := s.createAndVerifyNSTmplSet(username)
	// delete Namespace 'dev'
	typ := "dev"
	var revision string
	for _, ns := range nsTmplSet.Spec.Namespaces {
		if ns.Type == typ {
			revision = ns.Revision
		}
	}
	t.Logf("Deleting Namespace type :%s", typ)
	devNs, err := s.memberAwait.WaitForNamespace(username, typ, revision)
	require.NoError(t, err)
	err = s.awaitility.Client.Delete(context.TODO(), devNs)
	require.NoError(t, err)

	// wait for Namespace dev to recreate
	_, err = s.memberAwait.WaitForNamespace(username, devNs.Labels["type"], devNs.Labels["revision"])
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
	err = s.memberAwait.WaitUntilNSTemplateSetDeleted(nsTmplSet.Name)
	require.NoError(t, err)

	// wait for Namespace
	for _, ns := range nsTmplSet.Spec.Namespaces {
		err = s.memberAwait.WaitUntilNamespaceDeleted(username, ns.Type)
		require.NoError(t, err)
	}
}

func (s *nsTemplateSetTest) createAndVerifyNSTmplSet(username string) *toolchainv1alpha1.NSTemplateSet {
	t := s.T()

	// create NSTmplSet
	t.Logf("Creating NSTmplSet with username:%s", username)
	nsTmplSet := s.newNSTmplSet(username)
	t.Logf("Creating NSTmplSet:%v", nsTmplSet)
	err := s.awaitility.Client.Create(context.TODO(), nsTmplSet, testsupport.CleanupOptions(s.testCtx))
	require.NoError(t, err)

	// wait for NSTmplSet
	nsTmplSet, err = s.memberAwait.WaitForNSTmplSet(nsTmplSet.Name, isProvisioned())
	require.NoError(t, err)

	// wait for Namespace
	for _, ns := range nsTmplSet.Spec.Namespaces {
		_, err = s.memberAwait.WaitForNamespace(username, ns.Type, ns.Revision)
		require.NoError(t, err)
	}

	return nsTmplSet
}

func (s *nsTemplateSetTest) newNSTmplSet(username string) *toolchainv1alpha1.NSTemplateSet {
	nsTmplSet := &toolchainv1alpha1.NSTemplateSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      username,
			Namespace: s.namespace,
		},
		Spec: toolchainv1alpha1.NSTemplateSetSpec{
			TierName: "basic",
		},
	}
	for _, ns := range s.basicTier.Spec.Namespaces {
		nsTmplSet.Spec.Namespaces = append(nsTmplSet.Spec.Namespaces, toolchainv1alpha1.NSTemplateSetNamespace{
			Type:     ns.Type,
			Revision: ns.Revision,
		})
	}
	return nsTmplSet
}

func getBasicTier(t *testing.T, client framework.FrameworkClient, namespace string) *toolchainv1alpha1.NSTemplateTier {
	tmplTier := &toolchainv1alpha1.NSTemplateTier{}
	err := client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: "basic"}, tmplTier)
	require.NoError(t, err)
	return tmplTier
}
