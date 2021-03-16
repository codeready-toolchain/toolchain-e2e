package resources

import (
	"fmt"
	"io/ioutil"

	applycl "github.com/codeready-toolchain/toolchain-common/pkg/client"
	"github.com/codeready-toolchain/toolchain-common/pkg/template"

	templatev1 "github.com/openshift/api/template/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var tmpl *templatev1.Template

func CreateFromTemplate(cl client.Client, clientConfig *rest.Config, s *runtime.Scheme, templatePath, username string) error {
	// get the template from the file
	if tmpl == nil {
		var err error
		if tmpl, err = getTemplateFromFile(s, templatePath); err != nil {
			return fmt.Errorf("invalid template file: '%s'", templatePath)
		}
	}
	userNS := fmt.Sprintf("%s-stage", username)
	// waiting for rolebinding to exists before creating resources on behalf of the user, so we don't get
	// errors such as:
	// `User "<username>" cannot get resource "deployments" in API group "apps" in the namespace "<username>-stage"`
	if err := WaitForRoleBinding(cl, userNS, "user-edit"); err != nil {
		return err
	}

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
	clientConfig.Impersonate = rest.ImpersonationConfig{
		UserName: username,
	}
	userCl, err := client.New(clientConfig, client.Options{Scheme: s})
	if err != nil {
		return err
	}
	applycl := applycl.NewApplyClient(userCl, s)
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
	_, _, err = decoder.Decode([]byte(content), nil, tmpl)
	return tmpl, err
}
