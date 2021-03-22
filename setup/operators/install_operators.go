package operators

import (
	"context"

	"github.com/codeready-toolchain/toolchain-e2e/setup/resources"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const AllNamespacesOperatorName = "kiali-ossm"
const AllNamespacesOperatorPrefix = "kiali-operator"

func EnsureAllNamespacesOperator(cl client.Client, csvNamespace string) error {
	hasCSV, err := resources.HasCSVWithPrefix(cl, AllNamespacesOperatorPrefix, csvNamespace)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	if hasCSV {
		// CSV is already present, skip the operator install
		return nil
	}

	subscription := &v1alpha1.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AllNamespacesOperatorName,
			Namespace: "openshift-operators",
		},
		Spec: &v1alpha1.SubscriptionSpec{
			Channel:                "stable",
			InstallPlanApproval:    v1alpha1.ApprovalAutomatic,
			Package:                AllNamespacesOperatorName,
			CatalogSource:          "redhat-operators",
			CatalogSourceNamespace: "openshift-marketplace",
		},
	}

	if err := cl.Create(context.TODO(), subscription); err != nil {
		return errors.Wrapf(err, "failed to install all-namespaces operator")
	}

	// Wait for CSV to be created
	return resources.WaitForCSVWithPrefix(cl, AllNamespacesOperatorPrefix, csvNamespace)
}
