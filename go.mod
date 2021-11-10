module github.com/codeready-toolchain/toolchain-e2e

require (
	github.com/codeready-toolchain/api v0.0.0-20211018173325-31068bb3f4ac
	github.com/codeready-toolchain/toolchain-common v0.0.0-20210928071424-2ace1784e8d2
	github.com/davecgh/go-spew v1.1.1
	github.com/fatih/color v1.10.0
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.4.0
	github.com/gofrs/uuid v3.3.0+incompatible
	github.com/google/go-cmp v0.5.5
	github.com/gosuri/uilive v0.0.4 // indirect
	github.com/gosuri/uiprogress v0.0.1
	github.com/gosuri/uitable v0.0.4
	github.com/hashicorp/go-multierror v1.1.0
	github.com/manifoldco/promptui v0.8.0
	// using latest commit from 'github.com/openshift/api@release-4.7'
	github.com/openshift/api v0.0.0-20210428205234-a8389931bee7
	github.com/operator-framework/api v0.9.2
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.28.0
	github.com/redhat-cop/operator-utils v1.1.3-0.20210602122509-2eaf121122d2
	github.com/sergi/go-diff v1.1.0
	github.com/sirupsen/logrus v1.7.0 // indirect
	github.com/spf13/cobra v1.1.3
	github.com/spf13/viper v1.7.1
	github.com/stretchr/testify v1.7.0
	honnef.co/go/tools v0.0.1-2020.1.6 // indirect
	k8s.io/api v0.20.2
	k8s.io/apimachinery v0.20.2
	k8s.io/client-go v0.20.2
	k8s.io/kubectl v0.20.2
	k8s.io/metrics v0.20.2
	sigs.k8s.io/controller-runtime v0.8.3
)

go 1.16
