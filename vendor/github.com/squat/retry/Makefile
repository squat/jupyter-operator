.PHONY: all build fmt lint test vet 

TEST?=$$(go list ./... | grep -v 'vendor/')
FMT_FILES?=$$(find . -name '*.go' | grep -v vendor)
TESTARGS?=

all: build

build:
	go build

fmt:
	gofmt -w -s $(FMT_FILES)

lint:
	@echo 'golint $(TEST)'
	@lint_res=$$(golint $(TEST)); if [ -n "$$lint_res" ]; then \
		echo ""; \
		echo "Golint found style issues. Please check the reported issues"; \
		echo "and fix them if necessary before submitting the code for review:"; \
		echo "$$lint_res"; \
		exit 1; \
	fi
	@echo 'gofmt -d -s $(FMT_FILES)'
	@fmt_res=$$(gofmt -d -s $(FMT_FILES)); if [ -n "$$fmt_res" ]; then \
		echo ""; \
		echo "Gofmt found style issues. Please check the reported issues"; \
		echo "and fix them if necessary before submitting the code for review:"; \
		echo "$$fmt_res"; \
		exit 1; \
	fi

test: vet lint
	go test $(TESTARGS) -timeout=30s -parallel=4 $(TEST)

vet:
	@echo 'go vet $(TEST)'
	@go vet $(TEST); if [ $$? -eq 1 ]; then \
		echo ""; \
		echo "Vet found suspicious constructs. Please check the reported constructs"; \
		echo "and fix them if necessary before submitting the code for review."; \
		exit 1; \
	fi
