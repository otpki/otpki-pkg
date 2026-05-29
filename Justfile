set dotenv-load := false

# ------------------------------------------------------------------
# Paths / Config
# ------------------------------------------------------------------

root            := justfile_directory()
tool_path       := root / ".tool"

export PATH := tool_path + ":" + env_var("PATH")

default:
    @just --list

# install binny into the tools directory
binny:
    mkdir -p {{tool_path}}
    test -x {{tool_path}}/binny || curl -sSfL https://get.anchore.io/binny | sh -s -- -b {{tool_path}}

tools:
    {{tool_path}}/binny install

check: binny tools test-ci # todo: fmt/lint
    go mod tidy

format target="":
    {{tool_path}}/golangci-lint fmt {{target}}

lint target="":
    {{tool_path}}/golangci-lint run {{target}}

lint-fix target="":
    {{tool_path}}/golangci-lint run --fix {{target}}

test-ci:
    go test -short -timeout 2m ./...

test:
    go test -timeout 10m ./...
