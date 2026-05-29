# buildctl

`buildctl` is a small generic Docker image build runner designed to be called from a `Justfile`, CI job, or local shell script.

It replaces repo-specific Bash build scripts by handling the reusable parts:

- project root discovery
- Git metadata build args
- GitHub token discovery for private Go modules
- Docker BuildKit secrets
- multi-platform defaults
- templated image tags
- optional glob-based Dockerfile builds
- optional image push behavior

The app-specific workflow stays in your `Justfile`.

---

## Running

```bash
go run github.com/otpki/otpki-pkg/tools/docker/buildctl --help
```

## Basic Usage

```bash
buildctl docker \
  --registry hub.omnitrustregistry.com/otpki-private \
  --image otpki-acme \
  --arg BUILDAPP=acme
```

By default, this builds `hub.omnitrustregistry.com/otpki-private/otpki-acme:latest` using:

```text
Dockerfile
context .
platforms linux/amd64,linux/arm64
```

## Defaults

Unless overriden, `buildctl docker` assumes:

```text
version       latest
tag           {version}
context       .
dockerfile    Dockerfile
platforms     linux/amd64,linux/arm64
push          false
push-env      PUSH
git-args      true
github-token  required
secret        id=github_token,env=GITHUB_TOKEN
```

When `--git-args=true`, these Docker build args are added automatically:
```text
GIT_BRANCH
GIT_HASH
GIT_TAG
SOURCE_DATE_EPOCH
```

These values come from the environment first, then Git.

## GitHub Token Behavior

By default, buildctl requires a GitHub token because many Docker builds need access to private Go modules.

It looks for:

1. `GITHUB_TOKEN`
1. `gh auth token`

The token is exposed to Docker BuildKit using:

`--secret id=github_token,env=GITHUB_TOKEN`

Disable this requirement with:

```bash
buildctl docker --github-token-required=false ...
```

## Push Behavior

Push explicitly:

```bash
buildctl docker \
  --registry hub.omnitrustregistry.com/otpki-private \
  --image otpki-acme \
  --arg BUILDAPP=acme \
  --push
```

Or push using an environment variable:

```bash
PUSH=true buildctl docker \
  --registry hub.omnitrustregistry.com/otpki-private \
  --image otpki-acme \
  --arg BUILDAPP=acme
```

For scripts that must use a different environment variable:

```bash
PUSH_IMAGE=1 buildctl docker \
  --registry hub.omnitrustregistry.com/otpki-private \
  --image otpki-pkcs11-proxy \
  --push-env PUSH_IMAGE
```

Truthy values are:
```text
1
true
yes
y
on
```

## Template Variables

Some flag values support simple `{name}` templates.

Common variables:

```text
{registry}
{image}
{version}
{context}
{dockerfile}
{platforms}
{target}
{build_tags}
{git_branch}
{git_hash}
{git_tag}
{source_date_epoch}
```

When using `--glob`, these are also available:

```text
{dockerfile}
{dockerfile_abs}
{dockerfile_dir}
{dockerfile_dir_base}
{dockerfile_base}
{dockerfile_suffix}
```

For this Dockerfile: `deploy/docker/utimaco/Dockerfile.alpine`, the derived variables are:

```text
dockerfile              deploy/docker/utimaco/Dockerfile.alpine
dockerfile_abs          /absolute/path/to/repo/deploy/docker/utimaco/Dockerfile.alpine
dockerfile_dir          deploy/docker/utimaco
dockerfile_dir_base     utimaco
dockerfile_base         Dockerfile.alpine
dockerfile_suffix       alpine
```

## Justfile Examples

### Single image

Replaces a script that always builds `otpki-acme`:

```just
build-acme registry="hub.omnitrustregistry.com/otpki-private" version="latest":
    buildctl docker \
      --registry {{registry}} \
      --image otpki-acme \
      --version {{version}} \
      --arg BUILDAPP=acme
```

Run:

```bash
just build-acme
PUSH=true just build-acme some-other-registry.com 1.0.1
```

### Multi-service image builds

This keeps the service list and service-specific target logic in the Justfile.

```just
registry := "hub.omnitrustregistry.com/otpki-private"
version := env_var_or_default("VERSION", "latest")

_build app target:
    buildctl docker \
      --registry {{registry}} \
      --image otpki-{{app}} \
      --version {{version}} \
      --target {{target}} \
      --arg BUILDAPP={{app}}

build app:
    @case "{{app}}" in \
      all|gateway) just _build {{app}} runtime ;; \
      issuance|enrollment|validation) just _build {{app}} no-ui ;; \
      *) echo "Unknown app: {{app}}" && exit 1 ;; \
    esac

build-all:
    just build all
    just build gateway
    just build issuance
    just build enrollment
    just build validation
```

### Globbed Dockerfile builds

This replaces loops like:

```bash
for dockerfile in deploy/docker/*/Dockerfile.*; do
  ...
done
```

