SANDBOX_UI_NS := sandbox-ui
SANDBOX_PLUGIN_IMAGE_NAME := sandbox-rhdh-plugin
TAG := latest
PLATFORM ?= linux/amd64
RHDH_PLUGINS_DIR ?= $(TMPDIR)rhdh-plugins
AUTH_FILE := /tmp/auth.json
IMAGE_TO_PUSH_IN_QUAY ?= quay.io/$(QUAY_NAMESPACE)/sandbox-rhdh-plugin:$(TAG)
OPENID_SECRET_NAME=openid-sandbox-public-client-secret
PUSH_SANDBOX_IMAGE ?= true
UI_ENVIRONMENT := ui-e2e-tests

.PHONY: deploy-sandbox-ui
deploy-sandbox-ui: REGISTRATION_SERVICE_API=https://$(shell oc get route registration-service -n ${HOST_NS} -o custom-columns=":spec.host" | tr -d '\n')/api/v1
deploy-sandbox-ui: HOST_OPERATOR_API=https://$(shell oc get route api -n ${HOST_NS} -o custom-columns=":spec.host" | tr -d '\n')
deploy-sandbox-ui: RHDH=https://rhdh-${SANDBOX_UI_NS}.$(shell oc get ingress.config.openshift.io/cluster -o jsonpath='{.spec.domain}')
deploy-sandbox-ui:
	$(MAKE) check-sso-credentials
	@echo "sandbox ui will be deployed in '${SANDBOX_UI_NS}' namespace"
	$(MAKE) create-namespace SANDBOX_UI_NS=${SANDBOX_UI_NS}
ifeq ($(PUSH_SANDBOX_IMAGE),true)
	$(MAKE) push-sandbox-plugin
endif
	kustomize build deploy/sandbox-ui/e2e-tests | REGISTRATION_SERVICE_API=${REGISTRATION_SERVICE_API} \
			HOST_NS=${HOST_NS} \
			HOST_OPERATOR_API=${HOST_OPERATOR_API} \
			SANDBOX_UI_NS=${SANDBOX_UI_NS} \
			SANDBOX_PLUGIN_IMAGE=${IMAGE_TO_PUSH_IN_QUAY} \
			RHDH=${RHDH} envsubst | oc apply -f -
	$(MAKE) configure-oauth-idp
	@echo "restarting registration-service to apply toolchainconfig changes"
	@oc -n ${HOST_NS} rollout restart deploy/registration-service
	@oc -n ${SANDBOX_UI_NS} rollout status deploy/rhdh
	@echo "Developer Sandbox UI running at ${RHDH}"


check-sso-credentials:
	@echo "checking SSO credentials..."
	@if [ -z "$$SSO_USERNAME" ] || [ -z "$$SSO_PASSWORD" ]; then \
		echo "SSO_USERNAME or SSO_PASSWORD not set"; \
		exit 1; \
	fi
	@status=$$(curl -s -o /dev/null -w "%{http_code}" \
	  -X POST "https://sso.devsandbox.dev/auth/realms/sandbox-dev/protocol/openid-connect/token" \
	  -d "grant_type=password" \
	  -d "client_id=sandbox-public" \
	  -d "username=$$SSO_USERNAME" \
	  -d "password=$$SSO_PASSWORD"); \
	if [ "$$status" != "200" ]; then \
	  echo "failed trying to login to 'https://sso.devsandbox.dev/auth/realms/sandbox-dev' ($$status) — check your SSO credentials."; \
	  exit 1; \
	fi

configure-oauth-idp:
	@echo "configuring DevSandbox identity provider"
	@oc create secret generic ${OPENID_SECRET_NAME} \
		--from-literal=clientSecret=dummy \
		--namespace=openshift-config
	OPENID_SECRET_NAME=${OPENID_SECRET_NAME} envsubst < deploy/sandbox-ui/e2e-tests/oauth-idp-patch.yaml | \
		oc patch oauths.config.openshift.io/cluster --type=merge --patch-file=/dev/stdin

create-namespace:
	@if ! oc get project ${SANDBOX_UI_NS} >/dev/null 2>&1; then \
		echo "Creating namespace ${SANDBOX_UI_NS}"; \
		oc new-project ${SANDBOX_UI_NS} >/dev/null 2>&1 || true; \
	else \
		echo "Namespace ${SANDBOX_UI_NS} already exists"; \
	fi
	@oc project ${SANDBOX_UI_NS} >/dev/null 2>&1


.PHONY: get-rhdh-plugins
get-rhdh-plugins:
ifeq ($(strip $(RHDH_PLUGINS_DIR)), $(TMPDIR)rhdh-plugins)
	echo "using rhdh-plugins repo from master"
	@$(MAKE) clone-rhdh-plugins
else
	echo "using local rhdh-plugins repo: ${RHDH_PLUGINS_DIR}"
