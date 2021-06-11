package templates

import (
	"fmt"
	"io/ioutil"
	"sync"
	"time"

	applyclientlib "github.com/codeready-toolchain/toolchain-common/pkg/client"
	cfg "github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	multierror "github.com/hashicorp/go-multierror"
	templatev1 "github.com/openshift/api/template/v1"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetTemplateFromFile(s *runtime.Scheme, filepath string) (*templatev1.Template, error) {
	content, err := ioutil.ReadFile(filepath)
	if err != nil {
		return nil, err
	}
	decoder := serializer.NewCodecFactory(s).UniversalDeserializer()
	tmpl := &templatev1.Template{}
	_, gvk, err := decoder.Decode([]byte(content), nil, tmpl)
	if err != nil {
		return nil, err
	}
	if gvk.Kind == "Template" { // expect an OpenShift template
		return tmpl, nil
	}
	return nil, fmt.Errorf("wrong kind of object in the template file: '%s'", gvk)
}

// ApplyObjects applies the given objects in order
func ApplyObjects(cl client.Client, s *runtime.Scheme, objsToApply []applyclientlib.ToolchainObject, modifiers ...TooolchainObjectModifier) error {
	applycl := applyclientlib.NewApplyClient(cl, s)
	for _, obj := range objsToApply {
		fmt.Printf("Applying %s object with name '%s' in namespace '%s'\n", obj.GetGvk().Kind, obj.GetName(), obj.GetNamespace())
		if err := applyObject(applycl, obj, modifiers...); err != nil {
			return err
		}
	}
	return nil
}

// ApplyObjectsConcurrently applies multiple objects concurrently
func ApplyObjectsConcurrently(cl client.Client, s *runtime.Scheme, combinedObjsToProcess []applyclientlib.ToolchainObject, modifiers ...TooolchainObjectModifier) error {
	var objProcessors []<-chan error
	objChannel := distribute(combinedObjsToProcess)
	for i := 0; i < len(combinedObjsToProcess); i++ {
		objProcessors = append(objProcessors, startObjectProcessor(cl, s, objChannel, modifiers...))
		time.Sleep(100 * time.Millisecond) // wait for a short time before starting each object processor to avoid hitting rate limits
	}

	// combine the results
	var overallErr error
	for err := range combineResults(objProcessors...) {
		if err != nil {
			overallErr = multierror.Append(overallErr, err)
		}
	}

	return overallErr
}

func distribute(objs []applyclientlib.ToolchainObject) <-chan applyclientlib.ToolchainObject {
	out := make(chan applyclientlib.ToolchainObject)
	go func() {
		for _, obj := range objs {
			out <- obj
		}
		close(out)
	}()
	return out
}

func combineResults(results ...<-chan error) <-chan error {
	var wg sync.WaitGroup
	out := make(chan error)

	// Start an output goroutine for each input channel in results.
	// output copies values from results to out until results is closed, then calls wg.Done.
	output := func(c <-chan error) {
		for r := range c {
			out <- r
		}
		wg.Done()
	}
	wg.Add(len(results))
	for _, result := range results {
		go output(result)
	}

	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func startObjectProcessor(cl client.Client, s *runtime.Scheme, objSource <-chan applyclientlib.ToolchainObject, modifiers ...TooolchainObjectModifier) <-chan error {
	out := make(chan error)
	go func() {
		applycl := applyclientlib.NewApplyClient(cl, s)
		for obj := range objSource {
			out <- applyObject(applycl, obj, modifiers...)
			time.Sleep(100 * time.Millisecond)
		}
		close(out)
	}()
	return out
}

type TooolchainObjectModifier func(obj applyclientlib.ToolchainObject) error

func NamespaceModifier(userNS string) TooolchainObjectModifier {
	return func(obj applyclientlib.ToolchainObject) error {
		// enforce the creation of the objects in the `userNS` namespace
		m, err := meta.Accessor(obj.GetRuntimeObject())
		if err != nil {
			return err
		}
		m.SetNamespace(userNS)
		return nil
	}
}

func applyObject(applycl *applyclientlib.ApplyClient, obj applyclientlib.ToolchainObject, modifiers ...TooolchainObjectModifier) error {

	// apply any modifiers before applying the object
	for _, modifier := range modifiers {
		if err := modifier(obj); err != nil {
			return err
		}
	}

	// retry the apply in case it fails due to errors like the following:
	// unable to create resource of kind: Deployment, version: v1: Operation cannot be fulfilled on clusterresourcequotas.quota.openshift.io "for-zippy-1882-deployments": the object has been modified; please apply your changes to the latest version and try again
	if err := k8swait.Poll(cfg.DefaultRetryInterval, 30*time.Second, func() (bool, error) {
		if _, applyErr := applycl.ApplyObject(obj.GetRuntimeObject()); applyErr != nil {
			return false, applyErr
		}
		return true, nil
	}); err != nil {
		return errors.Wrapf(err, "could not apply resource '%s' in namespace '%s'", obj.GetName(), obj.GetNamespace())
	}
	return nil
}
