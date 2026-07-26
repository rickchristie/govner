package configure

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rickchristie/govner/cooper/internal/app"
	"github.com/rickchristie/govner/cooper/internal/buildflow"
	"github.com/rickchristie/govner/cooper/internal/config"
	"github.com/rickchristie/govner/cooper/internal/tui/loading"
)

type configurePreparationResult struct {
	warnings []string
	prepared *buildflow.Prepared
	err      error
}

func requestedPreparationStepNames(save saveModel) []string {
	steps := app.SaveStepNames()
	if !save.buildRequested {
		return steps
	}
	return append(steps, buildflow.StagingStepNames()...)
}

func runRequestedActionWithLoading(ca *app.ConfigureApp, cfg *config.Config, save saveModel) ([]string, error) {
	stepNames := requestedPreparationStepNames(save)
	steps := make([]loading.LoadingStep, len(stepNames))
	for i, stepName := range stepNames {
		steps[i] = loading.LoadingStep{Name: stepName}
	}

	runningSubtitle := "applying configuration..."
	doneSubtitle := "configuration applied"
	if save.buildRequested {
		runningSubtitle = "preparing configuration..."
		doneSubtitle = "ready to build Docker images"
	}
	loadModel := loading.NewWithOptions(loading.Options{
		Steps:           steps,
		RunningSubtitle: runningSubtitle,
		DoneSubtitle:    doneSubtitle,
		ErrorSubtitle:   "configuration failed",
		AllowCancel:     false,
	})
	p := tea.NewProgram(&configureLoadingAdapter{model: loadModel}, tea.WithAltScreen(), tea.WithMouseCellMotion())

	resultCh := make(chan configurePreparationResult, 1)
	go func() {
		warnings, prepared, runErr := executeRequestedPreparation(ca, cfg, save, func(idx int, stepErr error) {
			if stepErr != nil {
				p.Send(loading.StepErrorMsg{Index: idx, Err: stepErr})
				return
			}
			p.Send(loading.StepCompleteMsg{Index: idx})
		})
		resultCh <- configurePreparationResult{warnings: warnings, prepared: prepared, err: runErr}
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
	if result.err != nil {
		return result.warnings, result.err
	}
	if result.prepared != nil {
		if err := runPreparedBuildWithFeedback(result.prepared, save.cleanBuildRequested); err != nil {
			return result.warnings, err
		}
	}
	return result.warnings, nil
}

func executeRequestedPreparation(ca *app.ConfigureApp, cfg *config.Config, save saveModel, report func(step int, err error)) ([]string, *buildflow.Prepared, error) {
	syncConfigureApp(ca, cfg)
	saveProgress := func(step int, total int, name string, stepErr error) {
		if report != nil {
			report(step, stepErr)
		}
	}
	var (
		warnings []string
		implicit []config.ImplicitToolConfig
		err      error
	)
	if save.buildRequested {
		warnings, implicit, err = ca.SaveForBuildWithProgress(saveProgress)
	} else {
		warnings, err = ca.SaveWithProgress(saveProgress)
	}
	if err != nil {
		return warnings, nil, err
	}
	if !save.buildRequested {
		return warnings, nil, nil
	}
	buildOffset := len(app.SaveStepNames())
	prepared, prepareErr := buildflow.Stage(ca.Config(), ca.CooperDir(), implicit, buildflow.Options{
		OnProgress: func(step int, total int, name string, stepErr error) {
			if report != nil {
				report(buildOffset+step, stepErr)
			}
		},
	})
	return warnings, prepared, prepareErr
}

func runPreparedBuildWithFeedback(prepared *buildflow.Prepared, noCache bool) error {
	model := newBuildFeedbackModel(prepared.StepNames())
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	resultCh := make(chan error, 1)
	go func() {
		buildErr := prepared.Build(buildflow.Options{
			NoCache: noCache,
			OnOutput: func(line string) {
				p.Send(dockerBuildLineMsg{Line: line})
			},
			OnProgress: func(step int, total int, name string, stepErr error) {
				p.Send(dockerBuildStepFinishedMsg{Index: step, Err: stepErr})
			},
		})
		resultCh <- buildErr
		p.Send(dockerBuildFinishedMsg{Err: buildErr})
	}()

	finalModel, runErr := p.Run()
	buildErr := <-resultCh
	if runErr != nil {
		return fmt.Errorf("docker build feedback screen: %w", runErr)
	}
	if _, ok := finalModel.(*buildFeedbackModel); !ok {
		if buildErr != nil {
			return buildErr
		}
		return fmt.Errorf("docker build feedback screen ended unexpectedly")
	}
	return buildErr
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
