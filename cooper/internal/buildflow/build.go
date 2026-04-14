package buildflow

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/docker"
	"github.com/rickchristie/govner/cooper/internal/templates"
)

// Options controls how a build run reports progress and output.
type Options struct {
	NoCache    bool
	Out        io.Writer
	OnProgress func(step int, total int, name string, err error)
}

var builtinAITools = map[string]bool{
	"claude":   true,
	"copilot":  true,
	"codex":    true,
	"opencode": true,
}

type plan struct {
	enabledAITools []string
	customImages   []string
}

// StepNames returns the ordered build progress labels for the current config.
func StepNames(cfg *config.Config, cooperDir string) ([]string, error) {
	p, err := buildPlan(cfg, cooperDir)
	if err != nil {
		return nil, err
	}
	return p.stepNames(), nil
}

// Run performs the full proxy/base/CLI image build used by `cooper build`
// and by configure's Save & Build flow.
func Run(cfg *config.Config, cooperDir string, opts Options) error {
	p, err := buildPlan(cfg, cooperDir)
	if err != nil {
		return err
	}
	stepNames := p.stepNames()
	report := func(step int, err error) {
		if opts.OnProgress == nil {
			return
		}
		opts.OnProgress(step, len(stepNames), stepNames[step], err)
	}
	out := opts.Out
	if out == nil {
		out = io.Discard
	}

	baseDir := filepath.Join(cooperDir, "base")
	cliDir := filepath.Join(cooperDir, "cli")
	proxyDir := filepath.Join(cooperDir, "proxy")
	for _, d := range []string{baseDir, cliDir, proxyDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			err = fmt.Errorf("create dir %s: %w", d, err)
			report(0, err)
			return err
		}
	}

	// Step 0: resolve desired versions and implicit tooling before rendering templates.
	fmt.Fprintln(out, "Resolving tool versions...")
	if _, err := config.RefreshDesiredToolVersions(cfg, config.DesiredVersionRefreshOptions{AllowStaleFallback: false}); err != nil {
		err = fmt.Errorf("resolve desired tool versions: %w", err)
		report(0, err)
		return err
	}
	implicit, err := config.ResolveImplicitTools(cfg)
	if err != nil {
		err = fmt.Errorf("resolve implicit tools: %w", err)
		report(0, err)
		return err
	}
	report(0, nil)

	// Step 1: regenerate every cooper-managed template from the desired state.
	fmt.Fprintln(out, "Generating templates...")
	if err := templates.WriteAllTemplates(baseDir, cliDir, cfg, implicit); err != nil {
		err = fmt.Errorf("write templates: %w", err)
		report(1, err)
		return err
	}
	if err := templates.WriteProxyTemplates(proxyDir, cfg); err != nil {
		err = fmt.Errorf("write proxy templates: %w", err)
		report(1, err)
		return err
	}
	report(1, nil)

	// Step 2: ensure the CA exists before we stage it into build contexts.
	fmt.Fprintln(out, "Ensuring CA certificate...")
	caCertPath, caKeyPath, err := config.EnsureCA(cooperDir)
	if err != nil {
		err = fmt.Errorf("ensure CA: %w", err)
		report(2, err)
		return err
	}
	report(2, nil)

	// Step 3: render the ACL helper into the proxy build context.
	fmt.Fprintln(out, "Writing ACL helper source...")
	if err := templates.WriteACLHelperSource(proxyDir); err != nil {
		err = fmt.Errorf("write acl helper source: %w", err)
		report(3, err)
		return err
	}
	report(3, nil)

	// Step 4: stage CA materials after generation so Docker sees fresh inputs.
	fmt.Fprintln(out, "Staging CA files into build contexts...")
	if err := copyFile(caCertPath, filepath.Join(baseDir, "cooper-ca.pem")); err != nil {
		err = fmt.Errorf("stage CA cert into base dir: %w", err)
		report(4, err)
		return err
	}
	if err := copyFile(caCertPath, filepath.Join(proxyDir, "cooper-ca.pem")); err != nil {
		err = fmt.Errorf("stage CA cert into proxy dir: %w", err)
		report(4, err)
		return err
	}
	if err := copyFile(caKeyPath, filepath.Join(proxyDir, "cooper-ca-key.pem")); err != nil {
		err = fmt.Errorf("stage CA key into proxy dir: %w", err)
		report(4, err)
		return err
	}
	report(4, nil)

	proxyDockerfile := filepath.Join(proxyDir, "proxy.Dockerfile")
	uidGidArgs := map[string]string{
		"USER_UID": fmt.Sprintf("%d", os.Getuid()),
		"USER_GID": fmt.Sprintf("%d", os.Getgid()),
	}

	// Step 5: build the proxy image first because the base/tool images depend on shared runtime assets.
	fmt.Fprintln(out, "Building proxy image...")
	if err := buildImage(out, docker.GetImageProxy(), proxyDockerfile, proxyDir, uidGidArgs, opts.NoCache); err != nil {
		err = fmt.Errorf("build proxy image: %w", err)
		report(5, err)
		return err
	}
	report(5, nil)

	// Step 6: build the base image and persist its built-state immediately.
	fmt.Fprintln(out, "Building base image...")
	baseDockerfile := filepath.Join(baseDir, "Dockerfile")
	if err := buildImage(out, docker.GetImageBase(), baseDockerfile, baseDir, uidGidArgs, opts.NoCache); err != nil {
		err = fmt.Errorf("build base image: %w", err)
		report(6, err)
		return err
	}
	updateProgrammingToolContainerVersions(cfg)
	setBuiltBaseNodeVersion(cfg)
	setBuiltImplicitTools(cfg, implicit)
	configPath := filepath.Join(cooperDir, "config.json")
	if err := config.SaveConfig(configPath, cfg); err != nil {
		err = fmt.Errorf("save config after base build: %w", err)
		report(6, err)
		return err
	}
	report(6, nil)

	step := 7
	for _, toolName := range p.enabledAITools {
		toolDir := filepath.Join(cliDir, toolName)
		dockerfile := filepath.Join(toolDir, "Dockerfile")
		fmt.Fprintf(out, "Building %s image...\n", toolName)
		if err := buildImage(out, docker.GetImageCLI(toolName), dockerfile, toolDir, nil, opts.NoCache); err != nil {
			err = fmt.Errorf("build %s image: %w", toolName, err)
			report(step, err)
			return err
		}
		updateAIToolContainerVersion(cfg, toolName)
		if err := config.SaveConfig(configPath, cfg); err != nil {
			err = fmt.Errorf("save config after %s build: %w", toolName, err)
			report(step, err)
			return err
		}
		report(step, nil)
		step++
	}

	for _, name := range p.customImages {
		customDir := filepath.Join(cliDir, name)
		customDockerfile := filepath.Join(customDir, "Dockerfile")
		fmt.Fprintf(out, "Building custom image %s...\n", name)
		if err := buildImage(out, docker.GetImageCLI(name), customDockerfile, customDir, nil, opts.NoCache); err != nil {
			err = fmt.Errorf("build custom image %s: %w", name, err)
			report(step, err)
			return err
		}
		report(step, nil)
		step++
	}

	fmt.Fprintln(out, "Build complete.")
	return nil
}

