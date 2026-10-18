BIN_DIR   := bin
BINS      := $(BIN_DIR)/ai-rules $(BIN_DIR)/ai-reviewer
ARGS      ?=
TESTIGO_SPEC_DIRS := \
	./internal/application/applicationtest \
	./internal/adapters/anthropic/anthropictest \
	./internal/adapters/ghcli/ghclitest \
	./internal/adapters/markdown/markdowntest \
	./internal/adapters/sqlite/sqlitetest

TESTIGO_SPEC_GENERATED := $(patsubst %,%/zz_testigo_gen.go,$(TESTIGO_SPEC_DIRS))
TESTIGO_GENERATED := $(TESTIGO_SPEC_GENERATED) \
	./internal/application/zz_testigo_stubs_gen_test.go \
	./internal/adapters/sqlite/zz_testigo_stubs_gen_test.go

.PHONY: all build generate test coverage e2e vet fmt lint run clean

all: fmt vet test build

build: $(TESTIGO_GENERATED)
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/ai-rules ./cmd/ai-rules
	go build -o $(BIN_DIR)/ai-reviewer ./cmd/ai-reviewer

# The testigo_bootstrap tag excludes the hand-written extensions of the
# generated builders, so a specification package loads while its generated file
# is still missing.
TESTIGO_ENV := GOFLAGS=-tags=testigo_bootstrap

generate:
	$(TESTIGO_ENV) go generate ./...

$(TESTIGO_SPEC_GENERATED): %/zz_testigo_gen.go: %/spec.go
	$(TESTIGO_ENV) go tool testigo-gen -dir ./$*

./internal/application/zz_testigo_stubs_gen_test.go: ./internal/application/testigo_stubs_spec.go
	$(TESTIGO_ENV) go tool testigo-gen -dir ./internal/application -output zz_testigo_stubs_gen_test.go -type testigoStubsSpec

./internal/adapters/sqlite/zz_testigo_stubs_gen_test.go: ./internal/adapters/sqlite/testigo_stubs_spec.go
	$(TESTIGO_ENV) go tool testigo-gen -dir ./internal/adapters/sqlite -output zz_testigo_stubs_gen_test.go -type testigoStubsSpec

test: $(TESTIGO_GENERATED)
	go test ./...

coverage: $(TESTIGO_GENERATED)
	@packages="$$(go list ./... | grep -Ev '/(applicationtest|anthropictest|ghclitest|markdowntest|sqlitetest|ports)$$')"; \
	for package in $$packages; do \
		output="$$(go test -covermode=atomic $$package)" || { printf '%s\n' "$$output"; exit 1; }; \
		printf '%s\n' "$$output"; \
		case "$$package" in \
			*/internal/domain) ;; \
			*) printf '%s\n' "$$output" | grep -q 'coverage: 100.0% of statements' || { \
				printf 'coverage below 100%% for %s\n' "$$package" >&2; exit 1; \
			} ;; \
		esac; \
	done

e2e: $(TESTIGO_GENERATED)
	go test ./internal/application -run '^TestSyncThenPublishWithSQLite$$'

vet: $(TESTIGO_GENERATED)
	go vet ./...

fmt: $(TESTIGO_GENERATED)
	test -z "$$(gofmt -l .)"

lint: fmt vet

run: build
	$(BIN_DIR)/ai-reviewer $(ARGS)

clean:
	rm -rf $(BIN_DIR)
	rm -f $(TESTIGO_GENERATED)
