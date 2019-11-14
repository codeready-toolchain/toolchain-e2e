DEV_MEMBER_NS := toolchain-member-operator
DEV_HOST_NS := toolchain-host-operator
DEV_REGISTRATION_SERVICE_NS := $(DEV_HOST_NS)
DEV_ENVIRONMENT := dev

.PHONY: dev-deploy-e2e
## Deploy the e2e resources to dev environment with the operators and reg-service from master
dev-deploy-e2e: MEMBER_NS=${DEV_MEMBER_NS}
dev-deploy-e2e: HOST_NS=${DEV_HOST_NS}
dev-deploy-e2e: REGISTRATION_SERVICE_NS=${DEV_REGISTRATION_SERVICE_NS}
dev-deploy-e2e: ENVIRONMENT=${DEV_ENVIRONMENT}
dev-deploy-e2e: deploy-e2e print-reg-service-link

.PHONY: dev-deploy-e2e-local
## Deploy the e2e resources to dev environment with the local operators and reg-service
dev-deploy-e2e-local: MEMBER_REPO_PATH=${PWD}/../member-operator
dev-deploy-e2e-local: HOST_REPO_PATH=${PWD}/../host-operator
dev-deploy-e2e-local: REG_REPO_PATH=${PWD}/../registration-service
dev-deploy-e2e-local: dev-deploy-e2e

.PHONY: dev-deploy-e2e-member-local
## Deploy the e2e resources to dev environment with the local 'member-operator' repository only
dev-deploy-e2e-member-local: MEMBER_REPO_PATH=${PWD}/../member-operator
dev-deploy-e2e-member-local: dev-deploy-e2e

.PHONY: dev-deploy-e2e-host-local
## Deploy the e2e resource to dev environment with the local 'host-operator' repository only
dev-deploy-e2e-host-local: HOST_REPO_PATH=${PWD}/../host-operator
dev-deploy-e2e-host-local: dev-deploy-e2e

.PHONY: dev-deploy-e2e-registration-local
## Deploy the e2e resources to dev environment with the local 'registration-service' repository only
dev-deploy-e2e-registration-local: REG_REPO_PATH=${PWD}/../registration-service
dev-deploy-e2e-registration-local: dev-deploy-e2e

.PHONY: print-reg-service-link
print-reg-service-link:
	@echo ""
	@echo "Deployment complete!"
	$(eval ROUTE = $(shell oc get routes registration-service -n ${DEV_REGISTRATION_SERVICE_NS} -o=jsonpath='{.spec.host}'))
	@echo Access the Landing Page here: https://${ROUTE}
	@echo "To clean the cluster run 'make clean-e2e-resources'"
	@echo ""
