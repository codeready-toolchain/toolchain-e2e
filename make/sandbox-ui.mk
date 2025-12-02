DEVSANDBOX_DASHBOARD_NS := devsandbox-dashboard
IMAGE_PLATFORM ?= linux/amd64
OPENID_SECRET_NAME=openid-sandbox-public-client-secret
UI_ENVIRONMENT := ui-e2e-tests
SSO_USERNAME_READ := $(shell if [ -n "$(CI)" ]; then cat /usr/local/sandbox-secrets/SSO_USERNAME 2>/dev/null || echo ""; else echo "${SSO_USERNAME}"; fi)
SSO_PASSWORD_READ := $(shell if [ -n "$(CI)" ]; then cat /usr/local/sandbox-secrets/SSO_PASSWORD 2>/dev/null || echo ""; else echo "${SSO_PASSWORD}"; fi)

QUAY_NAMESPACE ?= codeready-toolchain-test

PUBLISH_UI ?= false
DEPLOY_UI ?= true

.PHONY: get-and-publish-devsandbox-dashboard
get-and-publish-devsandbox-dashboard:
ifneq (${UI_REPO_PATH},"")
    ifneq (${UI_REPO_PATH},)
		$(eval UI_REPO_PATH_PARAM = -ur ${UI_REPO_PATH})
    endif
endif
ifneq (${FORCED_TAG},"")
    ifneq (${FORCED_TAG},)
		$(eval FORCED_TAG_PARAM = -ft ${FORCED_TAG})
    endif
endif
	@echo "Publishing and installing the Developer Sandbox Dashboard"
	OPENID_SECRET_NAME=${OPENID_SECRET_NAME} DEVSANDBOX_DASHBOARD_NS=${DEVSANDBOX_DASHBOARD_NS}\
		scripts/ci/manage-devsandbox-dashboard.sh -pp ${PUBLISH_UI} ${UI_REPO_PATH_PARAM} -ds ${DATE_SUFFIX} -qn ${QUAY_NAMESPACE} ${FORCED_TAG_PARAM} -du ${DEPLOY_UI}

.PHONY: e2e-run-devsandbox-dashboard
e2e-run-devsandbox-dashboard: HOST_NS=$(shell oc get projects -l app=host-operator --output=name -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort | tail -n 1)
e2e-run-devsandbox-dashboard: RHDH=https://rhdh-${DEVSANDBOX_DASHBOARD_NS}.$(shell oc get ingress.config.openshift.io/cluster -o jsonpath='{.spec.domain}')
e2e-run-devsandbox-dashboard:
	$(eval PWGO_VER := $(shell grep -oE "playwright-go v\S+" go.mod | sed 's/playwright-go //g'))
	@echo "Installing Playwright CLI version: $(PWGO_VER)"
	go install github.com/playwright-community/playwright-go/cmd/playwright@$(PWGO_VER)
	@echo "Installing Firefox browser for Playwright..."
	$(GOPATH)/bin/playwright install firefox

	@echo "Running Developer Sandbox Dashboard setup e2e tests..."
	DEVSANDBOX_DASHBOARD_NS=${DEVSANDBOX_DASHBOARD_NS} go test "./test/e2e/sandbox-ui/setup" -v -timeout=10m -failfast
	
	@echo "Running Developer Sandbox Dashboard e2e tests in firefox..."
	@SSO_USERNAME=$(SSO_USERNAME_READ) SSO_PASSWORD=$(SSO_PASSWORD_READ) BASE_URL=${RHDH} BROWSER=firefox envsubst < deploy/sandbox-ui/ui-e2e-tests/.env > testsupport/sandbox-ui/.env
	go test "./test/e2e/sandbox-ui" -v -timeout=10m -failfast
	@oc delete usersignup $(SSO_USERNAME_READ) -n $(HOST_NS)

	@echo "The Developer Sandbox Dashboard e2e tests successfully finished"

