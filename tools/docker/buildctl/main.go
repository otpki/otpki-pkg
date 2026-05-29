package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samber/oops"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	defaultVersion    = "latest"
	defaultTag        = "{version}"
	defaultContext    = "."
	defaultDockerfile = "Dockerfile"
	defaultPlatforms  = "linux/amd64,linux/arm64"
	defaultPushEnv    = "PUSH"
	defaultSecret     = "id=github_token,env=GITHUB_TOKEN"
)

var (
	cfgFile string
	setVars []string
)

type Config struct {
	Registry            string
	Image               string
	Version             string
	TagTemplate         string
	ProjectRoot         string
	Context             string
	Dockerfile          string
	Platforms           string
	Target              string
	Args                []string
	Secrets             []string
	Glob                string
	PlatformOverrides   []string
	Push                bool
	PushEnv             string
	DryRun              bool
	GitArgs             bool
	GitHubTokenRequired bool
	SetVars             []string
}

type Build struct {
	Image      string
	Context    string
	Dockerfile string
	Platforms  string
	Target     string
	Args       map[string]string
	Secrets    []string
	Vars       map[string]string
}

type PlatformOverride struct {
	Key       string
	Value     string
	Platforms string
}

func main() {
	root := newRootCommand()

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "buildctl",
		Short: "Generic Docker image builder",
	}

	root.PersistentFlags().StringVar(&cfgFile, "config", "", "Optional config file")
	root.PersistentFlags().StringSliceVar(&setVars, "set", nil, "Set template variable, repeatable as key=value")

	root.AddCommand(newDockerCommand())

	cobra.OnInitialize(initConfig)

	return root
}

func newDockerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docker",
		Short: "Build one or more Docker images",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			if cfg.ProjectRoot == "" {
				cfg.ProjectRoot, err = findProjectRoot()
				if err != nil {
					return err
				}
			}

			vars := baseVars(cfg)
			if err := applySetVars(vars, cfg.SetVars); err != nil {
				return err
			}

			if cfg.GitHubTokenRequired {
				token := firstNonEmpty(os.Getenv("GITHUB_TOKEN"), ghToken())
				if token == "" {
					return oops.New("GITHUB_TOKEN is required. Set GITHUB_TOKEN or login with: gh auth login")
				}

				if err := os.Setenv("GITHUB_TOKEN", token); err != nil {
					return err
				}
			}

			builds, err := makeBuilds(cfg, vars)
			if err != nil {
				return err
			}

			for _, build := range builds {
				if err := runBuild(cfg, build); err != nil {
					return err
				}
			}

			fmt.Println("Build completed!")
			return nil
		},
	}

	flags := cmd.Flags()

	flags.String("registry", "", "Container registry/repository prefix")
	flags.String("image", "", "Image name, without registry and tag unless you intentionally want a fully qualified image")
	flags.String("version", defaultVersion, "Version used by {version}")
	flags.String("tag", defaultTag, "Tag template, for example {version} or {version}-{dockerfile_dir_base}-{dockerfile_suffix}")
	flags.String("project-root", "", "Project root. Defaults to git root or nearest Dockerfile directory")
	flags.String("context", defaultContext, "Docker build context")
	flags.String("dockerfile", defaultDockerfile, "Dockerfile path")
	flags.String("platforms", defaultPlatforms, "Docker platforms")
	flags.String("target", "", "Docker build target")
	flags.StringSlice("arg", nil, "Docker build arg, repeatable as KEY=VALUE")
	flags.StringSlice("secret", []string{defaultSecret}, "Docker BuildKit secret, repeatable")
	flags.String("glob", "", "Glob of Dockerfiles to build, for example deploy/docker/*/Dockerfile.*")
	flags.StringSlice("platform-override", nil, "Platform override in KEY=VALUE:PLATFORMS form")
	flags.Bool("push", false, "Push image after building")
	flags.String("push-env", defaultPushEnv, "Environment variable that enables push when truthy")
	flags.Bool("dry-run", false, "Print Docker commands without running them")
	flags.Bool("git-args", true, "Automatically add Git metadata build args")
	flags.Bool("github-token-required", true, "Require GITHUB_TOKEN or gh auth token")

	bindFlag(cmd, "registry")
	bindFlag(cmd, "image")
	bindFlag(cmd, "version")
	bindFlag(cmd, "tag")
	bindFlag(cmd, "project-root")
	bindFlag(cmd, "context")
	bindFlag(cmd, "dockerfile")
	bindFlag(cmd, "platforms")
	bindFlag(cmd, "target")
	bindFlag(cmd, "arg")
	bindFlag(cmd, "secret")
	bindFlag(cmd, "glob")
	bindFlag(cmd, "platform-override")
	bindFlag(cmd, "push")
	bindFlag(cmd, "push-env")
	bindFlag(cmd, "dry-run")
	bindFlag(cmd, "git-args")
	bindFlag(cmd, "github-token-required")

	return cmd
}

