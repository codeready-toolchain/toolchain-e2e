###########################################################
#
# Performance Tests
#
###########################################################

.PHONY: test-perf
## Run the performance tests
test-perf: deploy-e2e perf-run
	@echo "The tests successfully finished"
	@echo "To clean the cluster run 'make clean-e2e-resources'"

.PHONY: perf-run
perf-run:
	oc get toolchaincluster -n $(HOST_NS)
	oc get toolchaincluster -n $(MEMBER_NS)
	oc get toolchaincluster -n ${MEMBER_NS_2}
	-oc new-project $(TEST_NS) --display-name perf-tests 1>/dev/null
	ARTIFACT_DIR=${ARTIFACT_DIR} USER_COUNT=3000 USER_BATCH_SIZE=100 MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} HOST_NS=${HOST_NS} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} operator-sdk test local ./test/perf --no-setup --operator-namespace $(TEST_NS) --verbose --go-test-flags "-timeout=120m -failfast" || \
	($(MAKE) print-logs HOST_NS=${HOST_NS} MEMBER_NS=${MEMBER_NS} MEMBER_NS_2=${MEMBER_NS_2} REGISTRATION_SERVICE_NS=${REGISTRATION_SERVICE_NS} && exit 1)
