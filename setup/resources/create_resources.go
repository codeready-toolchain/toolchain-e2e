package resources

import (
	"fmt"
	"io/ioutil"

	applycl "github.com/codeready-toolchain/toolchain-common/pkg/client"
	"github.com/codeready-toolchain/toolchain-common/pkg/template"
	"github.com/pkg/errors"

	templatev1 "github.com/openshift/api/template/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var tmpls map[string]*templatev1.Template = make(map[string]*templatev1.Template)

func CreateFromTemplateFiles(cl client.Client, s *runtime.Scheme, username string, templatePaths []string) error {
	for _, path := range templatePaths {
		if err := CreateFromTemplateFile(cl, s, username, path); err != nil {
			return err
		}
	}
	return nil
}

func CreateFromTemplateFile(cl client.Client, s *runtime.Scheme, username, templatePath string) error {
	// get the template from the file if it hasn't been processed already
	if _, ok := tmpls[templatePath]; !ok {
		var err error
		if tmpls[templatePath], err = getTemplateFromFile(s, templatePath); err != nil {
			return errors.Wrapf(err, "invalid template file: '%s'", templatePath)
		}
	}
	tmpl := tmpls[templatePath]

	userNS := fmt.Sprintf("%s-stage", username)
	// waiting for each namespace here prevents some edge cases where the setup job can progress beyond the usersignup job and fail with a timeout
	if err := WaitForNamespace(cl, userNS); err != nil {
		return err
	}
	processor := template.NewProcessor(s)
	objs, err := processor.Process(tmpl.DeepCopy(), map[string]string{})
	if err != nil {
		return err
	}
	applycl := applycl.NewApplyClient(cl, s)
	for _, obj := range objs {
		// enforce the creation of the objects in the `userNS` namespace
		m, err := meta.Accessor(obj.GetRuntimeObject())
		if err != nil {
			return err
		}
		m.SetNamespace(userNS)
		if _, err := applycl.ApplyObject(obj.GetRuntimeObject()); err != nil {
			return err
		}
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