endif

.PHONY: clone-rhdh-plugins
clone-rhdh-plugins:
	rm -rf ${RHDH_PLUGINS_DIR}; \
	git clone https://github.com/redhat-developer/rhdh-plugins $(RHDH_PLUGINS_DIR) && \
	echo "cloned to $(RHDH_PLUGINS_DIR)"

.PHONY: push-sandbox-plugin
push-sandbox-plugin:
	$(MAKE) get-rhdh-plugins
	cd $(RHDH_PLUGINS_DIR)/workspaces/sandbox && \
	rm -rf plugins/sandbox/dist-dynamic && \
	rm -rf red-hat-developer-hub-backstage-plugin-sandbox && \
	yarn install && \
	npx @janus-idp/cli@3.3.1 package package-dynamic-plugins \
      --tag $(IMAGE_TO_PUSH_IN_QUAY) \
      --platform $(PLATFORM) && \
  podman push $(IMAGE_TO_PUSH_IN_QUAY)

.PHONY: clean-sandbox-ui
clean-sandbox-ui:
	@oc delete ns ${SANDBOX_UI_NS}
	@oc delete secret ${OPENID_SECRET_NAME} -n openshift-config
	@oc delete usersignup ${SSO_USERNAME} -n ${HOST_NS}

.PHONY: e2e-run-sandbox-ui
e2e-run-sandbox-ui: RHDH=https://rhdh-${SANDBOX_UI_NS}.$(shell oc get ingress.config.openshift.io/cluster -o jsonpath='{.spec.domain}')
e2e-run-sandbox-ui:
	@echo "Running Developer Sandbox UI setup e2e tests..."
	SANDBOX_UI_NS=${SANDBOX_UI_NS} go test "./test/e2e/sandbox-ui/setup" -v -timeout=10m -failfast
	
	@echo "Running Developer Sandbox UI e2e tests in firefox..."
	SSO_USERNAME=${SSO_USERNAME} SSO_PASSWORD=${SSO_PASSWORD} BASE_URL=${RHDH} BROWSER=firefox envsubst < deploy/sandbox-ui/e2e-tests/.env > testsupport/sandbox-ui/.env
	go test "./test/e2e/sandbox-ui" -v -timeout=10m -failfast
	@oc delete usersignup ${SSO_USERNAME} -n ${HOST_NS}

	@echo "The Developer Sandbox UI e2e tests successfully finished"


.PHONY: test-ui-e2e
test-ui-e2e:
	$(MAKE) prepare-and-deploy-e2e deploy-sandbox-ui e2e-run-sandbox-ui ENVIRONMENT=${UI_ENVIRONMENT}

.PHONY: test-ui-e2e-local
test-ui-e2e-local:
	$(MAKE) prepare-and-deploy-e2e deploy-sandbox-ui e2e-run-sandbox-ui RHDH_PLUGINS_DIR=${PWD}/../rhdh-plugins ENVIRONMENT=${UI_ENVIRONMENT}


UNIT_TEST_IMAGE_NAME=sandbox-ui-e2e-tests
UNIT_TEST_DOCKERFILE=build/sandbox-ui/Dockerfile

# Build Developer Sandbox UI e2e tests image using podman
.PHONY: build-sandbox-ui-e2e-tests
build-sandbox-ui-e2e-tests:
	@echo "building the $(UNIT_TEST_IMAGE_NAME) image with podman..."
	podman build --platform $(PLATFORM) -t $(UNIT_TEST_IMAGE_NAME) -f $(UNIT_TEST_DOCKERFILE) .

# Run Developer Sandbox UI e2e tests image using podman
PHONY: test-sandbox-ui-in-container
test-sandbox-ui-in-container: build-sandbox-ui-e2e-tests
	@echo "pushing Developer Sandbox UI image..."
	$(MAKE) push-sandbox-plugin
	@echo "running the e2e tests in podman container..."
	podman run --platform $(PLATFORM) --rm \
	  -v $(KUBECONFIG):/root/.kube/config \
	  -e KUBECONFIG=/root/.kube/config \
	  -v ${PWD}:/root/toolchain-e2e \
	  -e E2E_REPO_PATH=/root/toolchain-e2e \
	  -v $(RHDH_PLUGINS_DIR):/root/rhdh-plugins \
	  -e RHDH_PLUGINS_DIR=/root/rhdh-plugins \
	  -e SSO_USERNAME=$(SSO_USERNAME) \
	  -e SSO_PASSWORD=$(SSO_PASSWORD) \
	  -e QUAY_NAMESPACE=$(QUAY_NAMESPACE) \
	  -e TMP=/tmp/ \
	  -e PUSH_SANDBOX_IMAGE=false \
	  -e RUNNING_IN_CONTAINER=true \
	  $(UNIT_TEST_IMAGE_NAME)