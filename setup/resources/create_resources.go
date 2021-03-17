package resources

import (
	"fmt"
	"io/ioutil"

	applycl "github.com/codeready-toolchain/toolchain-common/pkg/client"
	"github.com/codeready-toolchain/toolchain-common/pkg/template"
	"github.com/pkg/errors"

	templatev1 "github.com/openshift/api/template/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var tmpl *templatev1.Template

func CreateFromTemplateFile(cl client.Client, s *runtime.Scheme, templatePath, username string) error {
	// get the template from the file
	if tmpl == nil {
		var err error
		if tmpl, err = getTemplateFromFile(s, templatePath); err != nil {
			return errors.Wrapf(err, "invalid template file: '%s'", templatePath)
		}
	}

	userNS := fmt.Sprintf("%s-stage", username)
	// waiting for each namespace here prevents some edge cases where the setup job can progress beyond the usersignup job and fail with a timeout
	if err := WaitForNamespace(cl, userNS); err != nil {
		return err
	}
	processor := template.NewProcessor(s)
	objs, err := processor.Process(tmpl.DeepCopy(), map[string]string{
		"NAMESPACE": userNS,
	})
	if err != nil {
		return err
	}
	applycl := applycl.NewApplyClient(cl, s)
	for _, obj := range objs {
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
	_, kind, err := decoder.Decode([]byte(content), nil, tmpl)
	if kind.Kind == "Template" { // expect an OpenShift template
		return tmpl, err
	}
	return nil, fmt.Errorf("wrong kind of object in the template file: '%s'", kind)

}
