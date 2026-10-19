package main

import (
	"context"
	"fmt"
	_ "time/tzdata"

	"rnldo.dev/wnshell/internal/appconfig"
	"rnldo.dev/wnshell/internal/cli"
	"rnldo.dev/wnshell/internal/engine"
	"rnldo.dev/wnshell/internal/flows"
	"rnldo.dev/wnshell/internal/logger"
	"rnldo.dev/wnshell/internal/storage"
	"rnldo.dev/wnshell/internal/whtnt"
)

func main() {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Error(fmt.Sprintf("Recovered fatal panic: %v", recovered))
		}
	}()

	cfg, err := appconfig.LoadFresh()
	if err != nil {
		logger.Error(err.Error())
		return
	}

	if err := whtnt.ConfigureRuntime(whtnt.RuntimeOptions{
		ProxyFile: cfg.Engine.ProxyFile,
		Platform:  whtnt.PlatformIOS,
	}); err != nil {
		logger.Error(err.Error())
		return
	}

	store, err := storage.NewSQLiteStore(cfg.Engine.Storage)
	if err != nil {
		logger.Error(err.Error())
		return
	}
	defer store.Close()

	var flowAdapter engine.Flows = flows.NewWhatnot()
	if cfg.Engine.StubMode {
		flowAdapter = flows.NewStub()
	}

	app := engine.New(engine.Dependencies{
		Config: cfg,
		Store:  store,
		Flows:  flowAdapter,
	})

	if err := cli.Run(context.Background(), app, store); err != nil {
		logger.Error(err.Error())
	}
}