func initConfig() {
	viper.SetEnvPrefix("BUILDCTL")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	// Backward-compatible env vars from the bash scripts.
	_ = viper.BindEnv("registry", "REGISTRY")
	_ = viper.BindEnv("version", "VERSION")
	_ = viper.BindEnv("push", "PUSH")
	_ = viper.BindEnv("push_image", "PUSH_IMAGE")
	_ = viper.BindEnv("build_tags", "BUILD_TAGS")
	_ = viper.BindEnv("git_branch", "GIT_BRANCH")
	_ = viper.BindEnv("git_hash", "GIT_HASH")
	_ = viper.BindEnv("git_tag", "GIT_TAG")
	_ = viper.BindEnv("source_date_epoch", "SOURCE_DATE_EPOCH")

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigName(".buildctl")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
		viper.AddConfigPath("$HOME")
	}

	_ = viper.ReadInConfig()
}

func bindFlag(cmd *cobra.Command, name string) {
	if err := viper.BindPFlag(name, cmd.Flags().Lookup(name)); err != nil {
		panic(err)
	}
}

func loadConfig() (Config, error) {
	cfg := Config{
		Registry:            viper.GetString("registry"),
		Image:               viper.GetString("image"),
		Version:             valueOrDefault(viper.GetString("version"), defaultVersion),
		TagTemplate:         valueOrDefault(viper.GetString("tag"), defaultTag),
		ProjectRoot:         viper.GetString("project-root"),
		Context:             valueOrDefault(viper.GetString("context"), defaultContext),
		Dockerfile:          valueOrDefault(viper.GetString("dockerfile"), defaultDockerfile),
		Platforms:           valueOrDefault(viper.GetString("platforms"), defaultPlatforms),
		Target:              viper.GetString("target"),
		Args:                viper.GetStringSlice("arg"),
		Secrets:             viper.GetStringSlice("secret"),
		Glob:                viper.GetString("glob"),
		PlatformOverrides:   viper.GetStringSlice("platform-override"),
		Push:                viper.GetBool("push"),
		PushEnv:             valueOrDefault(viper.GetString("push-env"), defaultPushEnv),
		DryRun:              viper.GetBool("dry-run"),
		GitArgs:             viper.GetBool("git-args"),
		GitHubTokenRequired: viper.GetBool("github-token-required"),
		SetVars:             setVars,
	}

	// Support PUSH_IMAGE=1 style without requiring --push-env PUSH_IMAGE
	// if the config or command explicitly uses push_image.
	if truthy(viper.GetString("push_image")) {
		cfg.Push = true
	}

	if cfg.PushEnv != "" && truthy(os.Getenv(cfg.PushEnv)) {
		cfg.Push = true
	}

	if len(cfg.Secrets) == 0 {
		cfg.Secrets = []string{defaultSecret}
	}

	if cfg.Image == "" {
		return Config{}, oops.New("--image is required")
	}

	return cfg, nil
}

