.PHONY: appstudio-dev-deploy-latest
appstudio-dev-deploy-latest: DEV_ENVIRONMENT=appstudio-dev
appstudio-dev-deploy-latest: dev-deploy-e2e

.PHONY: appstudio-e2e-deploy-latest
appstudio-e2e-deploy-latest: DEV_ENVIRONMENT=appstudio-e2e
appstudio-e2e-deploy-latest: dev-deploy-e2e

.PHONY: appstudio-cleanup
appstudio-cleanup: clean-dev-resources