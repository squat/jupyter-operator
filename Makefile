.PHONY: all push container clean container-name container-latest push-latest local fmt lint test vendor generate client deepcopy informer lister openapi

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
SRC := $(shell find . -type f -name '*.go' -not -path "./vendor/*") pkg/clientset/versioned/typed/jupyter/v1/notebook.go pkg/apis/jupyter/v1/zz_generated.deepcopy.go pkg/informers/externalversions/jupyter/v1/notebook.go pkg/listers/jupyter/v1/notebook.go pkg/apis/jupyter/v1/openapi_generated.go
GO_FILES ?= $$(find . -name '*.go' -not -path './vendor/*')
GO_PKGS ?= $$(go list ./... | grep -v "$(PKG)/vendor")
CODE_GENERATOR_VERSION := release-1.12
CLIENT_GEN_BINARY:=$(GOPATH)/bin/client-gen
DEEPCOPY_GEN_BINARY:=$(GOPATH)/bin/deepcopy-gen
INFORMER_GEN_BINARY:=$(GOPATH)/bin/informer-gen
LISTER_GEN_BINARY:=$(GOPATH)/bin/lister-gen
OPENAPI_GEN_BINARY:=$(GOPATH)/bin/openapi-gen

BUILD_IMAGE ?= golang:1.11.1-alpine

all: build

build: bin/$(BIN)

bin:
	@mkdir -p bin

generate: client deepcopy informer lister

client: pkg/clientset/versioned/typed/jupyter/v1/notebook.go
pkg/clientset/versioned/typed/jupyter/v1/notebook.go: .header pkg/apis/jupyter/v1/types.go $(CLIENT_GEN_BINARY)
	$(CLIENT_GEN_BINARY) \
	--clientset-name versioned \
	--input-base "" \
	--input $(PKG)/pkg/apis/jupyter/v1 \
	--output-package $(PKG)/pkg/clientset \
	--go-header-file=.header \
	--logtostderr
	go fmt $(PKG)/pkg/clientset/...

deepcopy: pkg/apis/jupyter/v1/zz_generated.deepcopy.go
pkg/apis/jupyter/v1/zz_generated.deepcopy.go: .header pkg/apis/jupyter/v1/types.go $(DEEPCOPY_GEN_BINARY)
	$(DEEPCOPY_GEN_BINARY) \
	--input-dirs $(PKG)/pkg/apis/jupyter/v1 \
	--go-header-file=.header \
	--logtostderr \
	--bounding-dirs $(PKG)/pkg/apis \
	--output-file-base zz_generated.deepcopy
	go fmt $@

informer: pkg/informers/externalversions/jupyter/v1/notebook.go
pkg/informers/externalversions/jupyter/v1/notebook.go: .header pkg/apis/jupyter/v1/types.go $(INFORMER_GEN_BINARY)
	$(INFORMER_GEN_BINARY) \
	--input-dirs $(PKG)/pkg/apis/jupyter/v1 \
	--go-header-file=.header \
	--logtostderr \
	--versioned-clientset-package $(PKG)/pkg/clientset/versioned \
	--listers-package $(PKG)/pkg/listers \
	--output-package $(PKG)/pkg/informers
	go fmt $(PKG)/pkg/informers/...

lister: pkg/listers/jupyter/v1/notebook.go
pkg/listers/jupyter/v1/notebook.go: .header pkg/apis/jupyter/v1/types.go $(LISTER_GEN_BINARY)
	$(LISTER_GEN_BINARY) \
	--input-dirs $(PKG)/pkg/apis/jupyter/v1 \
	--go-header-file=.header \
	--logtostderr \
	--output-package $(PKG)/pkg/listers
	go fmt $(PKG)/pkg/listers/...

openapi: pkg/apis/jupyter/v1/openapi_generated.go
pkg/apis/jupyter/v1/openapi_generated.go: pkg/apis/jupyter/v1/types.go $(OPENAPI_GEN_BINARY)
	$(OPENAPI_GEN_BINARY) \
	--input-dirs $(PKG)/pkg/apis/jupyter/v1,k8s.io/apimachinery/pkg/apis/meta/v1,k8s.io/api/core/v1 \
	--output-package $(PKG)/pkg/apis/jupyter/v1 \
	--logtostderr \
	--report-filename /dev/null \
	--go-header-file=.header
	go fmt $@

bin/$(BIN): $(SRC) glide.yaml bin
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

local: $(SRC) glide.yaml bin
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

vendor:
	rm -rf glide.lock vendor
	glide install -v --skip-test

$(CLIENT_GEN_BINARY):
	go get -u -d k8s.io/code-generator/cmd/client-gen
	cd $(GOPATH)/src/k8s.io/code-generator; git checkout $(CODE_GENERATOR_VERSION)
	go install k8s.io/code-generator/cmd/client-gen

$(DEEPCOPY_GEN_BINARY):
	go get -u -d k8s.io/code-generator/cmd/deepcopy-gen
	cd $(GOPATH)/src/k8s.io/code-generator; git checkout $(CODE_GENERATOR_VERSION)
	go install k8s.io/code-generator/cmd/deepcopy-gen

$(INFORMER_GEN_BINARY):
	go get -u -d k8s.io/code-generator/cmd/informer-gen
	cd $(GOPATH)/src/k8s.io/code-generator; git checkout $(CODE_GENERATOR_VERSION)
	go install k8s.io/code-generator/cmd/informer-gen

$(LISTER_GEN_BINARY):
	go get -u -d k8s.io/code-generator/cmd/lister-gen
	cd $(GOPATH)/src/k8s.io/code-generator; git checkout $(CODE_GENERATOR_VERSION)
	go install k8s.io/code-generator/cmd/lister-gen

$(OPENAPI_GEN_BINARY):
	go get -u -d k8s.io/code-generator/cmd/openapi-gen
	go get -u -d k8s.io/kube-openapi/cmd/openapi-gen
	cd $(GOPATH)/src/k8s.io/code-generator; git checkout $(CODE_GENERATOR_VERSION); export OPENAPI_GEN_VERSION=$$(grep kube-openapi Godeps/Godeps.json -A 1 | tail -n 1 | awk '{print $$2}' | tr -d '"'); cd $(GOPATH)/src/k8s.io/kube-openapi; git checkout $$OPENAPI_GEN_VERSION
	go install k8s.io/kube-openapi/cmd/openapi-gen
