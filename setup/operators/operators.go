package operators

import (
	"context"
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

const (
	hostSubscriptionName   = "subscription-toolchain-host-operator"
	memberSubscriptionName = "subscription-toolchain-member-operator"
)

var Templates = []string{
	"kiali.yaml",
	"sbo.yaml",
	"rhoas.yaml",
}

func VerifySandboxOperatorsInstalled(cl client.Client) error {
	subs := &v1alpha1.SubscriptionList{}
	if err := cl.List(context.TODO(), subs); err != nil {
		return err
	}

	foundHost := false
	foundMember := false
	for _, sub := range subs.Items {
		if sub.Name == hostSubscriptionName {
			foundHost = true
		} else if sub.Name == memberSubscriptionName {
			foundMember = true
		}
	}
	if foundHost && foundMember {
		return nil
	}
	return fmt.Errorf("the sandbox host and member operators were not found")
}

func EnsureOperatorsInstalled(cl client.Client, s *runtime.Scheme, templatePaths []string) error {
	for _, templatePath := range templatePaths {

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
		var csverr error
		var currentCSV string
		err = wait.WaitForSubscriptionWithCondition(cl, subscriptionResource.GetName(), subscriptionResource.GetNamespace(), func(subscription *v1alpha1.Subscription) bool {
			currentCSV = subscription.Status.CurrentCSV
			if currentCSV == "" {
				return false
			}
			csverr = wait.WaitForCSVWithCondition(cl, currentCSV, subscriptionResource.GetNamespace(), func(csv *v1alpha1.ClusterServiceVersion) bool {
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
		if csverr != nil {
			return errors.Wrapf(csverr, "Failed to find CSV '%s' with Phase 'Succeeded'", currentCSV)
		}
		if err != nil {
			return errors.Wrapf(err, "Failed to verify installation of operator with subscription %s", subscriptionResource.GetName())
		}

		fmt.Printf("Verified installation of operator with subscription %s has succeeded\n", subscriptionResource.GetName())
	}

	return nil
}
