.PHONY: all push container clean container-name container-latest push-latest local generated fmt lint test

BIN := jupyter-operator
PROJECT := jupyter-operator
PKG := github.com/squat/$(PROJECT)
REGISTRY ?= index.docker.io
IMAGE ?= squat/$(PROJECT)

TAG := $(shell git describe --abbrev=0 --tags HEAD 2>/dev/null)
COMMIT := $(shell git rev-parse HEAD)
VERSION := $(COMMIT)
ifneq ($(TAG),)
    ifeq ($(COMMIT), $(shell git rev-list -n1 $(TAG)))
        VERSION := $(TAG)
    endif
endif
DIRTY := $(shell test -z "$$(git diff --shortstat 2>/dev/null)" || echo -dirty)
VERSION := $(VERSION)$(DIRTY)
LD_FLAGS := -ldflags '-X $(PKG)/version.Version=$(VERSION)'
SRC := $(shell find . -type f -name '*.go' -not -path "./vendor/*")
GO_FILES ?= $$(find . -name '*.go' -not -path './vendor/*')
GO_PKGS ?= $$(go list ./... | grep -v "$(PKG)/vendor")

BUILD_IMAGE ?= golang:1.9.1-alpine

all: build

build: bin/$(BIN)

bin:
	@mkdir -p bin

generated:
	vendor/k8s.io/code-generator/generate-groups.sh all $(PKG)/pkg $(PKG)/pkg/apis jupyter:v1 --go-header-file boilerplate.txt

bin/$(BIN): $(SRC) glide.yaml generated bin
	@echo "building: $@"
	@docker run --rm \
	    -u $$(id -u):$$(id -g) \
	    -v $$(pwd):/go/src/$(PKG) \
	    -v $$(pwd)/bin:/go/bin \
	    -w /go/src/$(PKG) \
	    $(BUILD_IMAGE) \
	    /bin/sh -c " \
	        GOOS=linux \
		CGO_ENABLED=0 \
		go build -o bin/$(BIN) \
		    $(LD_FLAGS) \
		    ./cmd/$(BIN)/... \
	    "

fmt:
	@echo $(GO_PKGS)
	gofmt -w -s $(GO_FILES)

lint:
	@echo 'golint $(GO_PKGS)'
	@lint_res=$$(golint $(GO_PKGS)); if [ -n "$$lint_res" ]; then \
		echo ""; \
		echo "Golint found style issues. Please check the reported issues"; \
		echo "and fix them if necessary before submitting the code for review:"; \
		echo "$$lint_res"; \
		exit 1; \
	fi
	@echo 'gofmt -d -s $(GO_FILES)'
	@fmt_res=$$(gofmt -d -s $(GO_FILES)); if [ -n "$$fmt_res" ]; then \
		echo ""; \
		echo "Gofmt found style issues. Please check the reported issues"; \
		echo "and fix them if necessary before submitting the code for review:"; \
		echo "$$fmt_res"; \
		exit 1; \
	fi

test: lint

local: $(SRC) glide.yaml generated
	@GOOS=linux \
	    CGO_ENABLED=0 \
	    go build -o bin/$(BIN) \
	    $(LD_FLAGS) \
	    ./cmd/$(BIN)/...

container: .container-$(VERSION) container-name
.container-$(VERSION): bin/$(BIN) Dockerfile
	@docker build -t $(IMAGE):$(VERSION) .
	@docker images -q $(IMAGE):$(VERSION) > $@

container-latest: .container-$(VERSION)
	@docker tag $(IMAGE):$(VERSION) $(IMAGE):latest
	@echo "container: $(IMAGE):latest"

container-name:
	@echo "container: $(IMAGE):$(VERSION)"

push: .push-$(VERSION) push-name
.push-$(VERSION): .container-$(VERSION)
	@docker push $(REGISTRY)/$(IMAGE):$(VERSION)
	@docker images -q $(IMAGE):$(VERSION) > $@

push-latest: container-latest
	@docker push $(REGISTRY)/$(IMAGE):latest
	@echo "pushed: $(IMAGE):latest"

push-name:
	@echo "pushed: $(IMAGE):$(VERSION)"

clean: container-clean bin-clean

container-clean:
	rm -rf .container-* .push-*

bin-clean:
	rm -rf bin
