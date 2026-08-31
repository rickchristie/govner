package buildflow

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/rickchristie/govner/cooper/internal/aitool"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/templates"
)

// Options controls how a build run reports progress and output.
type Options struct {
	NoCache bool
	// Out receives line-oriented progress and combined Docker stdout/stderr.
	Out io.Writer
	// OnOutput receives the same lines as Out and is intended for typed UI
	// adapters that must not expose an io.Writer to their presentation model.
	OnOutput func(line string)
	// OnProgress reports a completed or failed step.
	OnProgress func(step int, total int, name string, err error)
	// imageBuild is injectable only inside this package's tests; callers use
	// the production Docker boundary.
	imageBuild imageBuildFunc
}

type imageBuildFunc func(name, dockerfilePath, contextDir string, buildArgs map[string]string, noCache bool) (<-chan string, <-chan error)

var preparationStepNames = []string{
	"Resolving tool versions...",
	"Generating templates...",
	"Ensuring CA certificate...",
	"Writing ACL helper source...",
	"Staging CA files...",
}

var stagingStepNames = []string{
	"Writing ACL helper source...",
	"Staging CA files...",
}

type plan struct {
	enabledAITools []string
	customImages   []string
}

// Prepared is the stable build plan produced after Cooper has generated all
// templates and staged the CA files. Build updates only the built-state fields
// on the owned configuration as each image succeeds. Keeping this boundary
// explicit lets interactive callers finish their deterministic preparation UI
// before the slower Docker phase starts streaming output.
type Prepared struct {
	cfg        *config.Config
	cooperDir  string
	plan       plan
	implicit   []config.ImplicitToolConfig
	baseDir    string
	cliDir     string
	proxyDir   string
	configPath string
}

// PreparationStepNames returns the ordered non-Docker work performed by
// Prepare. The last step is always staging the CA files into build contexts.
func PreparationStepNames() []string {
	return append([]string(nil), preparationStepNames...)
}

// StagingStepNames returns the final pre-Docker steps for callers that already
// resolved versions, generated templates, and ensured the CA while saving.
func StagingStepNames() []string {
	return append([]string(nil), stagingStepNames...)
}

// StepNames returns the ordered build progress labels for the current config.
func StepNames(cfg *config.Config, cooperDir string) ([]string, error) {
	p, err := buildPlan(cfg, cooperDir)
	if err != nil {
		return nil, err
	}
	return append(PreparationStepNames(), p.buildStepNames()...), nil
}

// Run performs the full proxy/base/CLI image build used by `cooper build`
// and by configure's Save & Build flow.
func Run(cfg *config.Config, cooperDir string, opts Options) error {
	stepNames, err := StepNames(cfg, cooperDir)
	if err != nil {
		return err
	}
	preparationCount := len(preparationStepNames)
	prepared, err := Prepare(cfg, cooperDir, Options{
		Out:      opts.Out,
		OnOutput: opts.OnOutput,
		OnProgress: func(step int, _ int, name string, stepErr error) {
			if opts.OnProgress != nil {
				opts.OnProgress(step, len(stepNames), name, stepErr)
			}
		},
	})
	if err != nil {
		return err
	}
	return prepared.Build(Options{
		NoCache:  opts.NoCache,
		Out:      opts.Out,
		OnOutput: opts.OnOutput,
		OnProgress: func(step int, _ int, name string, stepErr error) {
			if opts.OnProgress != nil {
				opts.OnProgress(preparationCount+step, len(stepNames), name, stepErr)
			}
		},
		imageBuild: opts.imageBuild,
	})
}

