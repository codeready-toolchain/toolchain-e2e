package resources

import (
	"fmt"
	"io/ioutil"
	"sync"
	"time"

	applyclientlib "github.com/codeready-toolchain/toolchain-common/pkg/client"
	"github.com/codeready-toolchain/toolchain-common/pkg/template"
	cfg "github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"

	templatev1 "github.com/openshift/api/template/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var tmpls map[string]*templatev1.Template = make(map[string]*templatev1.Template)

func CreateFromTemplateFiles(cl client.Client, s *runtime.Scheme, username string, templatePaths []string) error {
	userNS := fmt.Sprintf("%s-stage", username)
	combinedObjsToProcess := []applyclientlib.ToolchainObject{}
	for _, templatePath := range templatePaths {
		// get the template from the file if it hasn't been processed already
		if _, ok := tmpls[templatePath]; !ok {
			var err error
			if tmpls[templatePath], err = getTemplateFromFile(s, templatePath); err != nil {
				return errors.Wrapf(err, "invalid template file: '%s'", templatePath)
			}
		}
		tmpl := tmpls[templatePath]

		// waiting for each namespace here prevents some edge cases where the setup job can progress beyond the usersignup job and fail with a timeout
		if err := WaitForNamespace(cl, userNS); err != nil {
			return err
		}
		processor := template.NewProcessor(s)
		objsToProcess, err := processor.Process(tmpl.DeepCopy(), map[string]string{})
		if err != nil {
			return err
		}
		combinedObjsToProcess = append(combinedObjsToProcess, objsToProcess...)
	}

	if len(combinedObjsToProcess) == 0 {
		return fmt.Errorf("No objects found in templates %v", templatePaths)
	}

	if err := ApplyObjects(cl, s, username, userNS, combinedObjsToProcess); err != nil {
		return err
	}
	return nil
}

func ApplyObjects(cl client.Client, s *runtime.Scheme, username, userNS string, combinedObjsToProcess []applyclientlib.ToolchainObject) error {
	var objProcessors []<-chan error
	objChannel := distribute(combinedObjsToProcess)
	for i := 0; i < len(combinedObjsToProcess); i++ {
		objProcessors = append(objProcessors, startObjectProcessor(cl, s, userNS, objChannel))
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

func startObjectProcessor(cl client.Client, s *runtime.Scheme, userNS string, objSource <-chan applyclientlib.ToolchainObject) <-chan error {
	out := make(chan error)
	go func() {
		applycl := applyclientlib.NewApplyClient(cl, s)
		for obj := range objSource {
			out <- applyObject(applycl, userNS, obj)
			time.Sleep(100 * time.Millisecond)
		}
		close(out)
	}()
	return out
}

func applyObject(applycl *applyclientlib.ApplyClient, userNS string, obj applyclientlib.ToolchainObject) error {
	// enforce the creation of the objects in the `userNS` namespace
	m, err := meta.Accessor(obj.GetRuntimeObject())
	if err != nil {
		return err
	}
	m.SetNamespace(userNS)

	// retry the apply in case it fails due to errors like the following:
	// unable to create resource of kind: Deployment, version: v1: Operation cannot be fulfilled on clusterresourcequotas.quota.openshift.io "for-zippy-1882-deployments": the object has been modified; please apply your changes to the latest version and try again
	if err := k8swait.Poll(cfg.DefaultRetryInterval, 30*time.Second, func() (bool, error) {
		if _, applyErr := applycl.ApplyObject(obj.GetRuntimeObject()); applyErr != nil {
			return false, applyErr
		}
		return true, nil
	}); err != nil {
		return errors.Wrapf(err, "could not apply resource '%s' in namespace '%s'", obj.GetName(), userNS)
	}
	return nil
}

func getTemplateFromFile(s *runtime.Scheme, filename string) (*templatev1.Template, error) {
	content, err := ioutil.ReadFile(filename)
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
