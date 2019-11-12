DEV_MEMBER_NS := toolchain-member-operator
DEV_HOST_NS := toolchain-host-operator
DEV_REGISTRATION_SERVICE_NS := $(HOST_NS)

.PHONY: dev-deploy-e2e
dev-deploy-e2e: $(MAKE) deploy-e2e MEMBER_NS=${DEV_MEMBER_NS} HOST_NS=${DEV_HOST_NS} REGISTRATION_SERVICE_NS=${DEV_REGISTRATION_SERVICE_NS}
	@echo "Deployment complete!"
	@echo "To clean the cluster run 'make clean-e2e-resources'"

.PHONY: dev-deploy-e2e-local
## Deploy the e2e environment with the local 'host', 'member', and 'registration-service' repositories
dev-deploy-e2e-local:
	$(MAKE) deploy-e2e-local MEMBER_NS=${DEV_MEMBER_NS} HOST_NS=${DEV_HOST_NS} REGISTRATION_SERVICE_NS=${DEV_REGISTRATION_SERVICE_NS}
	@echo "Deployment complete!"
	@echo "To clean the cluster run 'make clean-e2e-resources'"
