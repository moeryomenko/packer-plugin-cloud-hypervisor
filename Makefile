BINARY := packer-plugin-cloud-hypervisor
COVER_FILE ?= coverage.out
RACE_DETECTOR := $(if $(RACE_DETECTOR),-race)
IMPORT_PATH := $(shell go list -m -f {{.Path}} | head -1)

.PHONY: default
default: help

.PHONY: build
build: ## Build the plugin binary
	@go build -o $(BINARY) .

.PHONY: test
test: ## Run all tests
	@go tool gotestsum --format-hide-empty-pkg -f testname -- $(RACE_DETECTOR) -p=1 -vet=off -count=1 -timeout=1200s -coverprofile=$(COVER_FILE) ./...
	@go tool cover -func=$(COVER_FILE) | grep ^total

.PHONY: cover
cover: test ## Open coverage report in browser
	@go tool cover -html=$(COVER_FILE)

.PHONY: lint
lint: ## Run linter
	@go tool golangci-lint run -v --fix

.PHONY: fmt
fmt: ## Format Go source files
	@gofmt -s -w .
	@git status --short | grep '[A|M]' | grep -E -o "[^ ]*$$" | grep '\.go$$' | xargs -I{} go tool golines --base-formatter=gofumpt --ignore-generated --tab-len=1 --max-len=120 -w {}
	@git status --short | grep '[A|M]' | grep -E -o "[^ ]*$$" | grep '\.go$$' | xargs -I{} go tool goimports -local $(IMPORT_PATH) -w {}

.PHONY: vet
vet: ## Run go vet
	@go vet ./...

.PHONY: tidy
tidy: ## Tidy go module dependencies
	@go mod tidy -v

.PHONY: generate
generate: ## Run go generate (regenerate config.hcl2spec.go)
	@go generate ./...

.PHONY: clean
clean: ## Remove build artifacts and coverage output
	@rm -f $(BINARY) $(COVER_FILE)

.PHONY: check
check: lint vet test ## Run lint, vet, and tests (CI gate)

.PHONY: help
help: ## Print this help message
	@echo "Usage: make <target>"
	@echo ""
	@echo "Targets:"
	@grep -F -h '##' $(MAKEFILE_LIST) \
		| grep -F -v fgrep \
		| sort \
		| grep -E '^[a-zA-Z_-]+:.*?## .*$$' \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
