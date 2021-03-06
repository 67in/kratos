package kratos

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/registry"
	"github.com/go-kratos/kratos/v2/transport"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

// App is an application components lifecycle manager
type App struct {
	opts     options
	ctx      context.Context
	cancel   func()
	instance *registry.ServiceInstance
	log      *log.Helper
}

// New create an application lifecycle manager.
func New(opts ...Option) *App {
	options := options{
		ctx:    context.Background(),
		logger: log.DefaultLogger,
		sigs:   []os.Signal{syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT},
	}
	if id, err := uuid.NewUUID(); err == nil {
		options.id = id.String()
	}
	for _, o := range opts {
		o(&options)
	}
	ctx, cancel := context.WithCancel(options.ctx)
	return &App{
		opts:     options,
		ctx:      ctx,
		cancel:   cancel,
		instance: serviceInstance(options),
		log:      log.NewHelper("app", options.logger),
	}
}

// Logger returns logger.
func (a *App) Logger() log.Logger {
	return a.opts.logger
}

// Server returns transport servers.
func (a *App) Server() []transport.Server {
	return a.opts.servers
}

// Registry returns registry.
func (a *App) Registry() registry.Registry {
	return a.opts.registry
}

// Run executes all OnStart hooks registered with the application's Lifecycle.
func (a *App) Run() error {
	a.log.Infow(
		"service_id", a.opts.id,
		"service_name", a.opts.name,
		"version", a.opts.version,
	)
	g, ctx := errgroup.WithContext(a.ctx)
	for _, srv := range a.opts.servers {
		srv := srv
		g.Go(func() error {
			<-ctx.Done() // wait for stop signal
			return srv.Stop()
		})
		g.Go(func() error {
			return srv.Start()
		})
	}
	if a.opts.registry != nil {
		if err := a.opts.registry.Register(a.instance); err != nil {
			return err
		}
	}
	c := make(chan os.Signal, 1)
	signal.Notify(c, a.opts.sigs...)
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-c:
				a.Stop()
			}
		}
	})
	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// Stop gracefully stops the application.
func (a *App) Stop() error {
	if a.opts.registry != nil {
		if err := a.opts.registry.Deregister(a.instance); err != nil {
			return err
		}
	}
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

func serviceInstance(o options) *registry.ServiceInstance {
	if len(o.endpoints) == 0 {
		for _, srv := range o.servers {
			if e, err := srv.Endpoint(); err == nil {
				o.endpoints = append(o.endpoints, e)
			}
		}
	}
	return &registry.ServiceInstance{
		ID:        o.id,
		Name:      o.name,
		Version:   o.version,
		Metadata:  o.metadata,
		Endpoints: o.endpoints,
	}
}
