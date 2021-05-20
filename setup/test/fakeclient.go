package test

import (
	"github.com/codeready-toolchain/api/api/v1alpha1"
	commontest "github.com/codeready-toolchain/toolchain-common/pkg/test"

	quotav1 "github.com/openshift/api/quota/v1"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake" //nolint: staticcheck // not deprecated anymore: see https://github.com/kubernetes-sigs/controller-runtime/pull/1101
)

func NewFakeClient(t commontest.T, initObjs ...runtime.Object) *commontest.FakeClient {
	s := scheme.Scheme
	builder := append(runtime.SchemeBuilder{}, v1alpha1.AddToScheme, quotav1.Install, operatorsv1alpha1.AddToScheme)
	err := builder.AddToScheme(s)
	require.NoError(t, err)
	cl := fake.NewFakeClientWithScheme(s, initObjs...)
	return &commontest.FakeClient{Client: cl, T: t}
}
