.PHONY: clean
clean:
	$(Q)-rm -rf ${V_FLAG} $(OUT_DIR) ./vendor
	$(Q)go clean ${X_FLAG} ./...

.PHONY: clean-resources
clean-resources:
	$(Q)echo "cleaning resources in $(HOST_NS) and $(MEMBER_NS)..."
	$(Q)oc get usersignups -n $(HOST_NS) -o name | xargs oc delete -n $(HOST_NS)
	$(Q)oc get pods -n $(HOST_NS) -l name=host-operator -o name | xargs oc delete -n $(HOST_NS)
	$(Q)oc get pods -n $(MEMBER_NS) -l name=member-operator -o name | xargs oc delete -n $(MEMBER_NS)
	$(Q)oc get clusterresourcequotas -l "toolchain.dev.openshift.com/provider"=codeready-toolchain -o name| xargs oc delete