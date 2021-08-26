.PHONY: lint
## Run linters on Go code files and YAML files
lint: lint-go-code lint-yaml

YAML_FILES := $(shell find . -type f -regex ".*y[a]ml" -print)
.PHONY: lint-yaml
lint-yaml: ${YAML_FILES}
ifeq (, $(shell which yamllint))
	$(error "yamllint not found in PATH. Please install it using instructions on https://yamllint.readthedocs.io/en/stable/quickstart.html#installing-yamllint")
endif
	$(Q)yamllint -c .yamllint $(YAML_FILES)

.PHONY: lint-go-code
lint-go-code:
ifeq (, $(shell which golangci-lint 2>/dev/null))
	$(error "golangci-lint not found in PATH. Please install it using instructions on https://golangci-lint.run/usage/install/#local-installation")
endif
	golangci-lint ${V_FLAG} run -E gofmt,golint,megacheck,misspell