# CodeReady Toolchain E2E Tests

[![Go Report Card](https://goreportcard.com/badge/github.com/codeready-toolchain/toolchain-e2e)](https://goreportcard.com/report/github.com/codeready-toolchain/toolchain-e2e)
[![GoDoc](https://godoc.org/github.com/codeready-toolchain/toolchain-e2e?status.png)](https://godoc.org/github.com/codeready-toolchain/toolchain-e2e)

This repo contains e2e tests for [host](https://github.com/codeready-toolchain/host-operator) and [member](https://github.com/codeready-toolchain/member-operator) operators of CodeReady Toolchain.

## Build

Requires Go version 1.26.x (1.26.5 or higher) - download for your development environment [here](https://golang.org/dl/).

This repository uses [Go modules](https://github.com/golang/go/wiki/Modules).

## Step by step guide - running in CodeReady Containers

Refer to [this guide](openshift_local.md) for detailed instructions on running the e2e tests in a local CodeReady Containers cluster.

## End-to-End Tests

The e2e tests are executed against host and member operators running in OpenShift. The operators are built from the [host-operator](https://github.com/codeready-toolchain/host-operator) and [member-operator](https://github.com/codeready-toolchain/member-operator) repositories.

Since the changes in the e2e repo sometimes require changes in some of the operator repositories at the same time, the logic that executes the e2e tests supports a feature of pairing the e2e PR with **one** other PR based on branch names.
Before the e2e tests are executed in openshift-ci, the logic automatically tries to pair a PR opened for this (toolchain-e2e) repository with a branch of the same name that potentially could exist in any of the developer's fork of the operator repositories.

For example, if a developer with GH account `cooljohn` opens a PR (for toolchain-e2e repo) from a branch `fix-reconcile`, then the logic checks if there is a branch `fix-reconcile` also in the `cooljohn/host-operator` and `cooljohn/member-operator` forks.
Let's say that `cooljohn/host-operator` contains such a branch but `cooljohn/member-operator` doesn't, then the logic:

1. clones latest changes of both repos [codeready-toolchain/host-operator](https://github.com/codeready-toolchain/host-operator) and [codeready-toolchain/member-operator](https://github.com/codeready-toolchain/member-operator)
2. fetches the `fix-reconcile` branch from `cooljohn/host-operator` fork
3. merges `master` branch with the changes from `fix-reconcile` branch inside of `host-operator` repo
4. builds images from the merge branch of `host-operator` repo and from `master` branch of `member-operator` repo & deploys them to OpenShift
5. runs e2e tests taken from the opened PR

It would work analogically also for the case when none of the repositories contain the branch name. However, if **both** repositories contain the branch name then it will result in an error.
This is by design because OLM does not ensure that both the host-operator and member-operator will be updated at the same time in production. Prohibiting PR pairing with more than one repo helps ensure they do not depend on one another and can be updated separately.

If you still don't know what to do with e2e tests in some use-cases, go to the [What To Do](#what-to-do) section where all use-cases are covered.

> **Note:** You can also pair a toolchain-e2e PR with a PR from the [ksctl](https://github.com/kubesaw/ksctl) repository, but this is one-directional only (e2e tests are not yet executed in the `ksctl` repo). To rely on a locally installed version of the `ksctl` binary, set `USE_INSTALLED_KSCTL=true`. If you want to build and use the `ksctl` binary from your local `kubesaw/ksctl` repository, set the `KSCTL_REPO_PATH` variable to point to your `kubesaw/ksctl` folder.

### Running Locally

**Prerequisites:**

* Install the [required tools](required_tools.md).
* Configure [your quay account for dev deployment](quay.md).

#### Running in a Development Environment

See the procedure to install the Dev Sandbox in a development environment [here](https://kubesaw.github.io/contributing/).

### Running End-to-End Tests

1. [Configure your quay account for dev deployment](quay.md)

Although the e2e tests are in the separated repository than the actual operators are, it's still possible to run them against the current code that is at HEAD of the operator repositories.
There are multiple Makefile targets that will execute the e2e tests, they just differ in where the operators' code is taken from:

* `make test-e2e` - this target clones the latest changes from both repos [host-operator](https://github.com/codeready-toolchain/host-operator) and [member-operator](https://github.com/codeready-toolchain/member-operator), builds images from the master, deploys to OpenShift and runs e2e tests against them.
* `make test-e2e-local` - this target doesn't clone anything, but it builds operator images from the directories `../host-operator` and `../member-operator`. These images deploys to OpenShift and runs e2e tests against them.
* `make test-e2e-member-local` - this target clones only the [host-operator](https://github.com/codeready-toolchain/host-operator) repo and builds an image from it. For member-operator, it builds the image from `../member-operator` directory. These images deploys to OpenShift and runs e2e tests against them.
* `make test-e2e-host-local` - this target clones only the [member-operator](https://github.com/codeready-toolchain/member-operator) repo and builds an image from it. For host-operator, it builds the image from `../host-operator` directory. These images deploys to OpenShift and runs e2e tests against them.

The e2e tests will take care of creating all needed namespaces with random names (or see below for enforcing some specific namespace names).
It will also create all required CRDs, role and role bindings for the service accounts, build the container images for both operators and push them to the OpenShift container registry. Finally, it will deploy the operators and run the tests using the operator-sdk.

> **Note:** You can override the default namespace names where the end-to-end tests are going to be executed - eg.: `make test-e2e HOST_NS=my-host MEMBER_NS=my-member`.

> **Note:** You can disable SSL/TLS certificate verification in tests setting the `DISABLE_KUBE_CLIENT_TLS_VERIFY` variable to `true` - eg.: `make test-e2e DISABLE_KUBE_CLIENT_TLS_VERIFY=true`. This flag helps when you test in clusters using Self-Signed Certificates.

> **Note:** You can specify a regular expression to selectively run particular test cases by setting the `TESTS_RUN_FILTER_REGEXP` variable. eg.: `make test-e2e TESTS_RUN_FILTER_REGEXP="TestSetupMigration"`. For more information see the [go test -run documentation](https://pkg.go.dev/cmd/go#hdr-Testing_flags).

> **Note:** You should not override `SECOND_MEMBER_MODE` in test-e2e, since the e2e tests require a second member operator.

### Running/Debugging e2e tests from your IDE

In order to run/debug tests from your IDE you'll need to export some required env variables, those will be used by the test framework to interact with the operator namespaces and the other toolchain resources in you cluster.
Following snippet of code should be TEMPORARILY added at the top of the test you want to run/debug from your IDE:

```go
os.Setenv("MEMBER_NS","toolchain-member-18161051")
// `SECOND_MEMBER_MODE` should be set to true, since the e2e tests require a second member operator.
os.Setenv("SECOND_MEMBER_MODE","true")
os.Setenv("MEMBER_NS_2","toolchain-member2-18161051")
os.Setenv("HOST_NS","toolchain-host-18161051")
os.Setenv("REGISTRATION_SERVICE_NS","toolchain-host-18161051")
os.Setenv("KUBECONFIG", "~/aws-cluster-test/my-devsandbox/auth/kubeconfig")
```

Example of test case code containing the debugging env variables:

```go
package parallel

import (
	"context"
	"os"
	"testing"
)

func TestCreateSpaceRequest(t *testing.T) {
	os.Setenv("MEMBER_NS","toolchain-member-18161051")
	os.Setenv("SECOND_MEMBER_MODE","true")
	os.Setenv("MEMBER_NS_2","toolchain-member2-18161051")
	os.Setenv("HOST_NS","toolchain-host-18161051")
	os.Setenv("REGISTRATION_SERVICE_NS","toolchain-host-18161051")
	os.Setenv("KUBECONFIG", "~/aws-cluster-test/my-devsandbox/auth/kubeconfig")
	// some more code here ...

	t.Run("create space request", func(t *testing.T) {
        // test case implementation here ...
....
```

> **Note:** Replace the values with the ones from your dev/test environment and REMEMBER TO REMOVE THE SNIPPET BEFORE COMMITTING THE CODE OR OPENING A PR IN GH :)

#### What To Do

If you are still confused by the different e2e/operator location, execution and branch pairing, see the following cases and needed steps:

* **Working locally:**
  * **Need to verify changes in e2e tests against the latest version of both operators:**
    * run `make test-e2e`
  * **You are working in both repos `toolchain-e2e` and `member-operator`, so you need to run e2e tests against your current code located in `../member-operator` directory:**
    * run `make test-e2e-member-local`
  * **You are working in both repos `toolchain-e2e` and `host-operator`, so you need to run e2e tests against your current code located in `../host-operator` directory:**
    * run `make test-e2e-host-local`
  * **You are working in all three repos `toolchain-e2e`, `host-operator` and `member-operator`, so you need to run e2e tests against your current code located in both directories `../host-operator` and `../member-operator`:**
    * run `make test-e2e-local`

* **Creating PRs:**
  * **Your PR doesn't need any changes in [host-operator](https://github.com/codeready-toolchain/host-operator) repo nor [member-operator](https://github.com/codeready-toolchain/member-operator) repo:**
    1. check the name of a branch you are going to create a PR for
    2. make sure that your forks of both repos ([host-operator](https://github.com/codeready-toolchain/host-operator) and [member-operator](https://github.com/codeready-toolchain/member-operator)) don't contain a branch with the same name
    3. create a PR
  * **Your PR requires changes in [host-operator](https://github.com/codeready-toolchain/host-operator) repo but not in [member-operator](https://github.com/codeready-toolchain/member-operator) repo:**
    1. check the name of a branch you are going to create a PR for
    2. create a branch with the same name within your fork of [host-operator](https://github.com/codeready-toolchain/host-operator) repo and put all necessary changes there
    3. make sure that your fork of [member-operator](https://github.com/codeready-toolchain/member-operator) repo doesn't contain a branch with the same name
    4. push all changes into both forks of the repositories [toolchain-e2e](https://github.com/codeready-toolchain/toolchain-e2e) and [host-operator](https://github.com/codeready-toolchain/host-operator)
    5. create a PR for [toolchain-e2e](https://github.com/codeready-toolchain/toolchain-e2e)
    6. create a PR for [host-operator](https://github.com/codeready-toolchain/host-operator)
  * **Your PR requires changes in [member-operator](https://github.com/codeready-toolchain/member-operator) repo but not in [host-operator](https://github.com/codeready-toolchain/host-operator) repo:**
    * See the previous case and just swap member-operator and host-operator.
  * **Your PR requires changes in both repos [host-operator](https://github.com/codeready-toolchain/host-operator) and [member-operator](https://github.com/codeready-toolchain/member-operator):**
    * This is prohibited and will result in an error like `ERROR WHILE TRYING TO PAIR PRs` in the CI build. See the reasoning behind this in the [End-to-End Tests](#end-to-end-tests) section.

## Deploying End-to-End Resources Without Running Tests

All e2e resources (host operator, member operator, registration-service, CRDs, etc) can be deployed without running tests:

* `make dev-deploy-e2e-local` - deploys the same resources as `make test-e2e-local` in dev environment but doesn't run tests.

* `make dev-deploy-e2e` - deploys the same resources as `make test-e2e` in dev environment but doesn't run tests.

* `make deploy-single-member-e2e-latest` - deploys the same resources (using the latest and greatest images of Toolchain operators) as `make test-e2e` but with only one member and doesn't run tests.

> **Note:** By default these targets deploy resources to `toolchain-host-operator` and `toolchain-member-operator` namespaces.

> **Note:** If running in CodeReady Containers `eval $(crc oc-env)` is required.

> **Note:** By default, `SECOND_MEMBER_MODE` is set to false.

## How to Test Mailgun/Twilio Notifications in a Dev Environment

* Get a cluster and setup the following env vars
  * `export QUAY_NAMESPACE=<your-quay-namespace>`
  * `export KUBECONFIG=<location-to-kubeconfig>`
* Run `podman login quay.io`
* Create [IdP](https://github.com/codeready-toolchain/toolchain-infra/tree/master/config/oauth)
* If you need to change any of the default configuration, modify the ToolchainConfig in [deploy/host-operator/dev/toolchainconfig.yaml](https://github.com/codeready-toolchain/toolchain-e2e/blob/master/deploy/host-operator/dev/toolchainconfig.yaml)
* To set working notification/verification secrets, modify them in [deploy/host-operator/dev/secrets.yaml](https://github.com/codeready-toolchain/toolchain-e2e/blob/master/deploy/host-operator/dev/secrets.yaml)
* Run `make dev-deploy-e2e-local`
* Go to the registration-service link and sign in
* Click on the `Get Started With CodeReady Toolchain` button
* Approve your usersignup found on the `<username>-host-operator` namespace
