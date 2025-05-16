SANDBOX_UI_NS := sandbox-ui
SANDBOX_PLUGIN_IMAGE_NAME := sandbox-rhdh-plugin
TAG := latest
PLATFORM = linux/amd64
RHDH_PLUGINS_DIR = $(TMPDIR)rhdh-plugins
AUTH_FILE := /tmp/auth.json

.PHONY: deploy-sandbox-ui
deploy-sandbox-ui: REGISTRATION_SERVICE_API=https://$(shell oc get route registration-service -n ${HOST_NS} -o custom-columns=":spec.host" | tr -d '\n')/api/v1
deploy-sandbox-ui: HOST_OPERATOR_API=https://$(shell oc get route api -n ${HOST_NS} -o custom-columns=":spec.host" | tr -d '\n')
deploy-sandbox-ui: RHDH=https://rhdh-${SANDBOX_UI_NS}.$(shell oc get ingress.config.openshift.io/cluster -o jsonpath='{.spec.domain}')
deploy-sandbox-ui: check-registry
deploy-sandbox-ui: OS_IMAGE_REGISTRY=$(shell oc get route default-route -n openshift-image-registry --template='{{ .spec.host }}' 2>/dev/null || true)
deploy-sandbox-ui:
	@echo "sandbox ui will be deployed in '${SANDBOX_UI_NS}' namespace"
	$(MAKE) create-namespace SANDBOX_UI_NS=${SANDBOX_UI_NS}
	$(MAKE) push-sandbox-plugin
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
	kustomize build deploy/sandbox-ui/e2e-tests | REGISTRATION_SERVICE_API=${REGISTRATION_SERVICE_API} \
		HOST_OPERATOR_API=${HOST_OPERATOR_API} \
		SANDBOX_UI_NS=${SANDBOX_UI_NS} \
		SANDBOX_PLUGIN_IMAGE=${OS_IMAGE_REGISTRY}/${SANDBOX_UI_NS}/${SANDBOX_PLUGIN_IMAGE_NAME}:${TAG} \
		RHDH=${RHDH} envsubst | oc apply -f -
	@oc -n ${SANDBOX_UI_NS} rollout status deploy/rhdh


.PHONY: deploy-sandbox-ui-local
deploy-sandbox-ui-local:
	$(MAKE) deploy-sandbox-ui RHDH_PLUGINS_DIR=${PWD}/../rhdh-plugins

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
push-sandbox-plugin: check-registry
push-sandbox-plugin: OS_IMAGE_REGISTRY=$(shell oc get route default-route -n openshift-image-registry --template='{{ .spec.host }}' 2>/dev/null || true)
push-sandbox-plugin: IMAGE_TO_PUSH=${OS_IMAGE_REGISTRY}/${SANDBOX_UI_NS}/${SANDBOX_PLUGIN_IMAGE_NAME}:${TAG}
push-sandbox-plugin:
	$(MAKE) get-rhdh-plugins
	cd $(RHDH_PLUGINS_DIR)/workspaces/sandbox && \
	echo "podman push ${IMAGE_TO_PUSH} --creds=${OC_WHOAMI}:${OC_WHOAMI_TOKEN} --tls-verify=false" && \
	yarn install && \
	npx @janus-idp/cli@3.3.1 package package-dynamic-plugins \
		--tag ${IMAGE_TO_PUSH} \
		--platform ${PLATFORM} && \
	podman push ${IMAGE_TO_PUSH} --creds=${OC_WHOAMI}:${OC_WHOAMI_TOKEN} --tls-verify=false

