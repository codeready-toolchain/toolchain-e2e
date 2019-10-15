module github.com/codeready-toolchain/toolchain-e2e

require (
	cloud.google.com/go v0.46.3 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.6.0 // indirect
	github.com/codeready-toolchain/api v0.0.0-20191013054335-17f0782f9285
	github.com/codeready-toolchain/toolchain-common v0.0.0-20191008082920-e049a77c06ec
	github.com/gobuffalo/envy v1.7.1 // indirect
	github.com/gophercloud/gophercloud v0.3.0 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/openshift/api v3.9.1-0.20190730142803-0922aa5a655b+incompatible
	github.com/operator-framework/operator-sdk v0.10.0
	github.com/rogpeppe/go-internal v1.4.0 // indirect
	github.com/satori/go.uuid v1.2.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.4.0
	go.uber.org/multierr v1.2.0 // indirect
	golang.org/x/crypto v0.0.0-20190926180335-cea2066c6411 // indirect
	golang.org/x/sys v0.0.0-20190927073244-c990c680b611 // indirect
	golang.org/x/tools v0.0.0-20190927052746-69890759d905 // indirect
	google.golang.org/appengine v1.6.4 // indirect
	k8s.io/api v0.0.0-20190925180651-d58b53da08f5
	k8s.io/apiextensions-apiserver v0.0.0-20190927042040-728319705b32 // indirect
	k8s.io/apimachinery v0.0.0-20190927035529-0104e33c351d
	k8s.io/client-go v11.0.0+incompatible
	k8s.io/gengo v0.0.0-20190907103519-ebc107f98eab // indirect
	sigs.k8s.io/controller-runtime v0.2.2
	sigs.k8s.io/controller-tools v0.2.1 // indirect
	sigs.k8s.io/kubefed v0.1.0-rc2
)

// Pinned to kubernetes-1.13.1
replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.0.0+incompatible
	k8s.io/api => k8s.io/api v0.0.0-20181213150558-05914d821849
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20181213153335-0fe22c71c476
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20181127025237-2b1284ed4c93
	k8s.io/client-go => k8s.io/client-go v0.0.0-20181213151034-8d9ed539ba31
)

replace (
	github.com/coreos/prometheus-operator => github.com/coreos/prometheus-operator v0.29.0
	k8s.io/code-generator => k8s.io/code-generator v0.0.0-20181117043124-c2090bec4d9b
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20180711000925-0cf8f7e6ed1d
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.1.10
	sigs.k8s.io/controller-tools => sigs.k8s.io/controller-tools v0.1.11-0.20190411181648-9d55346c2bde
)

go 1.13
