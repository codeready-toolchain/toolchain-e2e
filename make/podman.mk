# WILL_PUBLISH is true when the e2e goals will publish new operator image(s)
# to quay. This enables the check to conditionally execute only when needed.
WILL_PUBLISH := false
ifneq ($(HOST_REPO_PATH),)
WILL_PUBLISH := true
endif
ifneq ($(MEMBER_REPO_PATH),)
WILL_PUBLISH := true
endif
ifneq ($(REG_REPO_PATH),)
WILL_PUBLISH := true
endif

.PHONY: check-quay-login-if-needed
check-quay-login-if-needed:
	@if [ "${WILL_PUBLISH}" = "true" -o "${PUSH_SANDBOX_IMAGE}" = "true" ]; then \
		podman login --get-login quay.io >/dev/null; \
	fi

.PHONY: check-quay-login
check-quay-login:
	@podman login --get-login quay.io >/dev/null
