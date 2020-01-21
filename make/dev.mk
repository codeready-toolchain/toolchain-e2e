DEV_MEMBER_NS := ${QUAY_NAMESPACE}-member-operator
DEV_HOST_NS := ${QUAY_NAMESPACE}-host-operator
DEV_REGISTRATION_SERVICE_NS := $(DEV_HOST_NS)
DEV_ENVIRONMENT := dev

.PHONY: dev-deploy-e2e
## deploys the resources
dev-deploy-e2e: deploy-e2e-to-dev-namespaces print-reg-service-link

.PHONY: deploy-e2e-to-dev-namespaces
deploy-e2e-to-dev-namespaces:
	$(MAKE) deploy-e2e MEMBER_NS=${DEV_MEMBER_NS} HOST_NS=${DEV_HOST_NS} REGISTRATION_SERVICE_NS=${DEV_REGISTRATION_SERVICE_NS} ENVIRONMENT=${DEV_ENVIRONMENT}

.PHONY: dev-deploy-e2e-local
dev-deploy-e2e-local: deploy-e2e-local-to-dev-namespaces print-reg-service-link

.PHONY: deploy-e2e-local-to-dev-namespaces
deploy-e2e-local-to-dev-namespaces:
	$(MAKE) deploy-e2e-local MEMBER_NS=${DEV_MEMBER_NS} HOST_NS=${DEV_HOST_NS} REGISTRATION_SERVICE_NS=${DEV_REGISTRATION_SERVICE_NS} ENVIRONMENT=${DEV_ENVIRONMENT}

.PHONY: print-reg-service-link
print-reg-service-link:
	@echo ""
	@echo "Deployment complete!"
	$(eval ROUTE = $(shell oc get routes registration-service -n ${DEV_REGISTRATION_SERVICE_NS} -o=jsonpath='{.spec.host}'))
	@echo Access the Landing Page here: https://${ROUTE}
	@echo "To clean the cluster run 'make clean-e2e-resources'"
	@echo ""

.PHONY: dev-deploy-e2e-member-local
## Deploy the e2e resources with the local 'member-operator' repository only
dev-deploy-e2e-member-local:
	$(MAKE) dev-deploy-e2e MEMBER_REPO_PATH=${PWD}/../member-operator ENVIRONMENT=${DEV_ENVIRONMENT}

.PHONY: dev-deploy-e2e-host-local
## Deploy the e2e resource with the local 'host-operator' repository only
dev-deploy-e2e-host-local:
	$(MAKE) dev-deploy-e2e HOST_REPO_PATH=${PWD}/../host-operator ENVIRONMENT=${DEV_ENVIRONMENT}

.PHONY: dev-deploy-e2e-registration-local
## Deploy the e2e resources with the local 'registration-service' repository only
dev-deploy-e2e-registration-local:
	$(MAKE) dev-deploy-e2e REG_REPO_PATH=${PWD}/../registration-service ENVIRONMENT=${DEV_ENVIRONMENT}
