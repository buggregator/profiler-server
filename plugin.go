package profiler

import (
	"context"
	"net"
	"net/http"
	"sync"

	"github.com/roadrunner-server/endure/v2/dep"
	"github.com/roadrunner-server/errors"
	"go.uber.org/zap"
)

const (
	PluginName = "profiler"
)

// Logger interface for dependency injection
type Logger interface {
	NamedLogger(name string) *zap.Logger
}

// Configurer interface for configuration access
type Configurer interface {
	UnmarshalKey(name string, out any) error
	Has(name string) bool
}

// Plugin is the XHProf profiler server plugin.
// It receives XHProf profile data via HTTP, processes it entirely in Go
// (peaks, diffs, edges, percentages), and forwards the result to the Jobs pipeline.
type Plugin struct {
	mu  sync.RWMutex
	cfg *Config
	log *zap.Logger

	jobs     Jobs
	server   *http.Server
	listener net.Listener
}

// Init initializes the plugin with configuration and logger
func (p *Plugin) Init(log Logger, cfg Configurer) error {
	const op = errors.Op("profiler_plugin_init")

	if !cfg.Has(PluginName) {
		return errors.E(op, errors.Disabled)
	}

	err := cfg.UnmarshalKey(PluginName, &p.cfg)
	if err != nil {
		return errors.E(op, err)
	}

	if err := p.cfg.InitDefaults(); err != nil {
		return errors.E(op, err)
	}

	p.log = log.NamedLogger(PluginName)

	p.log.Info("Profiler plugin initialized",
		zap.String("addr", p.cfg.Addr),
		zap.String("jobs_pipeline", p.cfg.Jobs.Pipeline),
	)

	return nil
}

// Serve starts the HTTP server
func (p *Plugin) Serve() chan error {
	errCh := make(chan error, 2)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.jobs == nil {
		errCh <- errors.E(errors.Op("profiler_serve"), errors.Str("jobs plugin not available"))
		return errCh
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleHTTP)

	p.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  p.cfg.ReadTimeout,
		WriteTimeout: p.cfg.WriteTimeout,
	}

	var err error
	p.listener, err = net.Listen("tcp", p.cfg.Addr)
	if err != nil {
		errCh <- errors.E(errors.Op("profiler_listen"), err)
		return errCh
	}

	p.log.Info("Profiler HTTP server started",
		zap.String("addr", p.cfg.Addr),
		zap.String("jobs_pipeline", p.cfg.Jobs.Pipeline),
	)

	go func() {
		if err := p.server.Serve(p.listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	return errCh
}

// Stop gracefully stops the plugin
func (p *Plugin) Stop(ctx context.Context) error {
	p.log.Info("stopping Profiler plugin")

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.server != nil {
		return p.server.Shutdown(ctx)
	}

	return nil
}

// Name returns plugin name
func (p *Plugin) Name() string {
	return PluginName
}

// Collects declares dependencies on other plugins
func (p *Plugin) Collects() []*dep.In {
	return []*dep.In{
		dep.Fits(func(pp any) {
			p.jobs = pp.(Jobs)
			p.log.Debug("collected jobs plugin")
		}, (*Jobs)(nil)),
	}
}

// pushToJobs sends a processed profile event to the Jobs plugin
func (p *Plugin) pushToJobs(event *ProfileEvent) error {
	const op = errors.Op("profiler_push_to_jobs")

	if p.jobs == nil {
		return errors.E(op, errors.Str("jobs plugin not available"))
	}

	msg, err := profileToJobMessage(event, &p.cfg.Jobs)
	if err != nil {
		return errors.E(op, err)
	}

	if err := p.jobs.Push(context.Background(), msg); err != nil {
		return errors.E(op, err)
	}

	p.log.Debug("profile pushed to jobs",
		zap.String("uuid", event.UUID),
		zap.String("pipeline", p.cfg.Jobs.Pipeline),
	)

	return nil
}
