package resources

import (
	"fmt"

	applyclientlib "github.com/codeready-toolchain/toolchain-common/pkg/client"
	ctemplate "github.com/codeready-toolchain/toolchain-common/pkg/template"
	"github.com/codeready-toolchain/toolchain-e2e/setup/templates"
	"github.com/codeready-toolchain/toolchain-e2e/setup/wait"
	"github.com/pkg/errors"

	templatev1 "github.com/openshift/api/template/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var tmpls map[string]*templatev1.Template = make(map[string]*templatev1.Template)

func CreateUserResourcesFromTemplateFiles(cl client.Client, s *runtime.Scheme, username string, templatePaths []string) error {
	userNS := fmt.Sprintf("%s-stage", username)
	combinedObjsToProcess := []applyclientlib.ToolchainObject{}
	for _, templatePath := range templatePaths {
		// get the template from the file if it hasn't been processed already
		if _, ok := tmpls[templatePath]; !ok {
			var err error
			if tmpls[templatePath], err = templates.GetTemplateFromFile(s, templatePath); err != nil {
				return errors.Wrapf(err, "invalid template file: '%s'", templatePath)
			}
		}
		tmpl := tmpls[templatePath]

		// waiting for each namespace here prevents some edge cases where the setup job can progress beyond the usersignup job and fail with a timeout
		if err := wait.ForNamespace(cl, userNS); err != nil {
			return err
		}
		processor := ctemplate.NewProcessor(s)
		objsToProcess, err := processor.Process(tmpl.DeepCopy(), map[string]string{})
		if err != nil {
			return err
		}
		combinedObjsToProcess = append(combinedObjsToProcess, objsToProcess...)
	}

	if len(combinedObjsToProcess) == 0 {
		return fmt.Errorf("No objects found in templates %v", templatePaths)
	}

	if err := templates.ApplyObjectsConcurrently(cl, s, combinedObjsToProcess, templates.NamespaceModifier(userNS)); err != nil {
		return err
	}
	return nil
}