// Prepare performs every deterministic filesystem and configuration step
// required before Docker starts. It does not execute a Docker command.
func Prepare(cfg *config.Config, cooperDir string, opts Options) (*Prepared, error) {
	p, err := buildPlan(cfg, cooperDir)
	if err != nil {
		return nil, err
	}
	// Prepare owns configuration migrations that affect generated templates.
	// StepNames and buildPlan must not mutate caller state.
	cfg.MergeDefaultDomains()
	stepNames := PreparationStepNames()
	report := func(step int, err error) {
		if opts.OnProgress == nil {
			return
		}
		opts.OnProgress(step, len(stepNames), stepNames[step], err)
	}

	baseDir := filepath.Join(cooperDir, "base")
	cliDir := filepath.Join(cooperDir, "cli")
	proxyDir := filepath.Join(cooperDir, "proxy")
	for _, d := range []string{baseDir, cliDir, proxyDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			err = fmt.Errorf("create dir %s: %w", d, err)
			report(0, err)
			return nil, err
		}
	}

	// Step 0: resolve desired versions and implicit tooling before rendering templates.
	emitOutput(opts, "Resolving tool versions...")
	if _, err := config.RefreshDesiredToolVersions(cfg, config.DesiredVersionRefreshOptions{AllowStaleFallback: false}); err != nil {
		err = fmt.Errorf("resolve desired tool versions: %w", err)
		report(0, err)
		return nil, err
	}
	implicit, err := config.ResolveImplicitTools(cfg)
	if err != nil {
		err = fmt.Errorf("resolve implicit tools: %w", err)
		report(0, err)
		return nil, err
	}
	report(0, nil)

	// Step 1: regenerate every cooper-managed template from the desired state.
	emitOutput(opts, "Generating templates...")
	if err := templates.WriteAllTemplates(baseDir, cliDir, cfg, implicit); err != nil {
		err = fmt.Errorf("write templates: %w", err)
		report(1, err)
		return nil, err
	}
	if err := templates.WriteProxyTemplates(proxyDir, cfg); err != nil {
		err = fmt.Errorf("write proxy templates: %w", err)
		report(1, err)
		return nil, err
	}
	report(1, nil)

	// Step 2: ensure the CA exists before we stage it into build contexts.
	emitOutput(opts, "Ensuring CA certificate...")
	caCertPath, caKeyPath, err := config.EnsureCA(cooperDir)
	if err != nil {
		err = fmt.Errorf("ensure CA: %w", err)
		report(2, err)
		return nil, err
	}
	report(2, nil)

	return stagePrepared(cfg, cooperDir, p, implicit, caCertPath, caKeyPath, opts, func(step int, stepErr error) {
		report(3+step, stepErr)
	})
}

// Stage finishes a Save & Build preparation without repeating the strict
// version resolution, template generation, or CA setup already completed by
// ConfigureApp.SaveForBuildWithProgress.
func Stage(cfg *config.Config, cooperDir string, implicit []config.ImplicitToolConfig, opts Options) (*Prepared, error) {
	p, err := buildPlan(cfg, cooperDir)
	if err != nil {
		return nil, err
	}
	stepNames := StagingStepNames()
	report := func(step int, stepErr error) {
		if opts.OnProgress != nil {
			opts.OnProgress(step, len(stepNames), stepNames[step], stepErr)
		}
	}
	return stagePrepared(cfg, cooperDir, p, implicit, "", "", opts, report)
}