func baseVars(cfg Config) map[string]string {
	gitBranch := firstNonEmpty(viper.GetString("git_branch"), os.Getenv("GIT_BRANCH"))
	gitHash := firstNonEmpty(viper.GetString("git_hash"), os.Getenv("GIT_HASH"))
	gitTag := firstNonEmpty(viper.GetString("git_tag"), os.Getenv("GIT_TAG"))
	sourceDateEpoch := firstNonEmpty(viper.GetString("source_date_epoch"), os.Getenv("SOURCE_DATE_EPOCH"))

	if gitBranch == "" {
		gitBranch = gitOutput(cfg.ProjectRoot, "rev-parse", "--abbrev-ref", "HEAD")
	}
	if gitHash == "" {
		gitHash = gitOutput(cfg.ProjectRoot, "rev-parse", "--short", "HEAD")
	}
	if gitTag == "" {
		gitTag = gitTagsAtHead(cfg.ProjectRoot)
	}
	if sourceDateEpoch == "" {
		sourceDateEpoch = gitOutput(cfg.ProjectRoot, "show", "-s", "--format=%ct", "HEAD")
	}

	return map[string]string{
		"registry":          cfg.Registry,
		"image":             cfg.Image,
		"version":           cfg.Version,
		"context":           cfg.Context,
		"dockerfile":        cfg.Dockerfile,
		"platforms":         cfg.Platforms,
		"target":            cfg.Target,
		"build_tags":        firstNonEmpty(viper.GetString("build_tags"), os.Getenv("BUILD_TAGS")),
		"git_branch":        gitBranch,
		"git_hash":          gitHash,
		"git_tag":           gitTag,
		"source_date_epoch": sourceDateEpoch,
	}
}

func applySetVars(vars map[string]string, pairs []string) error {
	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return oops.Errorf("invalid --set %q, expected key=value", pair)
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if key == "" {
			return oops.Errorf("invalid --set %q, empty key", pair)
		}

		vars[key] = value
	}

	return nil
}

func makeBuilds(cfg Config, base map[string]string) ([]Build, error) {
	if cfg.Glob == "" {
		return makeSingleBuild(cfg, cloneVars(base))
	}

	return makeGlobBuilds(cfg, base)
}

func makeSingleBuild(cfg Config, vars map[string]string) ([]Build, error) {
	vars["dockerfile"] = cfg.Dockerfile
	addDockerfileVars(vars, cfg.ProjectRoot, cfg.Dockerfile)

	build, err := renderBuild(cfg, vars)
	if err != nil {
		return nil, err
	}

	return []Build{build}, nil
}

func makeGlobBuilds(cfg Config, base map[string]string) ([]Build, error) {
	globPath := cfg.Glob
	if !filepath.IsAbs(globPath) {
		globPath = filepath.Join(cfg.ProjectRoot, globPath)
	}

	matches, err := filepath.Glob(globPath)
	if err != nil {
		return nil, err
	}

	sort.Strings(matches)

	if len(matches) == 0 {
		return nil, oops.Errorf("glob %q matched no files", cfg.Glob)
	}

	var builds []Build

	for _, match := range matches {
		vars := cloneVars(base)

		rel := match
		if r, err := filepath.Rel(cfg.ProjectRoot, match); err == nil {
			rel = r
		}

		vars["dockerfile"] = filepath.ToSlash(rel)
		addDockerfileVars(vars, cfg.ProjectRoot, rel)

		build, err := renderBuild(cfg, vars)
		if err != nil {
			return nil, err
		}

		builds = append(builds, build)
	}

	return builds, nil
}

func addDockerfileVars(vars map[string]string, projectRoot string, dockerfile string) {
	path := filepath.ToSlash(dockerfile)
	dir := filepath.ToSlash(filepath.Dir(path))
	base := filepath.Base(path)

	vars["dockerfile"] = path
	vars["dockerfile_dir"] = dir
	vars["dockerfile_dir_base"] = filepath.Base(dir)
	vars["dockerfile_base"] = base
	vars["dockerfile_suffix"] = dockerfileSuffix(base)
	vars["dockerfile_abs"] = resolvePath(projectRoot, path)
}

