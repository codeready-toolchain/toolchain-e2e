.PHONY: clean
## Removes vendor directory and runs go clean command
clean:
	$(Q)-rm -rf ${V_FLAG} $(OUT_DIR) ./vendor
	$(Q)go clean ${X_FLAG} ./...

.PHONY: clean-e2e-resources
## Deletes resources in the OpenShift cluster. The deleted resources are:
##    * all resources created by both host and member operators in all namespaces including user namespaces
##    * operator namespaces created during both the dev and e2e test setup (for both operators host and member)
##    * all CatalogSources that were created as part of operator deployment
clean-e2e-resources:
	$(Q)-for todelete in `oc get usersignup --all-namespaces -o jsonpath='{range .items[*]}{.metadata.name}={.metadata.namespace}{"\n"}{end}'`; do \
		echo "oc delete usersignup $${todelete%=*} -n $${todelete#*=}"; \
		oc delete usersignup $${todelete%=*} -n $${todelete#*=}; \
	done
	$(Q)-oc wait --for=delete namespaces -l toolchain.dev.openshift.com/provider=codeready-toolchain
	$(Q)-oc get projects --output=name | grep -E "${QUAY_NAMESPACE}-(toolchain\-)?(member|host)(\-operator)?(\-[0-9]+)?|${QUAY_NAMESPACE}-toolchain\-e2e\-[0-9]+" | xargs oc delete
	$(Q)-oc get catalogsource --output=name -n openshift-marketplace | grep "source-toolchain-.*${QUAY_NAMESPACE}" | xargs oc delete -n openshift-marketplace

.PHONY: clean-e2e-files
## Removes files and directories used during e2e test setup
clean-e2e-files:
	rm -f ${WAS_ALREADY_PAIRED_FILE} 2>/dev/null || true
	rm -rf ${IMAGE_NAMES_DIR} 2>/dev/null || true

.PHONY: clean-toolchain-resources-in-dev
## Deletes resources in the OpenShift cluster. The deleted resources are:
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
