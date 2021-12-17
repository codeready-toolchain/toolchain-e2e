WAS_ALREADY_PAIRED_FILE=/tmp/toolchain_e2e_already_paired

.PHONY: clean
## Remove vendor directory and runs go clean command
clean:
	$(Q)-rm -rf ${V_FLAG} $(OUT_DIR) ./vendor
	$(Q)go clean ${X_FLAG} ./...

.PHONY: clean-users
## Delete usersignups in the OpenShift cluster. The deleted resources are:
##    * all usersignups including user namespaces
clean-users:
	$(Q)-oc delete usersignups --all --all-namespaces
	$(Q)-oc wait --for=delete namespaces -l toolchain.dev.openshift.com/provider=codeready-toolchain

.PHONY: clean-e2e-resources
## Delete resources in the OpenShift cluster. The deleted resources are:
##    * all resources created by both host and member operators in all namespaces including user namespaces
##    * operator namespaces created during both the dev and e2e test setup (for both operators host and member)
##    * all CatalogSources that were created as part of operator deployment
clean-e2e-resources: clean-users
	$(Q)-oc get projects --output=name | grep -E "toolchain-(member|host)(\-operator)?(\-[0-9]+)?" | xargs oc delete
	$(Q)-oc get ClusterRoleBinding -o name | grep e2e-service-account | xargs oc delete
	$(Q)-oc delete PriorityClass -l='toolchain.dev.openshift.com/provider=codeready-toolchain'
	$(Q)-oc delete MutatingWebhookConfiguration -l='toolchain.dev.openshift.com/provider=codeready-toolchain'

.PHONY: clean-e2e-files
## Remove files and directories used during e2e test setup
clean-e2e-files:
	rm -f ${WAS_ALREADY_PAIRED_FILE} 2>/dev/null || true
	rm -rf ${IMAGE_NAMES_DIR} 2>/dev/null || true

.PHONY: clean-toolchain-resources-in-dev
## Delete resources in the OpenShift cluster. The deleted resources are:
##    * all toolchain.dev.openshift.com CRs in both host and member namespaces created during the dev setup
##    * all ClusterresourceQuotas with the label toolchain.dev.openshift.com/provider=codeready-toolchain in member namespace
##    * pods of both operators host and member
clean-toolchain-resources-in-dev:
	$(Q)echo "cleaning resources in $(DEV_HOST_NS) and $(DEV_MEMBER_NS)..."
	$(Q)oc get usersignups -n $(DEV_HOST_NS) -o name | xargs -i oc delete -n $(DEV_HOST_NS) {}
	$(Q)for CRD in `oc get crd -o name | grep toolchain`; do \
		echo "deleting $${CRD} in $(DEV_HOST_NS) and $(DEV_MEMBER_NS)"; \
		CRD_NAME=`oc get $${CRD} --template '{{.metadata.name}}'`; \
		oc get $${CRD_NAME} -n $(DEV_HOST_NS) -o name | xargs -i oc delete {}; \
		oc get $${CRD_NAME} -n $(DEV_MEMBER_NS) -o name | xargs -i oc delete {}; \
	done
	$(Q)oc get clusterresourcequotas -l "toolchain.dev.openshift.com/provider"=codeready-toolchain -o name -n $(DEV_MEMBER_NS) | xargs -i oc delete {}
	$(Q)oc get pods -n $(DEV_HOST_NS) -l name=host-operator -o name | xargs -i oc delete -n $(DEV_HOST_NS) {}
	$(Q)oc get pods -n $(DEV_MEMBER_NS) -l name=member-operator -o name | xargs -i oc delete -n $(DEV_MEMBER_NS) {}

.PHONY: clean-toolchain-namespaces-in-dev
## Delete dev namespaces
clean-toolchain-namespaces-in-dev:
	$(Q)oc delete namespace ${DEV_HOST_NS} || true
	$(Q)oc delete namespace ${DEV_MEMBER_NS} || true

.PHONY: clean-toolchain-crds
## Delete all Toolchain CRDs
clean-toolchain-crds:
	$(Q)for CRD in `oc get crd -o name | grep toolchain`; do \
		CRD_NAME=`oc get $${CRD} --template '{{.metadata.name}}'`; \
		oc delete crd $${CRD_NAME}; \
	done
