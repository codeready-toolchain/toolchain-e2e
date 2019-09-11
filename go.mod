module github.com/codeready-toolchain/toolchain-e2e

require (
	cloud.google.com/go v0.40.0 // indirect
	github.com/Azure/go-autorest v13.0.0+incompatible // indirect
	github.com/codeready-toolchain/api v0.0.0-20190910110833-66e1ab342d1e
	github.com/codeready-toolchain/toolchain-common v0.0.0-20190830131634-231d1bd08d5f
	github.com/go-openapi/spec v0.19.2 // indirect
	github.com/gobuffalo/envy v1.7.0 // indirect
	github.com/golangci/golangci-lint v1.17.1 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.9.2 // indirect
	github.com/mailru/easyjson v0.0.0-20190620125010-da37f6c1e481 // indirect
	github.com/openshift/api v3.9.0+incompatible
	github.com/operator-framework/operator-sdk v0.10.0
	github.com/satori/go.uuid v1.2.0
	github.com/spf13/pflag v1.0.3
	github.com/stretchr/testify v1.3.0
	go.opencensus.io v0.22.0 // indirect
	golang.org/x/tools v0.0.0-20190620191750-1fa568393b23 // indirect
	google.golang.org/genproto v0.0.0-20190620144150-6af8c5fc6601 // indirect
	google.golang.org/grpc v1.21.1 // indirect
	k8s.io/api v0.0.0-20190620073856-dcce3486da33
	k8s.io/apimachinery v0.0.0-20190620073744-d16981aedf33
	k8s.io/client-go v11.0.0+incompatible
	sigs.k8s.io/controller-runtime v0.1.12
	sigs.k8s.io/kubefed v0.1.0-rc2
)

// fix for unavailable git.apache.org https://status.apache.org/incidents/63030p4241xj
replace git.apache.org/thrift.git => github.com/apache/thrift v0.12.0

// Pinned to kubernetes-1.13.1
replace (
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
