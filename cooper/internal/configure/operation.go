package configure

import (
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/buildflow"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tui/loading"
)

type configureOperationResult struct {
	warnings []string
	err      error
}

func requestedStepNames(save saveModel, cfg *config.Config, cooperDir string) ([]string, error) {
	steps := app.SaveStepNames()
	if !save.buildRequested {
		return steps, nil
	}
	buildSteps, err := buildflow.StepNames(cfg, cooperDir)
	if err != nil {
		return nil, err
	}
	return append(steps, buildSteps...), nil
}

func runRequestedActionWithLoading(ca *app.ConfigureApp, cfg *config.Config, save saveModel) ([]string, error) {
	stepNames, err := requestedStepNames(save, cfg, ca.CooperDir())
	if err != nil {
		return nil, err
	}

	steps := make([]loading.LoadingStep, len(stepNames))
	for i, stepName := range stepNames {
		steps[i] = loading.LoadingStep{Name: stepName}
	}

	loadModel := loading.NewWithOptions(loading.Options{
		Steps:           steps,
		RunningSubtitle: "applying configuration...",
		DoneSubtitle:    "configuration applied",
		ErrorSubtitle:   "configuration failed",
		AllowCancel:     false,
	})
	p := tea.NewProgram(&configureLoadingAdapter{model: loadModel}, tea.WithAltScreen(), tea.WithMouseCellMotion())

	resultCh := make(chan configureOperationResult, 1)
	go func() {
		warnings, runErr := executeRequestedAction(ca, cfg, save, io.Discard, func(idx int, stepErr error) {
			if stepErr != nil {
				p.Send(loading.StepErrorMsg{Index: idx, Err: stepErr})
				return
			}
			p.Send(loading.StepCompleteMsg{Index: idx})
		})
		resultCh <- configureOperationResult{warnings: warnings, err: runErr}
	}()

	loadingResult, runErr := p.Run()
	result := <-resultCh
	if runErr != nil {
		return result.warnings, fmt.Errorf("loading screen: %w", runErr)
	}
	adapter, ok := loadingResult.(*configureLoadingAdapter)
	if !ok {
		if result.err != nil {
			return result.warnings, result.err
		}
		return result.warnings, fmt.Errorf("configure operation ended unexpectedly")
	}
	if adapter.model.HasError {
		if result.err != nil {
			return result.warnings, result.err
		}
		return result.warnings, fmt.Errorf("configure operation failed")
	}
	if !adapter.model.Done {
		return result.warnings, fmt.Errorf("configure operation cancelled")
	}
	return result.warnings, result.err
}

func executeRequestedAction(ca *app.ConfigureApp, cfg *config.Config, save saveModel, out io.Writer, report func(step int, err error)) ([]string, error) {
	syncConfigureApp(ca, cfg)
	warnings, err := ca.SaveWithProgress(func(step int, total int, name string, stepErr error) {
		if report != nil {
			report(step, stepErr)
		}
	})
	if err != nil {
		return warnings, err
	}
	if !save.buildRequested {
		return warnings, nil
	}
	buildOffset := len(app.SaveStepNames())
	buildErr := buildflow.Run(ca.Config(), ca.CooperDir(), buildflow.Options{
		NoCache: save.cleanBuildRequested,
		Out:     out,
		OnProgress: func(step int, total int, name string, stepErr error) {
			if report != nil {
				report(buildOffset+step, stepErr)
			}
		},
	})
	return warnings, buildErr
}

func syncConfigureApp(ca *app.ConfigureApp, cfg *config.Config) {
	if ca == nil || cfg == nil {
		return
	}
	ca.SetProgrammingTools(cfg.ProgrammingTools)
	ca.SetAITools(cfg.AITools)
	ca.SetWhitelistedDomains(cfg.WhitelistedDomains)
	ca.SetPortForwardRules(cfg.PortForwardRules)
	ca.SetBarrelEnvVars(cfg.BarrelEnvVars)
	ca.SetProxyPort(cfg.ProxyPort)
	ca.SetBridgePort(cfg.BridgePort)
	ca.SetBarrelSHMSize(cfg.BarrelSHMSize)
}

type configureLoadingAdapter struct {
	model loading.Model
}

func (a *configureLoadingAdapter) Init() tea.Cmd {
	return a.model.Init()
}

func (a *configureLoadingAdapter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := a.model.Update(msg)
	a.model = updated
	if a.model.Done && !a.model.HasError {
		return a, tea.Quit
	}
	return a, cmd
}

func (a *configureLoadingAdapter) View() string {
	return a.model.View(a.model.Width, a.model.Height)
}
