package user

import (
	"bytes"
	"context"
	"io"
	"time"

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

var interval = time.Second * 1
var timeout = time.Second * 60

// CreateResourcesFromTemplate uses the provided template to create resources in the provided namespace
func CreateResourcesFromTemplate(config *rest.Config, templateData []byte, namespace string) error {
	c, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	err = waitForNamespace(c, namespace)
	if err != nil {
		return err
	}

	dd, err := dynamic.NewForConfig(config)
	if err != nil {
		return err
	}

	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(templateData), 100)
	for {
		var rawObj runtime.RawExtension
		if err = decoder.Decode(&rawObj); err != nil {
			break
		}

		obj, gvk, err := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).Decode(rawObj.Raw, nil, nil)
		unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			return err
		}

		unstructuredObj := &unstructured.Unstructured{Object: unstructuredMap}

		gr, err := restmapper.GetAPIGroupResources(c.Discovery())
		if err != nil {
			return err
		}

		mapper := restmapper.NewDiscoveryRESTMapper(gr)
		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}

		unstructuredObj.SetNamespace(namespace)
		dri := dd.Resource(mapping.Resource).Namespace(unstructuredObj.GetNamespace())

		if _, err := dri.Create(context.TODO(), unstructuredObj, metav1.CreateOptions{}); err != nil {
			return err
		}
	}
	if err != io.EOF {
		return err
	}
	return nil
}

func waitForNamespace(c *kubernetes.Clientset, namespace string) error {
	err := wait.Poll(interval, timeout, func() (done bool, err error) {
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
