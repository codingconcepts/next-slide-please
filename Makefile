# Patterns for the local dev stack. The bracketed first letter stops pkill
# matching the shell that make is running it in.
#
# go run does not forward signals, so its compiled child has to be matched
# separately. The signal server is matched by path, which scopes it to the
# wrangler, workerd and esbuild processes belonging to this checkout.
NSP_PROCS := (go run \./cmd/[n]sp|/[n]sp (present|speak))
SIGNAL_PROCS := ${CURDIR}/signal/[n]ode_modules
SIGNAL_PORT := 8787

.PHONY: down
down:
	@echo "==> stopping nsp clients"
	@pkill -f '${NSP_PROCS}' || true
	@echo "==> stopping the signalling server"
	@pkill -f '${SIGNAL_PROCS}' || true
	@sleep 2
	@# Anything that ignored SIGTERM, or was orphaned by go run exiting first.
	@pkill -9 -f '${NSP_PROCS}' || true
	@pkill -9 -f '${SIGNAL_PROCS}' || true
	@if lsof -ti :${SIGNAL_PORT} >/dev/null 2>&1; then \
		echo "==> port ${SIGNAL_PORT} is still held by:"; \
		lsof -i :${SIGNAL_PORT}; \
	else \
		echo "==> everything stopped"; \
	fi

.PHONY: test
test:
	go test ./...

# The end to end tests need a signalling server to meet through. Run them
# against the local Worker rather than the deployed one.
.PHONY: test_e2e
test_e2e:
	NSP_SIGNAL_SERVER=http://localhost:${SIGNAL_PORT} go test -count=1 ./...

validate_version:
ifndef VERSION
	$(error VERSION is undefined)
endif

# Local install for day to day use. Built straight into DEV_BIN rather than
# into the working tree and moved, so a failed build cannot leave a stray
# binary behind or clobber the directory if DEV_BIN is missing.
#
# CGO_ENABLED=0 to match the release build - what you use should be what
# ships. The version is stamped from git so `nsp version` says something
# useful about a local build; -ldflags has to carry it alongside -s -w,
# since a second -ldflags would replace the first rather than add to it.
DEV_BIN := ${HOME}/dev/bin

.PHONY: build
build:
	@mkdir -p ${DEV_BIN}
	CGO_ENABLED=0 go build \
		-ldflags "-s -w -X main.version=$(shell git describe --tags --always --dirty)" \
		-o ${DEV_BIN}/nsp ./cmd/nsp
	@echo "==> installed $$(${DEV_BIN}/nsp version) to ${DEV_BIN}/nsp"

# Releasing is a tag push and nothing else. The workflow in
# .github/workflows/release.yml builds every target, attaches the binaries to
# a GitHub release, and rewrites Formula/nsp.rb in codingconcepts/homebrew-tap
# so `brew install codingconcepts/tap/nsp` picks the new version up.
#
# CGO_ENABLED=0 is why one Linux runner can build all five: nsp reaches
# CoreGraphics through purego at runtime, so there is no C toolchain in the
# build and every target can be built from any machine.
.PHONY: release
release: validate_version
	git tag -a ${VERSION} -m ${VERSION}
	git push origin ${VERSION}

deploy: build
	(cd signal && npx wrangler deploy)

# The install script has one home, at the repo root, and the site serves a
# copy of it from https://nextslideplease.run/install.sh. Copying it in at
# deploy time rather than keeping two files means the published script can
# never drift from the one in the repository.
.PHONY: site_assets
site_assets:
	cp install.sh site/public/install.sh

.PHONY: deploy_site
deploy_site: site_assets
	(cd site && npx wrangler deploy)

.PHONY: dev_site
dev_site: site_assets
	(cd site && npx wrangler dev)