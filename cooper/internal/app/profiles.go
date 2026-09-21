package app

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/rickchristie/govner/cooper/internal/profilemanager"
	"github.com/rickchristie/govner/cooper/internal/profiles"
)

func (a *CooperApp) profileService() (*profiles.Service, error) {
	workspace, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return profilemanager.New(a.cooperDir, workspace, home)
}

func (a *CooperApp) ListProfiles(ctx context.Context) ([]profiles.Summary, error) {
	ctx, done, err := a.profileTasks.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	service, err := a.profileService()
	if err != nil {
		return nil, err
	}
	return service.List(ctx)
}

func (a *CooperApp) SaveProfile(ctx context.Context, request profiles.SaveRequest) (profiles.Result, error) {
	ctx, done, err := a.profileTasks.begin(ctx)
	if err != nil {
		return profiles.Result{}, err
	}
	defer done()
	if err := profilemanager.CheckHost(); err != nil {
		return profiles.Result{}, err
	}
	service, err := a.profileService()
	if err != nil {
		return profiles.Result{}, err
	}
	return service.Save(ctx, request)
}

func (a *CooperApp) LoadProfile(ctx context.Context, request profiles.LoadRequest) (profiles.Result, error) {
	ctx, done, err := a.profileTasks.begin(ctx)
	if err != nil {
		return profiles.Result{}, err
	}
	defer done()
	if err := profilemanager.CheckHost(); err != nil {
		return profiles.Result{}, err
	}
	service, err := a.profileService()
	if err != nil {
		return profiles.Result{}, err
	}
	return service.Load(ctx, request)
}

func (a *CooperApp) DeleteProfile(ctx context.Context, harness, name string) error {
	ctx, done, err := a.profileTasks.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	if err := profilemanager.CheckHost(); err != nil {
		return err
	}
	service, err := a.profileService()
	if err != nil {
		return err
	}
	return service.Delete(ctx, harness, name)
}

// Shutdown cancels profile operations and waits for rollback before infrastructure
// stops. The zero value is ready to use, including in application tests.
type profileOperations struct {
	mu      sync.Mutex
	wait    sync.WaitGroup
	closed  bool
	next    int
	cancels map[int]context.CancelFunc
}

func (p *profileOperations) begin(parent context.Context) (context.Context, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, nil, errors.New("Cooper is stopping")
	}
	if p.cancels == nil {
		p.cancels = make(map[int]context.CancelFunc)
	}
	ctx, cancel := context.WithCancel(parent)
	p.next++
	id := p.next
	p.cancels[id] = cancel
	p.wait.Add(1)
	return ctx, func() {
		cancel()
		p.mu.Lock()
		delete(p.cancels, id)
		p.mu.Unlock()
		p.wait.Done()
	}, nil
}

func (p *profileOperations) stop() {
	p.mu.Lock()
	p.closed = true
	for _, cancel := range p.cancels {
		cancel()
	}
	p.mu.Unlock()
	p.wait.Wait()
}
