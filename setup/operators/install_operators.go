package operators

import (
	"fmt"

	"github.com/codeready-toolchain/toolchain-e2e/setup/templates"
	"github.com/codeready-toolchain/toolchain-e2e/setup/wait"

	applyclientlib "github.com/codeready-toolchain/toolchain-common/pkg/client"
	ctemplate "github.com/codeready-toolchain/toolchain-common/pkg/template"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var operatorTemplates = []string{
	"kiali.yaml",
	"sbo.yaml",
	"rhoas.yaml",
}

func EnsureOperatorsInstalled(cl client.Client, s *runtime.Scheme) error {
	for _, operatorTemplate := range operatorTemplates {
		templatePath := "setup/operators/installtemplates/" + operatorTemplate

		tmpl, err := templates.GetTemplateFromFile(s, templatePath)
		if err != nil {
			return errors.Wrapf(err, "invalid template file: '%s'", templatePath)
		}

		processor := ctemplate.NewProcessor(s)
		objsToProcess, err := processor.Process(tmpl.DeepCopy(), map[string]string{})
		if err != nil {
			return err
		}

		// find the subscription resource
		var subscriptionResource applyclientlib.ToolchainObject
		foundSub := false
		for _, obj := range objsToProcess {
			if obj.GetRuntimeObject().GetObjectKind().GroupVersionKind().Kind == "Subscription" {
				subscriptionResource = obj
				foundSub = true
			}
		}
		if !foundSub {
			return fmt.Errorf("A subscription was not found in template file '%s'", templatePath)
		}

		if err := templates.ApplyObjects(cl, s, objsToProcess); err != nil {
			return err
		}

		// wait for operator installation to succeed
		err = wait.WaitForSubscriptionWithCondition(cl, subscriptionResource.GetName(), subscriptionResource.GetNamespace(), func(subscription *v1alpha1.Subscription) bool {
			currentCSV := subscription.Status.CurrentCSV
			if currentCSV == "" {
				return false
			}
			csverr := wait.WaitForCSVWithCondition(cl, currentCSV, subscriptionResource.GetNamespace(), func(csv *v1alpha1.ClusterServiceVersion) bool {
				if csv.Status.Phase == "Succeeded" {
					return true
				}
				return false
			})
			if csverr != nil {
				return false
			}
			return true
		})
		if err != nil {
			return errors.Wrapf(err, "Failed to verify installation of operator with subscription %s", subscriptionResource.GetName())
		}

		fmt.Printf("Verified installation of operator with subscription %s has succeeded\n", subscriptionResource.GetName())
	}

	return nil
}

// // EnsureOperator checks if an operator is installed, if it's not then it installs it
// func EnsureOperator(cl client.Client, operatorNamespace string) error {
// 	// processTemplate

// 	hasCSV, err := resources.HasCSVWithPrefix(cl, "kiali-operator", operatorNamespace)
// 	if err != nil && !k8serrors.IsNotFound(err) {
// 		return err
// 	}

// 	if hasCSV {
// 		// CSV is already present, skip the operator install
// 		return nil
// 	}

// 	subscription := &v1alpha1.Subscription{
// 		ObjectMeta: metav1.ObjectMeta{
// 			Name:      "kiali-ossm",
// 			Namespace: "openshift-operators",
// 		},
// 		Spec: &v1alpha1.SubscriptionSpec{
// 			Channel:                "stable",
// 			InstallPlanApproval:    v1alpha1.ApprovalAutomatic,
// 			Package:                "kiali-ossm",
// 			CatalogSource:          "redhat-operators",
// 			CatalogSourceNamespace: "openshift-marketplace",
// 		},
// 	}

// 	if err := cl.Create(context.TODO(), subscription); err != nil {
// 		return errors.Wrapf(err, "failed to install all-namespaces operator")
// 	}

// 	// Wait for CSV to be created
// 	return resources.WaitForCSVWithPrefix(cl, "kiali-operator", operatorNamespace)
// }