func dockerfileSuffix(base string) string {
	const prefix = "Dockerfile."
	if strings.HasPrefix(base, prefix) {
		return strings.TrimPrefix(base, prefix)
	}

	return ""
}

func renderBuild(cfg Config, vars map[string]string) (Build, error) {
	args, err := parseArgPairs(cfg.Args, vars)
	if err != nil {
		return Build{}, err
	}

	if cfg.GitArgs {
		addGitArgs(args, vars)
	}

	contextValue, err := renderSimple(cfg.Context, vars)
	if err != nil {
		return Build{}, oops.Errorf("context: %w", err)
	}

	dockerfileValue, err := renderSimple(vars["dockerfile"], vars)
	if err != nil {
		return Build{}, oops.Errorf("dockerfile: %w", err)
	}

	platformsValue, err := renderSimple(cfg.Platforms, vars)
	if err != nil {
		return Build{}, oops.Errorf("platforms: %w", err)
	}

	targetValue, err := renderSimple(cfg.Target, vars)
	if err != nil {
		return Build{}, oops.Errorf("target: %w", err)
	}

	tag, err := renderSimple(cfg.TagTemplate, vars)
	if err != nil {
		return Build{}, oops.Errorf("tag: %w", err)
	}

	imageName, err := renderSimple(cfg.Image, vars)
	if err != nil {
		return Build{}, oops.Errorf("image: %w", err)
	}

	fullImage := fullImageName(cfg.Registry, imageName, tag)

	overrides, err := parsePlatformOverrides(cfg.PlatformOverrides)
	if err != nil {
		return Build{}, err
	}

	for _, override := range overrides {
		if vars[override.Key] == override.Value {
			platformsValue = override.Platforms
		}
	}

	return Build{
		Image:      fullImage,
		Context:    contextValue,
		Dockerfile: dockerfileValue,
		Platforms:  platformsValue,
		Target:     targetValue,
		Args:       args,
		Secrets:    cfg.Secrets,
		Vars:       vars,
	}, nil
}

func parseArgPairs(pairs []string, vars map[string]string) (map[string]string, error) {
	out := map[string]string{}

	for _, pair := range pairs {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, oops.Errorf("invalid --arg %q, expected KEY=VALUE", pair)
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		if key == "" {
			return nil, oops.Errorf("invalid --arg %q, empty key", pair)
		}

		rendered, err := renderSimple(value, vars)
		if err != nil {
			return nil, oops.Errorf("arg %s: %w", key, err)
		}

		out[key] = rendered
	}

	return out, nil
}

func addGitArgs(args map[string]string, vars map[string]string) {
	args["GIT_BRANCH"] = vars["git_branch"]
	args["GIT_HASH"] = vars["git_hash"]
	args["GIT_TAG"] = vars["git_tag"]
	args["SOURCE_DATE_EPOCH"] = vars["source_date_epoch"]
}

func fullImageName(registry, image, tag string) string {
	image = strings.TrimSpace(image)
	registry = strings.TrimSpace(registry)
	tag = strings.TrimSpace(tag)

	if tag != "" && !strings.Contains(filepath.Base(image), ":") {
		image = image + ":" + tag
	}

	if registry == "" {
		return image
	}

	if strings.HasPrefix(image, registry+"/") {
		return image
	}

	return strings.TrimRight(registry, "/") + "/" + strings.TrimLeft(image, "/")
}

func parsePlatformOverrides(values []string) ([]PlatformOverride, error) {
	var out []PlatformOverride

	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		condition, platforms, ok := strings.Cut(raw, ":")
		if !ok {
			return nil, oops.Errorf("invalid --platform-override %q, expected KEY=VALUE:PLATFORMS", raw)
		}

		key, value, ok := strings.Cut(condition, "=")
		if !ok {
			return nil, oops.Errorf("invalid --platform-override %q, expected KEY=VALUE:PLATFORMS", raw)
		}

		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		platforms = strings.TrimSpace(platforms)

		if key == "" || value == "" || platforms == "" {
			return nil, oops.Errorf("invalid --platform-override %q, expected KEY=VALUE:PLATFORMS", raw)
		}

		out = append(out, PlatformOverride{
			Key:       key,
			Value:     value,
			Platforms: platforms,
		})
	}

	return out, nil
}

