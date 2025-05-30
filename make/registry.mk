OS_IMAGE_REGISTRY := $(shell oc get route default-route -n openshift-image-registry --template='{{ .spec.host }}' 2>/dev/null || true)
OC_WHOAMI := $(shell oc whoami 2>/dev/null | sed 's/://' || true)
OC_WHOAMI_TOKEN := $(shell oc whoami --show-token 2>/dev/null || true)

.PHONY: enable-image-registry
## Enables OpenShift image registry in the cluster
enable-image-registry:
	oc patch configs.imageregistry.operator.openshift.io/cluster --patch '{"spec":{"defaultRoute":true}}' --type=merge

check-registry:
ifeq (${OS_IMAGE_REGISTRY},)
	@echo "ERROR: The OpenShift image registry in your cluster is not enabled."
	@echo "Attempting to enable it by running 'make enable-image-registry'..."
	$(MAKE) enable-image-registry
endif
ifeq (${OC_WHOAMI},)
	@echo "ERROR: The output of 'oc whoami' is empty."
	@exit 1
endif
ifeq (${OC_WHOAMI_TOKEN},)
	@echo "ERROR: The output of 'oc whoami --show-token' is empty."
	@exit 1
endif