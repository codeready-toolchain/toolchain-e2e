package e2e

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/doubles"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	templatev1 "github.com/openshift/api/template/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
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

	// TODO remove this, temp fix until CRT-231 is completed
	setupTemplateTier(s.T(), s.testCtx, s.awaitility.Client, s.awaitility.Scheme, s.awaitility.HostNs)
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
	err = s.memberAwait.WaitForNamespace(username, devNs.Labels["type"], devNs.Labels["revision"])
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
	err = s.memberAwait.WaitForNSTmplSet(nsTmplSet.Name, toBeProvisioned())
	require.NoError(t, err)

	// wait for Namespace
	for _, ns := range nsTmplSet.Spec.Namespaces {
		err = s.memberAwait.WaitForNamespace(username, ns.Type, ns.Revision)
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
			Namespaces: []toolchainv1alpha1.NSTemplateSetNamespace{
				{Type: "dev", Revision: "123abc", Template: ""},
				{Type: "code", Revision: "123abc", Template: ""},
				{Type: "stage", Revision: "123abc", Template: ""},
			},
		},
	}
	return nsTmplSet
}

// TODO remove this, temp fix until CRT-231 is completed
func setupTemplateTier(t *testing.T, testCtx *framework.TestCtx, client framework.FrameworkClient, scheme *runtime.Scheme, namespace string) {
	codecFactory := serializer.NewCodecFactory(scheme)
	decoder := codecFactory.UniversalDeserializer()

	devTmpl, err := decodeTemplate(decoder, _templatesBasicDevYaml)
	require.NoError(t, err)
	codeTmpl, err := decodeTemplate(decoder, _templatesBasicCodeYaml)
	require.NoError(t, err)
	stageTmpl, err := decodeTemplate(decoder, _templatesBasicStageYaml)
	require.NoError(t, err)

	nsTmplTier := &toolchainv1alpha1.NSTemplateTier{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "basic",
			Namespace: namespace,
		},
		Spec: toolchainv1alpha1.NSTemplateTierSpec{
			Namespaces: []toolchainv1alpha1.NSTemplateTierNamespace{
				{Type: "dev", Revision: "123abc", Template: *devTmpl},
				{Type: "code", Revision: "123abc", Template: *codeTmpl},
				{Type: "stage", Revision: "123abc", Template: *stageTmpl},
			},
		},
	}
	err = client.Create(context.TODO(), nsTmplTier, doubles.CleanupOptions(testCtx))
	require.NoError(t, err)
}

func decodeTemplate(decoder runtime.Decoder, tmplContent []byte) (*templatev1.Template, error) {
	tmpl := &templatev1.Template{}
	_, _, err := decoder.Decode(tmplContent, nil, tmpl)
	if err != nil {
		return nil, err
	}
	return tmpl, err
}

var _templatesBasicDevYaml = []byte(`apiVersion: template.openshift.io/v1
kind: Template
metadata:
  labels:
    provider: codeready-toolchain
    project: codeready-toolchain
  name: basic-dev
objects:
  - apiVersion: v1
    kind: Namespace
    metadata:
      labels:
        provider: codeready-toolchain
        project: codeready-toolchain
      name: ${USER_NAME}-dev
  - apiVersion: authorization.openshift.io/v1
    kind: RoleBinding
    metadata:
      labels:
        provider: codeready-toolchain
        app: codeready-toolchain
      name: user-edit
      namespace: ${USER_NAME}-dev
    roleRef:
      name: edit
    subjects:
      - kind: User
        name: ${USER_NAME}
    userNames:
      - ${USER_NAME}
parameters:
  - name: USER_NAME
    value: johnsmith
`)

var _templatesBasicCodeYaml = []byte(`apiVersion: template.openshift.io/v1
kind: Template
metadata:
  labels:
    provider: codeready-toolchain
    project: codeready-toolchain
  name: basic-code
objects:
  - apiVersion: v1
    kind: Namespace
    metadata:
      labels:
        provider: codeready-toolchain
        project: codeready-toolchain
      name: ${USER_NAME}-code
  - apiVersion: authorization.openshift.io/v1
    kind: RoleBinding
    metadata:
      labels:
        provider: codeready-toolchain
        app: codeready-toolchain
      name: user-edit
      namespace: ${USER_NAME}-code
    roleRef:
      name: edit
    subjects:
      - kind: User
        name: ${USER_NAME}
    userNames:
      - ${USER_NAME}
parameters:
  - name: USER_NAME
    value: johnsmith
`)

var _templatesBasicStageYaml = []byte(`apiVersion: template.openshift.io/v1
kind: Template
metadata:
  labels:
    provider: codeready-toolchain
    project: codeready-toolchain
  name: basic-stage
objects:
  - apiVersion: v1
    kind: Namespace
    metadata:
      labels:
        provider: codeready-toolchain
        project: codeready-toolchain
      name: ${USER_NAME}-stage
  - apiVersion: authorization.openshift.io/v1
    kind: RoleBinding
    metadata:
      labels:
        provider: codeready-toolchain
        app: codeready-toolchain
      name: user-edit
      namespace: ${USER_NAME}-stage
    roleRef:
      name: edit
    subjects:
      - kind: User
        name: ${USER_NAME}
    userNames:
      - ${USER_NAME}
parameters:
  - name: USER_NAME
    value: johnsmith
`)
