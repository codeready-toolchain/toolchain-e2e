GOFORMAT_FILES := $(shell find  . -name '*.go' | grep -vEf ./gofmt_exclude)

.PHONY: check-go-format
## Verify the formatting defined by 'gofmt'
check-go-format:
	$(Q)gofmt -s -l ${GOFORMAT_FILES} 2>&1 \
		| tee $(OUT_DIR)/gofmt-errors \
		| read \
	&& echo "ERROR: These files differ from gofmt's style (run 'make format-go-code' to fix this):" \
	&& cat $(OUT_DIR)/gofmt-errors \
	&& exit 1 \
	|| true

.PHONY: format-go-code
## Formats any go file that does not match formatting defined by gofmt
format-go-code:
	$(Q)gofmt -s -l -w ${GOFORMAT_FILES}