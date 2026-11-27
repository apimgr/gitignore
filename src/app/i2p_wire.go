package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/apimgr/gitignore/src/config"
	"github.com/apimgr/gitignore/src/i2p"
	"github.com/apimgr/gitignore/src/server"
)

type i2pRuntime struct {
	mu      sync.RWMutex
	status  i2p.Status
	service *i2p.Service
	ctx     context.Context
	config  i2p.Config
	paths   i2p.Paths
	handler http.Handler
}

func newI2PRuntime(enabled bool) *i2pRuntime {
	state := i2p.StateDisabled
	if enabled {
		state = i2p.StateNoProvider
	}
	return &i2pRuntime{status: i2p.Status{State: state, Provider: i2p.ProviderNone}}
}

func (runtime *i2pRuntime) healthInfo() server.I2PInfo {
	runtime.mu.RLock()
	status := runtime.status
	service := runtime.service
	runtime.mu.RUnlock()
	if service != nil {
		status = service.Status()
	}
	info := server.I2PInfo{Provider: string(status.Provider), Hostname: status.Address, State: string(status.State)}
	switch status.State {
	case i2p.StateStarting, i2p.StateBuilding:
		info.Enabled = true
		info.Status = "starting"
	case i2p.StateRunning:
		info.Enabled = true
		info.Running = true
		info.Status = "healthy"
	case i2p.StateError:
		info.Enabled = true
		info.Status = "unavailable"
	default:
		info.Status = "disabled"
		info.Provider = string(i2p.ProviderNone)
		info.Hostname = ""
	}
	return info
}

func (runtime *i2pRuntime) run(ctx context.Context, settings config.I2PConfig, configDir, dataDir, logsDir string, handler http.Handler) {
	if !settings.Enabled {
		return
	}
	i2pConfig := i2p.Config{
		Enabled:          true,
		Binary:           settings.Binary,
		SAMAddress:       settings.SAMAddress,
		VirtualPort:      settings.VirtualPort,
		InboundLength:    settings.InboundLength,
		OutboundLength:   settings.OutboundLength,
		InboundQuantity:  settings.InboundQuantity,
		OutboundQuantity: settings.OutboundQuantity,
		SignatureType:    settings.SignatureType,
		BootstrapTimeout: time.Duration(settings.BootstrapTimeout) * time.Second,
	}
	paths := i2p.Paths{ConfigDir: configDir, DataDir: dataDir, LogDir: logsDir}
	runtime.mu.Lock()
	runtime.ctx = ctx
	runtime.config = i2pConfig
	runtime.paths = paths
	runtime.handler = handler
	runtime.mu.Unlock()
	provider, err := i2p.DetectProvider(i2pConfig)
	if err != nil {
		runtime.setStatus(i2p.Status{State: i2p.StateNoProvider, Provider: i2p.ProviderNone, Error: err.Error(), UpdatedAt: time.Now().UTC()})
		log.Printf("I2P eepsite unavailable: %v", err)
		return
	}
	runtime.setStatus(i2p.Status{State: i2p.StateStarting, Provider: provider, UpdatedAt: time.Now().UTC()})
	service, err := i2p.Start(ctx, i2pConfig, paths, handler)
	if err != nil {
		state := i2p.StateError
		if errors.Is(err, i2p.ErrDisabled) || errors.Is(err, i2p.ErrNoProvider) {
			state = i2p.StateNoProvider
		}
		runtime.setStatus(i2p.Status{State: state, Provider: provider, Error: err.Error(), UpdatedAt: time.Now().UTC()})
		log.Printf("I2P eepsite unavailable: %v", err)
		return
	}
	runtime.mu.Lock()
	runtime.service = service
	runtime.mu.Unlock()
	go service.Monitor(ctx)
}

func (runtime *i2pRuntime) healthCheck(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	runtime.mu.RLock()
	service := runtime.service
	parent := runtime.ctx
	settings := runtime.config
	paths := runtime.paths
	handler := runtime.handler
	provider := runtime.status.Provider
	runtime.mu.RUnlock()
	if service != nil {
		if err := service.Probe(ctx); err == nil {
			return nil
		} else {
			log.Printf("I2P health check failed; restarting: %v", err)
		}
	}
	if parent == nil || parent.Err() != nil {
		return errors.New("I2P runtime is not active")
	}
	if service != nil {
		_ = service.Close()
	}
	restarted, err := i2p.Start(parent, settings, paths, handler)
	if err != nil {
		runtime.setStatus(i2p.Status{State: i2p.StateError, Provider: provider, Error: err.Error(), UpdatedAt: time.Now().UTC()})
		return err
	}
	runtime.mu.Lock()
	runtime.service = restarted
	runtime.status = restarted.Status()
	runtime.mu.Unlock()
	go restarted.Monitor(parent)
	return nil
}

func (runtime *i2pRuntime) setStatus(status i2p.Status) {
	runtime.mu.Lock()
	runtime.status = status
	runtime.mu.Unlock()
}