func stagePrepared(
	cfg *config.Config,
	cooperDir string,
	p plan,
	implicit []config.ImplicitToolConfig,
	caCertPath string,
	caKeyPath string,
	opts Options,
	report func(step int, err error),
) (*Prepared, error) {
	baseDir := filepath.Join(cooperDir, "base")
	cliDir := filepath.Join(cooperDir, "cli")
	proxyDir := filepath.Join(cooperDir, "proxy")
	for _, dir := range []string{baseDir, cliDir, proxyDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			err = fmt.Errorf("create dir %s: %w", dir, err)
			report(0, err)
			return nil, err
		}
	}

	emitOutput(opts, "Writing ACL helper source...")
	if err := templates.WriteACLHelperSource(proxyDir); err != nil {
		err = fmt.Errorf("write acl helper source: %w", err)
		report(0, err)
		return nil, err
	}
	report(0, nil)

	emitOutput(opts, "Staging CA files into build contexts...")
	if caCertPath == "" || caKeyPath == "" {
		var err error
		caCertPath, caKeyPath, err = config.EnsureCA(cooperDir)
		if err != nil {
			err = fmt.Errorf("ensure CA before staging: %w", err)
			report(1, err)
			return nil, err
		}
	}
	if err := copyFile(caCertPath, filepath.Join(baseDir, "cooper-ca.pem")); err != nil {
		err = fmt.Errorf("stage CA cert into base dir: %w", err)
		report(1, err)
		return nil, err
	}
	if err := copyFile(caCertPath, filepath.Join(proxyDir, "cooper-ca.pem")); err != nil {
		err = fmt.Errorf("stage CA cert into proxy dir: %w", err)
		report(1, err)
		return nil, err
	}
	if err := copyFile(caKeyPath, filepath.Join(proxyDir, "cooper-ca-key.pem")); err != nil {
		err = fmt.Errorf("stage CA key into proxy dir: %w", err)
		report(1, err)
		return nil, err
	}
	report(1, nil)

	return &Prepared{
		cfg:        cfg,
		cooperDir:  cooperDir,
		plan:       p,
		implicit:   append([]config.ImplicitToolConfig(nil), implicit...),
		baseDir:    baseDir,
		cliDir:     cliDir,
		proxyDir:   proxyDir,
		configPath: filepath.Join(cooperDir, "config.json"),
	}, nil
}

// StepNames returns the Docker image steps that Build will execute.
func (p *Prepared) StepNames() []string {
	if p == nil {
		return nil
	}
	return p.plan.buildStepNames()
}

// Build executes the slow Docker phase and streams every combined
// stdout/stderr line through Options.OnOutput and Options.Out.
func (p *Prepared) Build(opts Options) error {
	if p == nil {
		return fmt.Errorf("build preparation is nil")
	}
	stepNames := p.StepNames()
	report := func(step int, err error) {
		if opts.OnProgress == nil {
			return
		}
		opts.OnProgress(step, len(stepNames), stepNames[step], err)
	}

	proxyDockerfile := filepath.Join(p.proxyDir, "proxy.Dockerfile")
	uidGidArgs := map[string]string{
		"USER_UID": fmt.Sprintf("%d", os.Getuid()),
		"USER_GID": fmt.Sprintf("%d", os.Getgid()),
	}

	// Step 0: build the proxy image first because the base/tool images depend on shared runtime assets.
	emitOutput(opts, "Building proxy image...")
	if err := buildImage(opts, docker.GetImageProxy(), proxyDockerfile, p.proxyDir, uidGidArgs, opts.NoCache); err != nil {
		err = fmt.Errorf("build proxy image: %w", err)
		report(0, err)
		return err
	}
	report(0, nil)

	// Step 1: build the base image and persist its built-state immediately.
	emitOutput(opts, "Building base image...")
	baseDockerfile := filepath.Join(p.baseDir, "Dockerfile")
	if err := buildImage(opts, docker.GetImageBase(), baseDockerfile, p.baseDir, uidGidArgs, opts.NoCache); err != nil {
		err = fmt.Errorf("build base image: %w", err)
		report(1, err)
		return err
	}
	updateProgrammingToolContainerVersions(p.cfg)
	setBuiltBaseNodeVersion(p.cfg)
	setBuiltImplicitTools(p.cfg, p.implicit)
	if err := config.SaveConfig(p.configPath, p.cfg); err != nil {
		err = fmt.Errorf("save config after base build: %w", err)
		report(1, err)
		return err
	}
	report(1, nil)

	step := 2
	for _, toolName := range p.plan.enabledAITools {
		toolDir := filepath.Join(p.cliDir, toolName)
		dockerfile := filepath.Join(toolDir, "Dockerfile")
		emitOutput(opts, fmt.Sprintf("Building %s image...", toolName))
		if err := buildImage(opts, docker.GetImageCLI(toolName), dockerfile, toolDir, nil, opts.NoCache); err != nil {
			err = fmt.Errorf("build %s image: %w", toolName, err)
			report(step, err)
			return err
		}
		updateAIToolContainerVersion(p.cfg, toolName)
		if err := config.SaveConfig(p.configPath, p.cfg); err != nil {
			err = fmt.Errorf("save config after %s build: %w", toolName, err)
			report(step, err)
			return err
		}
		report(step, nil)
		step++
	}

	for _, name := range p.plan.customImages {
		customDir := filepath.Join(p.cliDir, name)
		customDockerfile := filepath.Join(customDir, "Dockerfile")
		emitOutput(opts, fmt.Sprintf("Building custom image %s...", name))
		if err := buildImage(opts, docker.GetImageCLI(name), customDockerfile, customDir, nil, opts.NoCache); err != nil {
			err = fmt.Errorf("build custom image %s: %w", name, err)
			report(step, err)
			return err
		}
		report(step, nil)
		step++
	}

	emitOutput(opts, "Build complete.")
	return nil
}

