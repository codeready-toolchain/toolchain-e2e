# Check all required tools are accessible
REQUIRED_EXECUTABLES = go gofmt git oc operator-sdk sed yamllint find grep python3 jq yq
# If we're running e.g. "make docker-build", nothing but docker is required
# because all the above build tools are supposed to be included in the docker
# image.
ifneq (,$(findstring docker-,$(MAKECMDGOALS)))
    REQUIRED_EXECUTABLES = docker
endif
# Don't check for any tool if "make help" is run or "make" without a target.
ifneq ($(MAKECMDGOALS),help)
ifneq ($(MAKECMDGOALS),)
ifeq ($(VERBOSE),1)
$(info Searching for required executables: $(REQUIRED_EXECUTABLES)...)
endif
K := $(foreach exec,$(REQUIRED_EXECUTABLES),\
        $(if $(shell which $(exec) 2>/dev/null),some string,$(error "ERROR: No "$(exec)" binary found in PATH!")))
endif
endif

# Define the message used for the sed version check below
define SED_MSG

ERROR: The current version of sed will cause the deploy script to fail. Try using gnu-sed instead.

eg.
brew install gnu-sed
export PATH="/usr/local/opt/gnu-sed/libexec/gnubin:$$PATH

endef

# On MacOS, the default sed install will cause a malformed target YAML, use gnu-sed instead
ifeq ($(shell uname -s),Darwin)
$(if $(shell sed --version 2&>1 > /dev/null || echo "bad sed"),$(error ${SED_MSG}))
endif
