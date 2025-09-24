WAS_ALREADY_PAIRED_FILE=/tmp/toolchain_e2e_already_paired

.PHONY: clean-warning
# note that the below comment IS printed on the screen by make because it is part of the target.
# For it to NOT be part of the output, it would have to be written as @#.
clean-warning:
	# If the clean gets stuck, you can call the following goal to unblock the deletion of the resources in the cluster:
	#
	# make force-remove-finalizers-from-e2e-resources
	#

.PHONY: clean
## Remove vendor directory and runs go clean command
clean:
	$(Q)-rm -rf ${V_FLAG} $(OUT_DIR) ./vendor
	$(Q)go clean ${X_FLAG} ./...

.PHONY: clean-users
## Delete usersignups in the OpenShift cluster. The deleted resources are:
##    * all usersignups including user namespaces and banned users
clean-users: clean-warning
	$(Q)-oc delete usersignups --all --all-namespaces
	$(Q)-oc delete bannedusers --all --all-namespaces
	$(Q)-oc delete spacerequests --all --all-namespaces
	$(Q)-oc delete spaces --all --all-namespaces
	$(Q)-oc wait --for=delete namespaces -l toolchain.dev.openshift.com/type
	$(Q)-oc delete namespace workloads-noise --wait --ignore-not-found

clean-nstemplatetiers: clean-warning
	$(Q)-oc delete nstemplatetier --all --all-namespaces
	$(Q)-oc wait --for=delete nstemplatetier --all --all-namespaces
	$(Q)-oc delete tiertemplate --all --all-namespaces
	$(Q)-oc wait --for=delete tiertemplate --all --all-namespaces

.PHONY: clean-cluster-wide-config
## Delete all cluster-wide configuration resources like PriorityClass, MutatingWebhookConfiguration, and ClusterRoleBinding for e2e SA
clean-cluster-wide-config: clean-warning
	$(Q)-oc get ClusterRoleBinding -o name | grep e2e-service-account | xargs oc delete
	$(Q)-oc delete ClusterRoleBinding e2e-test-cluster-admin
	$(Q)-oc get ClusterRole -o jsonpath="{range .items[*]}{.metadata.name} {.metadata.labels.olm\.owner}{'\n'}{end}" | grep "toolchain-" | awk '{print $$1}' | xargs oc delete ClusterRole
	$(Q)-oc get ClusterRoleBinding -o jsonpath="{range .items[*]}{.metadata.name} {.metadata.labels.olm\.owner}{'\n'}{end}" | grep "toolchain-" | awk '{print $$1}' | xargs oc delete ClusterRoleBinding
	$(Q)-oc delete PriorityClass -l='toolchain.dev.openshift.com/provider=codeready-toolchain'
	$(Q)-oc delete MutatingWebhookConfiguration -l='toolchain.dev.openshift.com/provider=codeready-toolchain'
	$(Q)-oc delete ValidatingWebhookConfiguration -l='toolchain.dev.openshift.com/provider=codeready-toolchain'

.PHONY: clean-toolchain-namespaces-in-e2e
## Delete e2e namespaces
clean-toolchain-namespaces-in-e2e: clean-warning
	$(Q)-oc get projects --output=name | grep -E "toolchain-(member|host)(\-operator)?(\-[0-9]+)?" | xargs oc delete

.PHONY: clean-toolchain-dev-sso-resources
## Delete resources in the dev sso namespace
clean-toolchain-dev-sso-resources:
	$(Q)-oc delete keycloak -n ${DEV_SSO_NS} --all --wait
	$(Q)-oc delete keycloakrealm -n ${DEV_SSO_NS} --all

.PHONY: clean-e2e-resources
## Delete resources in the OpenShift cluster. The deleted resources are:
##    * all user-related resources
##    * operator namespaces created during both the dev and e2e test setup (for both operators host and member)
##    * cluster-wide config
clean-e2e-resources: clean-users clean-nstemplatetiers clean-toolchain-namespaces-in-e2e clean-cluster-wide-config

.PHONY: clean-toolchain-namespaces-in-dev
## Delete dev namespaces
clean-toolchain-namespaces-in-dev: clean-toolchain-dev-sso-resources
	$(Q)oc delete namespace ${DEV_HOST_NS} || true
	$(Q)oc delete namespace ${DEV_MEMBER_NS} || true
	$(Q)oc delete namespace ${DEV_SSO_NS} || true

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
	$(Q)oc get clusterresourcequotas -l "toolchain.dev.openshift.com/provider"=codeready-toolchain --all-namespaces

.PHONY: clean-toolchain-crds
## Delete all Toolchain CRDs
clean-toolchain-crds:
	$(Q)for CRD in `oc get crd -o name | grep toolchain`; do \
		CRD_NAME=`oc get $${CRD} --template '{{.metadata.name}}'`; \
		oc delete crd $${CRD_NAME}; \
	done

.PHONY: force-remove-finalizers-from-e2e-resources
## Sometimes after a failed run, the cluster doesn't have our operators running but still contain our resources
## with finalizers. This target removes those finalizers so that the subsequent call to some "clean-*" target
## doesn't get stuck.
## This goal is not called by default so that an attempt to clean up "cleanly" is always attempted first. If that
## fails, you can call this goal explicitly before attempting the cleanup again.
force-remove-finalizers-from-e2e-resources:
	$(Q)for CRD in `oc get crd -o name | grep toolchain`; do \
		CRD_NAME=`oc get $${CRD} --template='{{.metadata.name}}'`; \
		for RES in `oc get $${CRD_NAME} --all-namespaces -ogo-template='{{range .items}}{{.metadata.name}},{{if ne .metadata.namespace nil}}{{.metadata.namespace}}{{else}}{{end}}{{"\n"}}{{end}}'`; do \
		  NAME=`echo $${RES} | cut -d',' -f1`; \
			NS=`echo $${RES} | cut -d',' -f2`; \
			if [ -z "$$NS" ]; then \
			  oc patch $${CRD_NAME} $${NAME} -p '{"metadata":{"finalizers": null}}' --type=merge; \
			else \
			  oc patch $${CRD_NAME} $${NAME} -n $${NS} -p '{"metadata":{"finalizers": null}}' --type=merge; \
			fi \
		done \
	done