func buildPlan(cfg *config.Config, cooperDir string) (plan, error) {
	cliDir := filepath.Join(cooperDir, "cli")
	var enabledAITools []string
	if cfg != nil {
		for _, tool := range cfg.AITools {
			if tool.Enabled {
				enabledAITools = append(enabledAITools, tool.Name)
			}
		}
	}
	customImages, err := DiscoverCustomImageNames(cliDir)
	if err != nil {
		return plan{}, err
	}
	return plan{enabledAITools: enabledAITools, customImages: customImages}, nil
}

func (p plan) buildStepNames() []string {
	steps := []string{
		"Building proxy image...",
		"Building base image...",
	}
	for _, toolName := range p.enabledAITools {
		steps = append(steps, fmt.Sprintf("Building %s image...", toolName))
	}
	for _, name := range p.customImages {
		steps = append(steps, fmt.Sprintf("Building custom image %s...", name))
	}
	return steps
}

func buildImage(opts Options, name, dockerfilePath, contextDir string, buildArgs map[string]string, noCache bool) error {
	builder := opts.imageBuild
	if builder == nil {
		builder = docker.BuildImageWithOutput
	}
	lines, errc := builder(name, dockerfilePath, contextDir, buildArgs, noCache)
	for line := range lines {
		emitOutput(opts, line)
	}
	if err := <-errc; err != nil {
		return err
	}
	return nil
}

func emitOutput(opts Options, line string) {
	if opts.Out != nil {
		fmt.Fprintln(opts.Out, line)
	}
	if opts.OnOutput != nil {
		opts.OnOutput(line)
	}
}

// DiscoverCustomImageNames returns user-managed CLI image names. It skips all
// built-in directories. It returns a conflict for a user-managed cli/grok path,
// including when Grok is disabled.
func DiscoverCustomImageNames(cliDir string) ([]string, error) {
	entries, err := os.ReadDir(cliDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read cli directory %s: %w", cliDir, err)
	}
	if err := templates.ValidateGrokOutputDir(cliDir); err != nil {
		return nil, err
	}
	custom := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || aitool.IsBuiltin(entry.Name()) {
			continue
		}
		if !fileExists(filepath.Join(cliDir, entry.Name(), "Dockerfile")) {
			continue
		}
		custom = append(custom, entry.Name())
	}
	sort.Strings(custom)
	return custom, nil
}

func updateProgrammingToolContainerVersions(cfg *config.Config) {
	for i := range cfg.ProgrammingTools {
		cfg.ProgrammingTools[i].RefreshContainerVersion()
	}
}

func updateAIToolContainerVersion(cfg *config.Config, toolName string) {
	for i := range cfg.AITools {
		if cfg.AITools[i].Name != toolName {
			continue
		}
		cfg.AITools[i].RefreshContainerVersion()
		return
	}
}

func setBuiltBaseNodeVersion(cfg *config.Config) {
	if cfg == nil {
		return
	}
	version, err := config.EffectiveBaseNodeVersion(cfg)
	if err != nil {
		return
	}
	cfg.BaseNodeVersion = version
}

func setBuiltImplicitTools(cfg *config.Config, tools []config.ImplicitToolConfig) {
	if cfg == nil {
		return
	}
	cfg.ImplicitTools = append([]config.ImplicitToolConfig(nil), tools...)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
