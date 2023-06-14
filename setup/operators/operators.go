package operators

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/setup/templates"
	"github.com/codeready-toolchain/toolchain-e2e/setup/wait"

	ctemplate "github.com/codeready-toolchain/toolchain-common/pkg/template"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	hostSubscriptionName   = "subscription-toolchain-host-operator"
	memberSubscriptionName = "subscription-toolchain-member-operator"
)

var Templates = []string{
	"devspaces.yaml",
	"aikit.yaml",
	"openvino.yaml",
	// "devworkspace-operator.yaml", // included with DevSpaces install
	"rhoas.yaml",
	"rhods.yaml",
	"sbo.yaml", // included when rhoda is installed
	"serverless-operator.yaml",
	"web-terminal-operator.yaml",
	"gitops-primer-template.yaml",
	"kiali.yaml", // OSD comes with an operator that creates CSVs in all namespaces so kiali is being used in this case to mimic the behaviour on OCP clusters
}

var csvTimeout = 10 * time.Second

func VerifySandboxOperatorsInstalled(cl client.Client) error {
	subs := &v1alpha1.SubscriptionList{}
	if err := cl.List(context.TODO(), subs); err != nil {
		return err
	}

	foundHost := false
	foundMember := false
	for _, sub := range subs.Items {
		if strings.HasPrefix(sub.Name, hostSubscriptionName) {
			foundHost = true
		} else if strings.HasPrefix(sub.Name, memberSubscriptionName) {
			foundMember = true
		}
	}
	if foundHost && foundMember {
		return nil
	}
	return fmt.Errorf("the sandbox host and/or member operators were not found")
}

func EnsureOperatorsInstalled(cl client.Client, s *runtime.Scheme, templatePaths []string) error {
	for _, templatePath := range templatePaths {

		tmpl, err := templates.GetTemplateFromFile(templatePath)
		if err != nil {
			return errors.Wrapf(err, "invalid template file: '%s'", templatePath)
		}

		processor := ctemplate.NewProcessor(s)
		objsToProcess, err := processor.Process(tmpl.DeepCopy(), map[string]string{})
		if err != nil {
			return err
		}

		// find the subscription resource
		var subscriptionResource runtimeclient.Object
		foundSub := false
		for _, obj := range objsToProcess {
			if obj.GetObjectKind().GroupVersionKind().Kind == "Subscription" {
				subscriptionResource = obj
				foundSub = true
			}
		}
		if !foundSub {
			return fmt.Errorf("a subscription was not found in template file '%s'", templatePath)
		}

		if err := templates.ApplyObjects(cl, objsToProcess); err != nil {
			return err
		}

		startTime := time.Now()

		// wait for operator installation to succeed
		var csverr error
		var currentCSV string
		var lastCSVs []string
		timeout := configuration.DefaultTimeout

		// longer timeout just for subscriptions in the redhat-ods-operator namespace since installation can take significantly longer than other operators
		if subscriptionResource.GetNamespace() == "redhat-ods-operator" {
			timeout = 15 * time.Minute
		}

		err = wait.ForSubscriptionWithCriteria(cl, subscriptionResource.GetName(), subscriptionResource.GetNamespace(), timeout, func(subscription *v1alpha1.Subscription) bool {
			currentCSV = subscription.Status.CurrentCSV
			if currentCSV == "" {
				return false
			}

			if len(lastCSVs) == 0 || currentCSV != lastCSVs[len(lastCSVs)-1] { // subscription's current CSV has changed
				lastCSVs = append(lastCSVs, currentCSV)
				fmt.Printf("CurrentCSV of subscription: '%s'\n", currentCSV)
			}

			// wait for the CurrentCSV to reach Succeeded status
			csverr = wait.ForCSVWithCriteria(cl, currentCSV, subscriptionResource.GetNamespace(), csvTimeout, func(csv *v1alpha1.ClusterServiceVersion) bool {
				return csv.Status.Phase == "Succeeded"
			})
			if csverr != nil {
				return false
			}

			time.Sleep(5 * time.Second) // wait a few seconds and then check if there's another CSV to wait for
			currentCSV = subscription.Status.CurrentCSV
			return currentCSV == lastCSVs[len(lastCSVs)-1] // return true only if the CurrentCSV has not changed. ie. no upgrade needed
		})
		if len(lastCSVs) > 1 {
			fmt.Printf("\nATTENTION! Update subscription '%s' StartingCSV to %s to speed up future installations\n\n", subscriptionResource.GetName(), lastCSVs[len(lastCSVs)-1])
		}
		installDuration := time.Since(startTime)
		if csverr != nil {
			return errors.Wrapf(csverr, "failed to find CSV '%s' with Phase 'Succeeded'", currentCSV)
		}
		if err != nil {
			return errors.Wrapf(err, "failed to verify installation of operator with subscription '%s' after %s", subscriptionResource.GetName(), installDuration.String())
		}

		fmt.Printf("Verified installation of operator with subscription '%s' completed in %s\n\n", subscriptionResource.GetName(), installDuration.String())
	}

	return nil
}