Justfile:
```just
registry := "hub.omnitrustregistry.com/otpki-private"
version := env_var_or_default("VERSION", "latest")
build_tags := env_var_or_default("BUILD_TAGS", "none")

build-pkcs11:
    buildctl docker \
      --registry {{registry}} \
      --image otpki-pkcs11-proxy \
      --version {{version}} \
      --tag '{version}-{dockerfile_dir_base}-{dockerfile_suffix}' \
      --glob 'deploy/docker/*/Dockerfile.*' \
      --platforms linux/amd64,linux/arm64 \
      --platform-override dockerfile_dir_base=utimaco:linux/amd64 \
      --arg BUILD_TAGS={{build_tags}} \
      --push-env PUSH_IMAGE
```

For this file, `deploy/docker/utimaco/Dockerfile.alpine`, due to the `--tag '{version}-{dockerfile_dir_base}-{dockerfile_suffix}'`, it builds:

`hub.omnitrustregistry.com/otpki-private/otpki-pkcs11-proxy:latest-utimaco-alpine`


The platform override, `--platform-override dockerfile_dir_base=utimaco:linux/amd64`, means all Dockerfiles under:

`deploy/docker/utimaco/`

build only for:

`linux/amd64`

All other matched Dockerfiles use the default:

`linux/amd64,linux/arm64`

Run:

```bash
just build-pkcs11
VERSION=v1.2.3 just build-pkcs11
PUSH_IMAGE=1 just build-pkcs11
```

## CLI Examples

### Build with a specific version

```bash
buildctl docker \
  --registry hub.omnitrustregistry.com/otpki-private \
  --image otpki-acme \
  --version v1.2.3 \
  --arg BUILDAPP=acme
```
Builds:

`hub.omnitrustregistry.com/otpki-private/otpki-acme:v1.2.3`

### Use a custom tag

```bash
buildctl docker \
  --registry hub.omnitrustregistry.com/otpki-private \
  --image otpki-acme \
  --version v1.2.3 \
  --tag '{version}-{git_hash}' \
  --arg BUILDAPP=acme
```

Example output image:

`hub.omnitrustregistry.com/otpki-private/otpki-acme:v1.2.3-a1b2c3d`

### Use a specific Dockerfile

```bash
buildctl docker \
  --registry hub.omnitrustregistry.com/otpki-private \
  --image otpki-worker \
  --dockerfile deploy/docker/worker/Dockerfile \
  --context . \
  --arg BUILDAPP=worker
```

### Use a Docker build target
```bash
buildctl docker \
  --registry hub.omnitrustregistry.com/otpki-private \
  --image otpki-gateway \
  --target runtime \
  --arg BUILDAPP=gateway
```

### Build for one platform
```bash
buildctl docker \
  --registry hub.omnitrustregistry.com/otpki-private \
  --image otpki-acme \
  --platforms linux/amd64 \
  --arg BUILDAPP=acme
```

### Dry run
```bash
buildctl docker \
  --registry hub.omnitrustregistry.com/otpki-private \
  --image otpki-acme \
  --arg BUILDAPP=acme \
  --dry-run
```


This prints the Docker command without running it.

### Add extra build args

```bash
buildctl docker \
  --registry hub.omnitrustregistry.com/otpki-private \
  --image otpki-acme \
  --arg BUILDAPP=acme \
  --arg FEATURE_FLAG=true \
  --arg BUILD_TAGS=pkcs11,hsm
```

### Set template variables manually

Use --set key=value for custom template values.

```bash
buildctl docker \
  --registry hub.omnitrustregistry.com/otpki-private \
  --image otpki-example \
  --tag '{version}-{flavor}' \
  --set flavor=debug
```

Builds:

`hub.omnitrustregistry.com/otpki-private/otpki-example:latest-debug`

## Optional config file

buildctl is intended to work well from a Justfile, so config files are optional.

By default, it looks for `.buildctl.yaml` in the current directory or home directory.

Example:

```yaml
registry: hub.omnitrustregistry.com/otpki-private
platforms: linux/amd64,linux/arm64
git-args: true
github-token-required: true
```

Then your Justfile can be shorter:

```just
version := env_var_or_default("VERSION", "latest")

build-acme:
    buildctl docker \
      --image otpki-acme \
      --version {{version}} \
      --arg BUILDAPP=acme
```


### Use a specific config file:

```bash
buildctl --config ./deploy/buildctl.yaml docker --image otpki-acme --arg BUILDAPP=acme
```

## Environment variables

buildctl supports `BUILDCTL_` environment variables for most flags:

```bash
BUILDCTL_REGISTRY=hub.omnitrustregistry.com/otpki-private \
BUILDCTL_VERSION=v1.2.3 \
buildctl docker --image otpki-acme --arg BUILDAPP=acme
```


It also supports compatibility variables from the previous Bash scripts:

```text
REGISTRY
VERSION
PUSH
PUSH_IMAGE
BUILD_TAGS
GIT_BRANCH
GIT_HASH
GIT_TAG
SOURCE_DATE_EPOCH
GITHUB_TOKEN
```