func buildPlan(cfg *config.Config, cooperDir string) (plan, error) {
	var enabledAITools []string
	if cfg != nil {
		for _, tool := range cfg.AITools {
			if tool.Enabled {
				enabledAITools = append(enabledAITools, tool.Name)
			}
		}
	}
	customImages, err := discoverCustomImageNames(filepath.Join(cooperDir, "cli"))
	if err != nil {
		return plan{}, err
	}
	return plan{enabledAITools: enabledAITools, customImages: customImages}, nil
}

func (p plan) stepNames() []string {
	steps := []string{
		"Resolving tool versions...",
		"Generating templates...",
		"Ensuring CA certificate...",
		"Writing ACL helper source...",
		"Staging CA files...",
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

func buildImage(out io.Writer, name, dockerfilePath, contextDir string, buildArgs map[string]string, noCache bool) error {
	lines, errc := docker.BuildImageWithOutput(name, dockerfilePath, contextDir, buildArgs, noCache)
	for line := range lines {
		fmt.Fprintln(out, line)
	}
	if err := <-errc; err != nil {
		return err
	}
	return nil
}

func discoverCustomImageNames(cliDir string) ([]string, error) {
	entries, err := os.ReadDir(cliDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read cli directory %s: %w", cliDir, err)
	}
	custom := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || builtinAITools[entry.Name()] {
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
