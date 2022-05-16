module github.com/codeready-toolchain/toolchain-e2e

require (
	github.com/codeready-toolchain/api v0.0.0-20220420163009-01d30d6cedd9
	github.com/codeready-toolchain/toolchain-common v0.0.0-20220407172553-826188a0ce5d
	github.com/davecgh/go-spew v1.1.1
	github.com/fatih/color v1.12.0
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.4.0
	github.com/gofrs/uuid v3.3.0+incompatible
	github.com/google/go-cmp v0.5.6
	github.com/gorilla/websocket v1.4.2
	github.com/gosuri/uilive v0.0.4 // indirect
	github.com/gosuri/uiprogress v0.0.1
	github.com/gosuri/uitable v0.0.4
	github.com/hashicorp/go-multierror v1.1.1
	github.com/manifoldco/promptui v0.8.0
	// using latest commit from 'github.com/openshift/api@release-4.9'
	github.com/openshift/api v0.0.0-20211028023115-7224b732cc14
	github.com/operator-framework/api v0.13.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.12.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.32.1
	github.com/redhat-cop/operator-utils v1.3.3-0.20220121120056-862ef22b8cdf
	github.com/spf13/cobra v1.2.1
	github.com/spf13/viper v1.8.1
	github.com/stretchr/testify v1.7.0
	k8s.io/api v0.22.7
	k8s.io/apimachinery v0.22.7
	k8s.io/client-go v0.22.7
	k8s.io/kubectl v0.22.7
	k8s.io/metrics v0.22.7
	sigs.k8s.io/controller-runtime v0.10.3
)

replace github.com/codeready-toolchain/api => github.com/ranakan19/api v0.0.0-20220516122538-d90642f61eaa

go 1.16
