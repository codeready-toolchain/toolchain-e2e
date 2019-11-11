module github.com/codeready-toolchain/toolchain-e2e

require (
	cloud.google.com/go v0.46.3 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.6.0 // indirect
	github.com/codeready-toolchain/api v0.0.0-20191107090146-e29aacb17012
	github.com/codeready-toolchain/toolchain-common v0.0.0-20191107144135-e8ba3faab2c8
	github.com/gobuffalo/envy v1.7.1 // indirect
	github.com/gophercloud/gophercloud v0.3.0 // indirect
	github.com/openshift/api v3.9.1-0.20190730142803-0922aa5a655b+incompatible
	github.com/operator-framework/operator-sdk v0.11.0
	github.com/rogpeppe/go-internal v1.4.0 // indirect
	github.com/satori/go.uuid v1.2.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.4.0
	go.uber.org/multierr v1.2.0 // indirect
	golang.org/x/crypto v0.0.0-20190926180335-cea2066c6411 // indirect
	golang.org/x/sys v0.0.0-20190927073244-c990c680b611 // indirect
	golang.org/x/tools v0.0.0-20190927052746-69890759d905 // indirect
	google.golang.org/appengine v1.6.4 // indirect
	k8s.io/api v0.0.0
	k8s.io/apimachinery v0.0.0
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	sigs.k8s.io/controller-runtime v0.2.2
	sigs.k8s.io/kubefed v0.1.0-rc6.0.20191023070212-24d45e9f4f15
)

// Pinned to kubernetes-1.14.1
replace (
	k8s.io/api => k8s.io/api v0.0.0-20190409021203-6e4e0e4f393b
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20190409022649-727a075fdec8
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20190404173353-6a84e37a896d
	k8s.io/apiserver => k8s.io/apiserver v0.0.0-20190409021813-1ec86e4da56c
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.0.0-20190409023024-d644b00f3b79
	k8s.io/client-go => k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.0.0-20190409023720-1bc0c81fa51d
	k8s.io/code-generator => k8s.io/code-generator v0.0.0-20190311093542-50b561225d70
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.0.0-20190409022021-00b8e31abe9d
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20190510232812-a01b7d5d6c22
	k8s.io/kubernetes => k8s.io/kubernetes v1.14.1
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.0.0+incompatible
	github.com/codeready-toolchain/api => github.com/sbryzak/api v0.0.0-20191108031415-fabe524427b3
)

go 1.13
