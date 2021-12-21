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
	$(Q)-oc delete spaces --all --all-namespaces
	$(Q)-oc wait --for=delete namespaces -l toolchain.dev.openshift.com/type

.PHONY: clean-cluster-wide-config
## Delete all cluster-wide configuration resources like PriorityClass, MutatingWebhookConfiguration, and ClusterRoleBinding for e2e SA
clean-cluster-wide-config:
	$(Q)-oc get ClusterRoleBinding -o name | grep e2e-service-account | xargs oc delete
	$(Q)-oc delete PriorityClass -l='toolchain.dev.openshift.com/provider=codeready-toolchain'
	$(Q)-oc delete MutatingWebhookConfiguration -l='toolchain.dev.openshift.com/provider=codeready-toolchain'

.PHONY: clean-toolchain-namespaces-in-e2e
## Delete e2e namespaces
clean-toolchain-namespaces-in-e2e:
	$(Q)-oc get projects --output=name | grep -E "toolchain-(member|host)(\-operator)?(\-[0-9]+)?" | xargs oc delete

.PHONY: clean-e2e-resources
## Delete resources in the OpenShift cluster. The deleted resources are:
##    * all user-related resources
##    * operator namespaces created during both the dev and e2e test setup (for both operators host and member)
##    * cluster-wide config
clean-e2e-resources: clean-users clean-toolchain-namespaces-in-e2e clean-cluster-wide-config

.PHONY: clean-toolchain-namespaces-in-dev
## Delete dev namespaces
clean-toolchain-namespaces-in-dev:
	$(Q)oc delete namespace ${DEV_HOST_NS} || true
	$(Q)oc delete namespace ${DEV_MEMBER_NS} || true

.PHONY: clean-dev-resources
## Delete resources in the OpenShift cluster. The deleted resources are:
##    * all user-related resources
##    * operator namespaces created during both the dev and e2e test setup (for both operators host and member)
##    * cluster-wide config
clean-dev-resources: clean-users clean-toolchain-namespaces-in-dev clean-cluster-wide-config

.PHONY: clean-e2e-files
## Remove files and directories used during e2e test setup
clean-e2e-files:
	rm -f ${WAS_ALREADY_PAIRED_FILE} 2>/dev/null || true
	rm -rf ${IMAGE_NAMES_DIR} 2>/dev/null || true

.PHONY: clean-all-toolchain-resources
## Delete resources in the OpenShift cluster. The deleted resources are:
##    * all toolchain.dev.openshift.com CRs in both host and member namespaces created during the dev setup
##    * all ClusterresourceQuotas with the label toolchain.dev.openshift.com/provider=codeready-toolchain in member namespace
clean-all-toolchain-resources:
	$(Q)echo "cleaning resources"
	$(Q)oc delete usersignups --all --all-namespaces
	$(Q)for CRD in `oc get crd -o name | grep toolchain`; do \
		echo "deleting $${CRD}"; \
		CRD_NAME=`oc get $${CRD} --template '{{.metadata.name}}'`; \
		oc delete $${CRD_NAME} --all --all-namespaces; \
	done
	$(Q)oc get clusterresourcequotas -l "toolchain.dev.openshift.com/provider"=codeready-toolchain --all --all-namespaces

.PHONY: clean-toolchain-crds
## Delete all Toolchain CRDs
clean-toolchain-crds:
	$(Q)for CRD in `oc get crd -o name | grep toolchain`; do \
		CRD_NAME=`oc get $${CRD} --template '{{.metadata.name}}'`; \
		oc delete crd $${CRD_NAME}; \
	done
