SANDBOX_UI_NS := sandbox-ui
SANDBOX_PLUGIN_IMAGE_NAME := sandbox-rhdh-plugin
TAG := latest
PLATFORM ?= linux/amd64
RHDH_PLUGINS_DIR ?= $(TMPDIR)rhdh-plugins
AUTH_FILE := /tmp/auth.json
OPENID_SECRET_NAME=openid-sandbox-public-client-secret
PUSH_SANDBOX_IMAGE ?= false
UI_ENVIRONMENT := ui-e2e-tests
SSO_USERNAME_READ := $(shell if [ -n "$(CI)" ]; then cat /usr/local/sandbox-secrets/SSO_USERNAME 2>/dev/null || echo ""; else echo "${SSO_USERNAME}"; fi)
SSO_PASSWORD_READ := $(shell if [ -n "$(CI)" ]; then cat /usr/local/sandbox-secrets/SSO_PASSWORD 2>/dev/null || echo ""; else echo "${SSO_PASSWORD}"; fi)

TAG := $(shell \
    if [ -n "$(CI)$(CLONEREFS_OPTIONS)" ]; then \
        if [ -n "$(GITHUB_ACTIONS)" ]; then \
            REPOSITORY_NAME=$$(basename "$(GITHUB_REPOSITORY)"); \
            COMMIT_ID_SUFFIX=$$(echo "$(PULL_PULL_SHA)" | cut -c1-7); \
            echo "from.$${REPOSITORY_NAME}.PR$(PULL_NUMBER).$${COMMIT_ID_SUFFIX}"; \
        else \
            AUTHOR=$$(jq -r '.refs[0].pulls[0].author' <<< $${CLONEREFS_OPTIONS} | tr -d '[:space:]'); \
            PULL_PULL_SHA=$${PULL_PULL_SHA:-$$(jq -r '.refs[0].pulls[0].sha' <<< $${CLONEREFS_OPTIONS} | tr -d '[:space:]')}; \
            COMMIT_ID_SUFFIX=$$(echo "$${PULL_PULL_SHA}" | cut -c1-7); \
            echo "from.$(REPO_NAME).PR$(PULL_NUMBER).$${COMMIT_ID_SUFFIX}"; \
        fi; \
    else \
        echo "latest"; \
    fi)


IMAGE_TO_PUSH_IN_QUAY ?= quay.io/$(QUAY_NAMESPACE)/sandbox-rhdh-plugin:$(TAG)


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
	kustomize build deploy/sandbox-ui/ui-e2e-tests | REGISTRATION_SERVICE_API=${REGISTRATION_SERVICE_API} \
		HOST_OPERATOR_API=${HOST_OPERATOR_API} \
		SANDBOX_UI_NS=${SANDBOX_UI_NS} \
		SANDBOX_PLUGIN_IMAGE=${IMAGE_TO_PUSH_IN_QUAY} \
		RHDH=${RHDH} envsubst | oc apply -f -
	$(MAKE) configure-oauth-idp
ifeq ($(ENVIRONMENT),ui-e2e-tests)
	@echo "applying toolchainconfig changes"
	@echo "HOST_NS: ${HOST_NS}"
	@oc apply -f deploy/host-operator/ui-e2e-tests/toolchainconfig.yaml -n ${HOST_NS}
	@echo "restarting registration-service to apply toolchainconfig changes"
	@oc -n ${HOST_NS} rollout restart deploy/registration-service
else
	@echo "skipping toolchainconfig changes - environment is not ui-e2e-tests"
endif
	@oc -n ${SANDBOX_UI_NS} rollout status deploy/rhdh
	@echo "Developer Sandbox UI running at ${RHDH}"


check-sso-credentials:
	@echo "checking SSO credentials..."
	@if [ -n "$(CI)" ]; then \
		echo "Running in CI - using file-based SSO credentials"; \
		if [ -z "$(SSO_USERNAME_READ)" ] || [ -z "$(SSO_PASSWORD_READ)" ]; then \
			echo "SSO credential files not found or empty in CI environment"; \
			exit 1; \
		fi; \
	else \
		echo "Running locally - using environment variables"; \
		if [ -z "$(SSO_USERNAME_READ)" ] || [ -z "$(SSO_PASSWORD_READ)" ]; then \
			echo "SSO_USERNAME or SSO_PASSWORD environment variables not set"; \
			exit 1; \
		fi; \
	fi
	@echo "Validating SSO credentials..."
	@status=$$(curl -s -o /dev/null -w "%{http_code}" \
	  -X POST "https://sso.devsandbox.dev/auth/realms/sandbox-dev/protocol/openid-connect/token" \
	  -d "grant_type=password" \
	  -d "client_id=sandbox-public" \
	  -d "username=$(SSO_USERNAME_READ)" \
	  -d "password=$(SSO_PASSWORD_READ)"); \
	if [ "$$status" != "200" ]; then \
	  echo "failed trying to login to 'https://sso.devsandbox.dev/auth/realms/sandbox-dev' ($$status) — check your SSO credentials."; \
	  exit 1; \
	fi
	@echo "SSO credentials validated successfully"

