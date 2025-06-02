SANDBOX_UI_NS := sandbox-ui
SANDBOX_PLUGIN_IMAGE_NAME := sandbox-rhdh-plugin
TAG := latest
PLATFORM = linux/amd64
RHDH_PLUGINS_DIR = $(TMPDIR)rhdh-plugins
AUTH_FILE := /tmp/auth.json
IMAGE_TO_PUSH_IN_QUAY ?= quay.io/$(QUAY_NAMESPACE)/sandbox-rhdh-plugin:$(TAG)
OPENID_SECRET_NAME=openid-sandbox-public-client-secret

.PHONY: deploy-sandbox-ui
deploy-sandbox-ui: REGISTRATION_SERVICE_API=https://$(shell oc get route registration-service -n ${HOST_NS} -o custom-columns=":spec.host" | tr -d '\n')/api/v1
deploy-sandbox-ui: HOST_OPERATOR_API=https://$(shell oc get route api -n ${HOST_NS} -o custom-columns=":spec.host" | tr -d '\n')
deploy-sandbox-ui: RHDH=https://rhdh-${SANDBOX_UI_NS}.$(shell oc get ingress.config.openshift.io/cluster -o jsonpath='{.spec.domain}')
deploy-sandbox-ui: check-registry
deploy-sandbox-ui:
	@echo "sandbox ui will be deployed in '${SANDBOX_UI_NS}' namespace"
	$(MAKE) create-namespace SANDBOX_UI_NS=${SANDBOX_UI_NS}
	$(MAKE) push-sandbox-plugin
	$(MAKE) create-pull-secret 
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

create-pull-secret: OS_IMAGE_REGISTRY=$(shell oc get route default-route -n openshift-image-registry --template='{{ .spec.host }}' 2>/dev/null || true)
create-pull-secret:
	@oc create secret docker-registry pull-secret \
		--docker-server=${OS_IMAGE_REGISTRY} \
		--docker-username=${OC_WHOAMI} \
		--docker-password=${OC_WHOAMI_TOKEN} \
		--namespace=${SANDBOX_UI_NS}
	@oc extract secret/pull-secret -n ${SANDBOX_UI_NS} --keys=.dockerconfigjson --to=- > ${AUTH_FILE}
	@oc create secret generic rhdh-dynamic-plugins-registry-auth \
		--from-file=auth.json=${AUTH_FILE} \
		--namespace=${SANDBOX_UI_NS}
	rm ${AUTH_FILE}

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

.PHONY: e2e-run-sandbox-ui-setup
e2e-run-sandbox-ui-setup: RHDH=https://rhdh-${SANDBOX_UI_NS}.$(shell oc get ingress.config.openshift.io/cluster -o jsonpath='{.spec.domain}')
e2e-run-sandbox-ui-setup:
	@echo "Running Developer Sandbox UI setup e2e tests..."
	SANDBOX_UI_NS=${SANDBOX_UI_NS} go test "./test/e2e/sandbox-ui/setup" -p 1 -v -timeout=90m -failfast
	SSO_USERNAME=${SSO_USERNAME} SSO_PASSWORD=${SSO_PASSWORD} BASE_URL=${RHDH} envsubst < deploy/sandbox-ui/e2e-tests/.env > $(RHDH_PLUGINS_DIR)/workspaces/sandbox/.env
	cd $(RHDH_PLUGINS_DIR)/workspaces/sandbox && \
		yarn playwright test --project=chrome && \
		oc delete usersignup ${SSO_USERNAME} -n ${HOST_NS} && \
		yarn playwright test --project=firefox
	@echo "The Developer Sandbox UI setup e2e tests successfully finished"

.PHONY: deploy-and-test-sandbox-ui
deploy-and-test-sandbox-ui: deploy-sandbox-ui e2e-run-sandbox-ui-setup

.PHONY: deploy-and-test-sandbox-ui-local
deploy-and-test-sandbox-ui-local: 
	$(MAKE) deploy-sandbox-ui e2e-run-sandbox-ui-setup RHDH_PLUGINS_DIR=${PWD}/../rhdh-plugins-1