.PHONY: test-devsandbox-dashboard-e2e
test-devsandbox-dashboard-e2e:
	$(MAKE) get-and-publish-devsandbox-dashboard e2e-run-devsandbox-dashboard ENVIRONMENT=${UI_ENVIRONMENT}

.PHONY: test-devsandbox-dashboard-e2e-local
test-devsandbox-dashboard-e2e-local: 
	$(MAKE) get-and-publish-devsandbox-dashboard e2e-run-devsandbox-dashboard UI_REPO_PATH=${PWD}/../devsandbox-dashboard ENVIRONMENT=${UI_ENVIRONMENT} PUBLISH_UI=true DEPLOY_UI=true

.PHONY: clean-devsandbox-dashboard
clean-devsandbox-dashboard: HOST_NS=$(shell oc get projects -l app=host-operator --output=name -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' | sort | tail -n 1)
clean-devsandbox-dashboard:
	@oc delete ns ${DEVSANDBOX_DASHBOARD_NS}
	@oc delete secret ${OPENID_SECRET_NAME} -n openshift-config
	@oc delete usersignup ${SSO_USERNAME} -n ${HOST_NS}


UNIT_TEST_IMAGE_NAME=devsandbox-dashboard-e2e-tests
UNIT_TEST_DOCKERFILE=build/devsandbox-dashboard/Dockerfile

# Build Developer Sandbox Dashboard e2e tests image using podman
.PHONY: build-devsandbox-dashboard-e2e-tests
build-devsandbox-dashboard-e2e-tests:
	@echo "building the $(UNIT_TEST_IMAGE_NAME) image with podman..."
	podman build --platform $(IMAGE_PLATFORM) -t $(UNIT_TEST_IMAGE_NAME) -f $(UNIT_TEST_DOCKERFILE) .

# Run Developer Sandbox Dashboard e2e tests image using podman
.PHONY: test-devsandbox-dashboard-in-container
test-devsandbox-dashboard-in-container: build-devsandbox-dashboard-e2e-tests
ifneq ($(UI_REPO_PATH),)
	$(eval FORCED_TAG := $(DATE_SUFFIX))
	$(eval ABS_UI_REPO_PATH := $(abspath $(UI_REPO_PATH)))
	@echo "Generated FORCED_TAG: $(FORCED_TAG)"
	@echo "Using UI_REPO_PATH: $(ABS_UI_REPO_PATH)"
	@echo "pushing Developer Dashboard image..."
	$(MAKE) get-and-publish-devsandbox-dashboard PUBLISH_UI=true DEPLOY_UI=false FORCED_TAG=$(FORCED_TAG) UI_REPO_PATH=$(UI_REPO_PATH)
else
	@echo "Skipping Developer Sandbox Dashboard publish - UI_REPO_PATH not set"
endif
	@echo "running the e2e tests in podman container..."
	podman run --platform $(IMAGE_PLATFORM) --rm \
	  -v $(KUBECONFIG):/root/.kube/config \
	  -e KUBECONFIG=/root/.kube/config \
	  -v ${PWD}:/root/toolchain-e2e \
	  -e E2E_REPO_PATH=/root/toolchain-e2e \
	  $(if $(ABS_UI_REPO_PATH),-v $(ABS_UI_REPO_PATH):/root/devsandbox-dashboard -e UI_REPO_PATH=/root/devsandbox-dashboard) \
	  $(if $(ABS_UI_REPO_PATH),-e FORCED_TAG=$(FORCED_TAG)) \
	  -e SSO_USERNAME=$(SSO_USERNAME) \
	  -e SSO_PASSWORD=$(SSO_PASSWORD) \
	  -e QUAY_NAMESPACE=$(QUAY_NAMESPACE) \
	  -e RUNNING_IN_CONTAINER=true \
	  -e DEPLOY_UI=true \
	  -e PUBLISH_UI=false \
	  -e OPENID_SECRET_NAME=$(OPENID_SECRET_NAME) \
	  -e DEVSANDBOX_DASHBOARD_NS=$(DEVSANDBOX_DASHBOARD_NS) \
	  $(UNIT_TEST_IMAGE_NAME)
