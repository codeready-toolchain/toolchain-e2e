# Required Pre-installed Tools

* go 1.26.x (1.26.5 or higher)
* git
* operator-sdk 1.42.0+

> **Note:** Follow the installation instructions [here](https://sdk.operatorframework.io/docs/installation/#install-from-github-release). Make sure that the download URL (specified by the `OPERATOR_SDK_DL_URL` environment variable) is set to the correct version.

* sed
* yamllint
* jq
* podman
* opm v1.59.0+

> **Note:** To download the Operator Registry tool use either https://github.com/operator-framework/operator-registry/releases or https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp/. The version should correspond with the OpenShift version you are running. To confirm that the Operator Registry tool is installed correctly: `$ opm version`
