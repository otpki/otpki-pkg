set dotenv-load := false

# ------------------------------------------------------------------
# Paths / Config
# ------------------------------------------------------------------

root            := justfile_directory()
tool_path       := root / ".tool/bin"

export PATH := tool_path + ":" + env_var("PATH")
export AQUA_ROOT_DIR := root + "/.tool"
export AQUA_DISABLE_POLICY := "true"

default:
    @just --list

# -----------------------------
# aqua bootstrap + installs
# -----------------------------

# install aqua into the tools directory
aqua:
    mkdir -p {{tool_path}}
    test -x {{tool_path}}/aqua || curl -sSfL https://raw.githubusercontent.com/aquaproj/aqua-installer/v4.0.2/aqua-installer | bash

tools *tags:
    aqua install {{ if tags == "" { "" } else { "-t " + replace(tags, " ", " -t ") } }}

check: aqua tools
    just format-check
    just lint
    just test-ci
    go mod tidy

format target="./...":
    {{tool_path}}/golangci-lint fmt {{target}}

# Check formatting without rewriting files; propagate formatter errors too.
format-check target="./...":
    diff="$(golangci-lint fmt -d {{target}})" && { printf '%s' "$diff"; test -z "$diff"; }

lint target="./...":
    {{tool_path}}/golangci-lint run {{target}}

lint-fix target="./...":
    {{tool_path}}/golangci-lint run --fix {{target}}

test-ci:
    go test -short -timeout 2m ./...

test:
    go test -timeout 10m ./...