func runBuild(cfg Config, build Build) error {
	contextPath := resolvePath(cfg.ProjectRoot, build.Context)
	dockerfilePath := resolvePath(cfg.ProjectRoot, build.Dockerfile)

	args := []string{
		"build",
		"--platform", build.Platforms,
	}

	if build.Target != "" {
		args = append(args, "--target", build.Target)
	}

	argKeys := make([]string, 0, len(build.Args))
	for key := range build.Args {
		argKeys = append(argKeys, key)
	}
	sort.Strings(argKeys)

	for _, key := range argKeys {
		args = append(args, "--build-arg", key+"="+build.Args[key])
	}

	for _, secret := range build.Secrets {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			continue
		}
		args = append(args, "--secret", secret)
	}

	args = append(args,
		"-t", build.Image,
		"-f", dockerfilePath,
		contextPath,
	)

	fmt.Printf("Building %s...\n", build.Image)

	if err := runCommand(cfg, "docker", args...); err != nil {
		return err
	}

	fmt.Printf("Built %s\n", build.Image)

	if cfg.Push {
		fmt.Printf("Pushing %s...\n", build.Image)
		if err := runCommand(cfg, "docker", "push", build.Image); err != nil {
			return err
		}
	}

	return nil
}

func runCommand(cfg Config, name string, args ...string) error {
	fmt.Printf("+ %s %s\n", name, shellJoin(args))

	if cfg.DryRun {
		return nil
	}

	cmd := exec.Command(name, args...)
	cmd.Dir = cfg.ProjectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

// renderSimple renders {key} templates. It intentionally avoids Go-template
// syntax so usage from Justfiles stays readable.
func renderSimple(input string, vars map[string]string) (string, error) {
	out := input

	for i := 0; i < 100; i++ {
		start := strings.Index(out, "{")
		if start < 0 {
			return out, nil
		}

		end := strings.Index(out[start:], "}")
		if end < 0 {
			return "", oops.Errorf("unclosed template expression in %q", input)
		}

		end = start + end
		key := strings.TrimSpace(out[start+1 : end])

		if key == "" {
			return "", oops.Errorf("empty template expression in %q", input)
		}

		value, ok := vars[key]
		if !ok {
			return "", oops.Errorf("unknown template variable %q in %q", key, input)
		}

		out = out[:start] + value + out[end+1:]
	}

	return "", oops.Errorf("too many template expansions in %q", input)
}

func findProjectRoot() (string, error) {
	if root := gitOutput("", "rev-parse", "--show-toplevel"); root != "" {
		return root, nil
	}

	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if fileExists(filepath.Join(dir, "Dockerfile")) {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}

		dir = parent
	}

	return "", oops.New("could not find project root. Run inside a git repo, near a Dockerfile, or pass --project-root")
}

func gitOutput(dir string, args ...string) string {
	return commandOutput(dir, "git", args...)
}

func gitTagsAtHead(dir string) string {
	out := gitOutput(dir, "tag", "--points-at", "HEAD")
	if out == "" {
		return ""
	}

	return strings.Join(strings.Fields(out), ",")
}

func ghToken() string {
	return strings.TrimSpace(commandOutput("", "gh", "auth", "token"))
}

func commandOutput(dir, name string, args ...string) string {
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil

	if err := cmd.Run(); err != nil {
		return ""
	}

	return strings.TrimSpace(out.String())
}

func resolvePath(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(root, path)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func cloneVars(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}

	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}

	return ""
}

func valueOrDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}

	return value
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))

	for _, arg := range args {
		if strings.ContainsAny(arg, " \t\n\"'") {
			quoted = append(quoted, fmt.Sprintf("%q", arg))
		} else {
			quoted = append(quoted, arg)
		}
	}

	return strings.Join(quoted, " ")
}