configure-oauth-idp:
	@echo "configuring DevSandbox identity provider"
	@oc create secret generic ${OPENID_SECRET_NAME} \
		--from-literal=clientSecret=dummy \
		--namespace=openshift-config
	OPENID_SECRET_NAME=${OPENID_SECRET_NAME} envsubst < deploy/sandbox-ui/ui-e2e-tests/oauth-idp-patch.yaml | \
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
ifeq ($(GITHUB_ACTIONS),true)
	@echo "using author ${AUTHOR}"
	$(eval AUTHOR_LINK = https://github.com/${AUTHOR})
	@echo "detected branch ${BRANCH_NAME}"
	# check if a branch with the same ref exists in the user's fork of rhdh-plugins repo
	@echo "branches of ${AUTHOR_LINK}/rhdh-plugins - checking if there is a branch ${BRANCH_NAME} we could pair with."
	curl ${AUTHOR_LINK}/rhdh-plugins.git/info/refs?service=git-upload-pack --output -
	$(eval REMOTE_RHDH_PLUGINS_BRANCH := $(shell curl ${AUTHOR_LINK}/rhdh-plugins.git/info/refs?service=git-upload-pack --output - 2>/dev/null | grep -a "refs/heads/${BRANCH_NAME}$$" | awk '{print $$2}'))
	
	# check if the branch with the same name exists, if so then merge it with master and use the merge branch, if not then use master
	@echo "REMOTE_RHDH_PLUGINS_BRANCH: ${REMOTE_RHDH_PLUGINS_BRANCH}"
	@$(MAKE) pair-if-needed REMOTE_RHDH_PLUGINS_BRANCH=${REMOTE_RHDH_PLUGINS_BRANCH} AUTHOR_LINK=${AUTHOR_LINK}
else
	@echo "using rhdh-plugins repo from master"
	@$(MAKE) clone-rhdh-plugins
endif
else
	@echo "using local rhdh-plugins repo, no pairing needed: ${RHDH_PLUGINS_DIR}"
endif

pair-if-needed:
ifneq ($(strip $(REMOTE_RHDH_PLUGINS_BRANCH)),)
	@echo "Branch ref of the user's fork to be used for pairing: \"${REMOTE_RHDH_PLUGINS_BRANCH}\""
	git config --global user.email "devsandbox@redhat.com"
	git config --global user.name "KubeSaw"
	# clone
	rm -rf ${RHDH_PLUGINS_DIR}
	git clone --depth=1 https://github.com/redhat-developer/rhdh-plugins.git ${RHDH_PLUGINS_DIR}
	# add the user's fork as remote repo
	git --git-dir=${RHDH_PLUGINS_DIR}/.git --work-tree=${RHDH_PLUGINS_DIR} remote add external ${AUTHOR_LINK}/rhdh-plugins.git
	# fetch the branch
	git --git-dir=${RHDH_PLUGINS_DIR}/.git --work-tree=${RHDH_PLUGINS_DIR} fetch external ${REMOTE_RHDH_PLUGINS_BRANCH}
	# merge the branch with master
	git --git-dir=${RHDH_PLUGINS_DIR}/.git --work-tree=${RHDH_PLUGINS_DIR} merge --allow-unrelated-histories --no-commit FETCH_HEAD
else
	@echo "no pairing needed, using rhdh-plugins repo from master"
	@$(MAKE) clone-rhdh-plugins 
endif

.PHONY: clone-rhdh-plugins
clone-rhdh-plugins:
	rm -rf ${RHDH_PLUGINS_DIR}; \
	git clone --depth=1 https://github.com/redhat-developer/rhdh-plugins $(RHDH_PLUGINS_DIR) && \
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
	@echo "Installing Playwright..."
	$(eval PWGO_VER := $(shell grep -oE "playwright-go v\S+" go.mod | sed 's/playwright-go //g'))
	@echo "Installing Playwright CLI version: $(PWGO_VER)"
	go install github.com/playwright-community/playwright-go/cmd/playwright@$(PWGO_VER)
	@echo "Installing Firefox browser for Playwright..."
	$(GOPATH)/bin/playwright install firefox

	@echo "Running Developer Sandbox UI setup e2e tests..."
	SANDBOX_UI_NS=${SANDBOX_UI_NS} go test "./test/e2e/sandbox-ui/setup" -v -timeout=10m -failfast
	
	@echo "Running Developer Sandbox UI e2e tests in firefox..."
	@SSO_USERNAME=$(SSO_USERNAME_READ) SSO_PASSWORD=$(SSO_PASSWORD_READ) BASE_URL=${RHDH} BROWSER=firefox envsubst < deploy/sandbox-ui/ui-e2e-tests/.env > testsupport/sandbox-ui/.env
	go test "./test/e2e/sandbox-ui" -v -timeout=10m -failfast
	@oc delete usersignup $(SSO_USERNAME_READ) -n ${HOST_NS}

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
.PHONY: test-sandbox-ui-in-container
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
	  -e RUNNING_IN_CONTAINER=true \
	  $(UNIT_TEST_IMAGE_NAME)
