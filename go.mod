module github.com/codeready-toolchain/toolchain-e2e

require (
	github.com/codeready-toolchain/api v0.0.0-20200730091449-94992fef0c87
	github.com/codeready-toolchain/toolchain-common v0.0.0-20200717175027-3b8c7e68e409
	github.com/gobuffalo/envy v1.7.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e // indirect
	github.com/openshift/api v3.9.1-0.20190924102528-32369d4db2ad+incompatible
	github.com/operator-framework/operator-sdk v0.17.1
	github.com/satori/go.uuid v1.2.0
	github.com/spf13/cobra v1.0.0 // indirect
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.5.1
	golang.org/x/tools v0.0.0-20200502202811-ed308ab3e770 // indirect
	gopkg.in/check.v1 v1.0.0-20200227125254-8fa46927fb4f // indirect
	honnef.co/go/tools v0.0.1-2020.1.3 // indirect
	k8s.io/api v0.17.4
	k8s.io/apimachinery v0.17.4
	k8s.io/client-go v12.0.0+incompatible
	sigs.k8s.io/controller-runtime v0.5.2
)

replace github.com/codeready-toolchain/toolchain-common => github.com/MatousJobanek/toolchain-common v0.0.0-20200804075956-19d605945d02

replace github.com/codeready-toolchain/api => github.com/MatousJobanek/api v0.0.0-20200803150704-20052fba4e3f

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.0.0+incompatible
	github.com/openshift/api => github.com/openshift/api v0.0.0-20200414152312-3e8f22fb0b56 // Using 'github.com/openshift/api@release-4.4'
	k8s.io/client-go => k8s.io/client-go v0.17.4 // Required by prometheus-operator
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20200204173128-addea2498afe // avoids case-insensitive import collision: "github.com/googleapis/gnostic/openapiv2" and "github.com/googleapis/gnostic/OpenAPIv2"
)

go 1.13
