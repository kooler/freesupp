.PHONY: all build build-web build-go test test-go test-web typecheck run dev-visitor dev-inbox deps clean

BINARY := freesupp
GO_LDFLAGS := -s -w

all: build

## build: build both frontends, then the binary with the assets embedded
build: build-web build-go

build-web:
	cd web/visitor && npm run build
	cd web/inbox && npm run build

build-go:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(GO_LDFLAGS)' -o $(BINARY) ./cmd/freesupp

## test: Go tests plus both Vitest suites
test: test-go test-web

test-go:
	go test ./...

test-web:
	cd web/visitor && npm test
	cd web/inbox && npm test

typecheck:
	cd web/visitor && npm run typecheck
	cd web/inbox && npm run typecheck

## run: local dev server on :8080 serving the last built assets.
## No SMTP or Turnstile: mail is logged, the captcha is skipped.
## The first visit to the inbox creates the first admin account.
BASE_URL ?= http://localhost:8080
DB_PATH ?= ./freesupp.db
SESSION_SECRET ?= dev-secret-not-for-production

run:
	BASE_URL='$(BASE_URL)' DB_PATH='$(DB_PATH)' \
	SESSION_SECRET='$(SESSION_SECRET)' \
	go run ./cmd/freesupp

## dev-visitor / dev-inbox: Vite dev servers proxying the API to :8080
dev-visitor:
	cd web/visitor && npm run dev

dev-inbox:
	cd web/inbox && npm run dev

deps:
	cd web/visitor && npm ci
	cd web/inbox && npm ci

clean:
	rm -f $(BINARY)
	rm -rf web/visitor/dist web/inbox/dist
