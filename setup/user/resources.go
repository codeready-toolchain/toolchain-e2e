package user

import (
	"bytes"
	"context"
	"io"
	"sync"

	cfg "github.com/codeready-toolchain/toolchain-e2e/setup/configuration"

	"github.com/hashicorp/go-multierror"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/util/wait"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
)

type templateProcessor struct {
	config    *rest.Config
	namespace string
}

// CreateResourcesFromTemplate uses the provided template to create resources in the provided namespace
func CreateResourcesFromTemplate(config *rest.Config, namespace string, templateData []byte, resourceProcessorsCount int) error {

	tp := templateProcessor{config: config, namespace: namespace}
	clSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	err = waitForNamespace(clSet, namespace)
	if err != nil {
		return err
	}

	// decode template
	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(templateData), 100)
	var objsToProcess []runtime.RawExtension
	for {
		var rawObj runtime.RawExtension
		if err = decoder.Decode(&rawObj); err != nil {
			break
		}
		objsToProcess = append(objsToProcess, rawObj)
	}
	if err != io.EOF {
		return err
	}

	// feed objects to be processed
	in := distribute(objsToProcess)
	var objProcessors []<-chan error
	for i := 0; i < resourceProcessorsCount; i++ {
		objProcessors = append(objProcessors, tp.multiObjectProcessor(in))
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

func distribute(objs []runtime.RawExtension) <-chan runtime.RawExtension {
	out := make(chan runtime.RawExtension)
	go func() {
		for _, obj := range objs {
			out <- obj
		}
		close(out)
	}()
	return out
}

func (p templateProcessor) multiObjectProcessor(objSource <-chan runtime.RawExtension) <-chan error {
	out := make(chan error)
	go func() {
		for rawObj := range objSource {
			out <- p.processRawObject(rawObj)
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

func (p templateProcessor) processRawObject(rawObj runtime.RawExtension) error {

	clSet, err := kubernetes.NewForConfig(p.config)
	if err != nil {
		return err
	}

	dynamicCl, err := dynamic.NewForConfig(p.config)
	if err != nil {
		return err
	}

	obj, gvk, err := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).Decode(rawObj.Raw, nil, nil)
	unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return err
	}

	unstructuredObj := &unstructured.Unstructured{Object: unstructuredMap}

	gr, err := restmapper.GetAPIGroupResources(clSet.Discovery())
	if err != nil {
		return err
	}

	mapper := restmapper.NewDiscoveryRESTMapper(gr)
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return err
	}

	unstructuredObj.SetNamespace(p.namespace)
	dri := dynamicCl.Resource(mapping.Resource).Namespace(unstructuredObj.GetNamespace())

	_, err = dri.Create(context.TODO(), unstructuredObj, metav1.CreateOptions{})
	return err
}

func waitForNamespace(c *kubernetes.Clientset, namespace string) error {
	err := wait.Poll(cfg.DefaultRetryInterval, cfg.DefaultTimeout, func() (done bool, err error) {
		_, err = c.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})

	return err
}
