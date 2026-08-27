.PHONY: build test cover lint ci soak live run tidy clean help

BIN        := bin/wake
PKG        := ./...

# The coverage floor, applied to every package rather than to the total. The
# reasoning is in scripts/covergate.awk.
COVER_MIN  := 80

# There is no exemption. cmd/wake had one at 76% while `attach` was untestable -
# it opened a terminal on the first line that mattered - and the gate was built
# to fail once the package outgrew it so the exemption would be deleted rather
# than forgotten. That is what happened: `attach` now waits for the daemon to
# confirm the spawn before it builds a TUI, so its refusal paths are reachable
# from a test, and the package cleared the floor on its own.

# How long `make soak` runs. Short enough to be worth running in a review,
# and the only thing that changes for the advertised hour: SOAK_DURATION=1h.
SOAK_DURATION ?= 30s

help: ## Show available targets
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[33m%-8s\033[0m %s\n", $$1, $$2}'

# WAKE_SOCKET points every target that can start a daemon at a scratch path,
# so nothing run through this Makefile can reach the socket a real fleet uses.
#
# This exists because it did not. An agent cleaning up a daemon its own test had
# leaked ran `go run ./cmd/wake stop` with no WAKE_SOCKET set, so it resolved to
# ~/.wake/daemon.sock and stopped the owner's fleet - three sessions, two of
# whose transcripts were not on disk to recover. `wake stop` is the one
# irreversible verb in this project, and the safe path required remembering an
# environment variable while the dangerous one was the default.
#
# The default is right for a user and wrong for a working tree. This closes the
# working tree. It does NOT close a bare `go run ./cmd/wake ...` typed by hand -
# see the rule in CLAUDE.md, which is the only thing that covers that.
WAKE_SOCKET ?= $(shell mktemp -d)/wake.sock
export WAKE_SOCKET

build: ## Compile the wake binary
	go build -o $(BIN) ./cmd/wake

run: build ## Build and start wake
	$(BIN)

test: ## Run tests both with and without the race detector
	go test $(PKG) -race -count=1
	go test $(PKG) -count=1

cover: ## Run tests and enforce the coverage gate on every package
	go test $(PKG) -race -count=1 -coverprofile=coverage.out
	@go tool cover -func=coverage.out | tail -1
	@# Nothing is exempt. The gate still takes the two exemption variables
	@# because that is its interface, and empty is how "no package is exempt" is
	@# spelled - which keeps the seam visible for the next package that needs a
	@# temporary floor.
	@#
	@# Deleting the exemption is also what makes the gate's *own* first guard
	@# load-bearing. covergate.awk says so in its END block: an unset or renamed
	@# COVER_MIN arrives as the empty string, compares as 0 and passes every
	@# package, and while an exemption existed that was caught only by accident -
	@# the ratchet fired first, because any package is at or above a floor of
	@# zero. With no exemption the ratchet never runs, and the min check is the
	@# only thing between a typo here and a gate that reports green over nothing.
	@awk -v min=$(COVER_MIN) -v xpkg='' -v xmin='' \
	  -f scripts/covergate.awk coverage.out

lint: ## Run golangci-lint
	golangci-lint run

# Everything CI runs, in one command, for the days CI cannot run it - a spend
# cap, an outage, or a change nobody wants to burn ten-times-billed macOS
# minutes iterating on. The runner is macos-latest and so is the machine this
# is for, which is the whole reason it can stand in at all: the workflow header
# says the platform is the point.
#
# It is a second copy of that step list, which is the thing this project
# otherwise forbids. What makes it allowed is that it cannot drift in silence:
# internal/core's TestMakeCIRunsEveryStepTheWorkflowRuns reads both files and
# requires the same set of commands, so a step added to one and not the other
# is a build failure rather than a local check that quietly proves less than it
# claims. A stand-in nobody can trust is worse than no stand-in.
#
# The cheap jobs go first so a typo fails in seconds rather than after three
# suite runs. Within the test job the workflow's own order is kept: build
# before test, because the tests do not build cmd/wake and would not notice it
# was broken.
#
# Two things it does not replicate, both of them structural. A clean checkout:
# this runs the working tree, which is the point. And a second machine: the
# runner is slower and busier, which is where load-sensitive tests fail and
# this cannot make them.
ci: ## Run every step CI runs (see .github/workflows/ci.yml)
	@echo "TMPDIR=$$TMPDIR ($$(printf %s "$$TMPDIR" | wc -c | tr -d ' ') bytes) - CI pins its own; whether it fits sun_path is measured by internal/daemon"
	golangci-lint config verify
	$(MAKE) lint
	go mod tidy -diff
	$(MAKE) build
	GOOS=windows go build ./...
	$(MAKE) test
	$(MAKE) cover

soak: ## 20 fake sessions replaying fixtures, then the same through a daemon (SOAK_DURATION=1h make soak for the long one)
	@# The guard exists because this target was a green no-op for the whole of
	@# Phase 1: there was no TestSoak anywhere in the tree, and `go test -run`
	@# reports "ok ... [no tests to run]" and exits 0 when its pattern matches
	@# nothing. -list matches without running, so this fails loudly instead.
	@go test ./internal/core -tags=soak -list='^TestSoak$$' | grep -qx TestSoak \
	  || { echo "no TestSoak under -tags=soak: this target would pass without running anything"; exit 1; }
	@go test ./internal/daemon -tags=soak -list='^TestSoakDaemon$$' | grep -qx TestSoakDaemon \
	  || { echo "no TestSoakDaemon under -tags=soak: this target would pass without running anything"; exit 1; }
	go test ./internal/core -race -run TestSoak -timeout 90m -tags=soak -v -soak.duration=$(SOAK_DURATION)
	@# The daemon lane. core churns sessions in-process; a leak that costs one
	@# goroutine per connection or one process per lifecycle only accumulates
	@# once there is a socket and clients on it.
	go test ./internal/daemon -race -run TestSoakDaemon -timeout 90m -tags=soak -v -soak.duration=$(SOAK_DURATION)

live: ## SPENDS MONEY: one fleet of real `claude` agents through the pty harness
	@# The only target here that costs anything, and the only one needing a
	@# network, credentials and a `claude` on disk. It is not in `ci` and never
	@# will be - CLAUDE.md's rule is that a live model may not sit on a gate, and
	@# the build tag is what keeps that true rather than this comment.
	@#
	@# Read it with -v: every phase logs its rendered frame, and the frames are
	@# the deliverable. A green run nobody read has answered nothing.
	@#
	@# Same anti-vacuous guard as soak, for the same reason: `-run` against a
	@# pattern matching nothing exits 0 and reports "ok".
	@go test ./cmd/wake -tags=live -list='^TestLiveJourney$$' | grep -qx TestLiveJourney \
	  || { echo "no TestLiveJourney under -tags=live: this target would pass without running anything"; exit 1; }
	@echo "This spends real money on a real model. Ctrl-C within 3s to stop."
	@sleep 3
	go test ./cmd/wake -tags=live -run TestLiveJourney -timeout 20m -v -count=1

tidy: ## Sync go.mod and go.sum
	go mod tidy

clean: ## Remove build output and coverage data
	rm -rf bin dist coverage.out coverage.html
