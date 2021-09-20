###########################################################
#
# Performance Tests
#
###########################################################

.PHONY: test-perf
## Run the performance tests using code from the remote repos
test-perf: deploy-e2e perf-run
	@echo "The tests successfully finished"
	@echo "To clean the cluster run 'make clean-e2e-resources'"

.PHONY: test-perf-local
## Run the performance tests using code from local repos
test-perf-local: deploy-e2e-local perf-run
	@echo "The tests successfully finished"
	@echo "To clean the cluster run 'make clean-e2e-resources'"

.PHONY: perf-run
perf-run:
	oc get toolchaincluster -n $(HOST_NS)
	oc get toolchaincluster -n $(MEMBER_NS)
	oc get toolchaincluster -n ${MEMBER_NS_2}
	-oc new-project $(TEST_NS) --display-name perf-tests 1>/dev/null
	ARTIFACT_DIR=${ARTIFACT_DIR} USER_COUNT=3000 USER_BATCH_SIZE=100 MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} HOST_NS=${HOST_NS} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} go test ./test/perf -v -timeout=90m -failfast || \
	($(MAKE) print-logs HOST_NS=${HOST_NS} MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} && exit 1)
