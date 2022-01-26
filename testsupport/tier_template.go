package testsupport

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/setup/templates"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func NewTierTemplate(t *testing.T, namespace, name, tierName, typeName, revision string, template []byte) *toolchainv1alpha1.TierTemplate {
	tierTmpl := &toolchainv1alpha1.TierTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: toolchainv1alpha1.TierTemplateSpec{
			TierName: tierName,
			Type:     typeName,
			Revision: revision,
		},
	}
	tmpl, err := templates.GetTemplateFromContent(template)
	require.NoError(t, err)
	tierTmpl.Spec.Template = *tmpl
	return tierTmpl
}
