set dotenv-load := false

# ------------------------------------------------------------------
# Paths / Config
# ------------------------------------------------------------------

root := justfile_directory()
tool_path := root / ".tool/bin"

export PATH := tool_path + ":" + env_var("PATH")
export AQUA_ROOT_DIR := root + "/.tool"
export AQUA_DISABLE_POLICY := "true"

default:
    @just --list

shell:
    exec "${SHELL:-/bin/sh}" -i

# -----------------------------
# aqua bootstrap + installs
# -----------------------------

# install aqua itself into .tool/bin — no global install needed
aqua:
    mkdir -p "{{ tool_path }}"
    test -x "{{ tool_path }}/aqua" || curl -sSfL https://raw.githubusercontent.com/aquaproj/aqua-installer/v4.1.1/aqua-installer | bash

# install every tool pinned in .aqua.yaml (or a tag subset: just tools lint)
tools *tags: aqua
    aqua install {{ if tags == "" { "" } else { "-t " + replace(tags, " ", " -t ") } }}

check: tools
    just format-check
    just lint
    just test-ci
    just tidy
    @if [ -n "$(git status --porcelain)" ]; then \
        echo "Generation, tidy, or formatting left changes:"; \
        git status --porcelain; \
        git diff --stat; \
        exit 1; \
    fi

# Apply the Go formatters configured in .golangci.yml.
format target="./...":
    golangci-lint fmt {{ target }}

alias fmt := format

# Check formatting without rewriting files; propagate formatter errors too.
format-check target="./...":
    diff="$(golangci-lint fmt -d {{ target }})" && { printf '%s' "$diff"; test -z "$diff"; }

lint target="./...":
    golangci-lint run {{ target }}

alias lint-go := lint

lint-fix target="./...":
    golangci-lint run --fix {{ target }}

tidy:
    go mod tidy

test-ci:
    go test -short -timeout 2m ./...

test:
    go test -timeout 10m ./...

# Check reachable code for known vulnerabilities.
vulncheck:
    govulncheck ./...

# bump all module dependencies, then tidy
update:
    go get -u all
    just tidy
