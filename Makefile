GO      ?= go
PKGS    := ./...

.DEFAULT_GOAL := ci

.PHONY: build
build:
	$(GO) build $(PKGS)

.PHONY: vet
vet:
	$(GO) vet $(PKGS)

.PHONY: test
test:
	$(GO) test -race -count=1 $(PKGS)

.PHONY: cover
cover:
	$(GO) test -race -count=1 -coverprofile=coverage.out $(PKGS)
	$(GO) tool cover -func=coverage.out | tail -n 1
	$(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: fmt
fmt:
	$(GO) fmt $(PKGS)

.PHONY: tidy
tidy:
	$(GO) mod tidy
	git diff --exit-code go.mod go.sum

.PHONY: lint
lint:
	golangci-lint run

.PHONY: ci
ci: build vet test lint

# Requires the live API key and places REAL orders against a REAL account.
# ₹499+tax/month subscription. Never wired into `make ci`.
.PHONY: test-live
test-live:
	@echo "!! This hits the live Groww API and can place real orders. !!"
	@echo "!! Requires GROWW_API_KEY and -tags=live.               !!"
	$(GO) test -tags=live -race -count=1 -run TestLive $(PKGS)
