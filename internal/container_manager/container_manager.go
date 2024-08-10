package containermanager

import (
	"context"
	"github.com/docker/docker/client"
	"github.com/krystofrezac/lifebuoy/internal/apps"
	"github.com/krystofrezac/lifebuoy/internal/queues"
	"log/slog"
	"time"
)

var tickInterval = 10 * time.Second

const managedLabel = "dev.lifebuoy.managed"
const appNameLabel = "dev.lifebuoy.app-name"

type ContainerManager struct {
	logger                    *slog.Logger
	dockerClient              *client.Client
	resourcePrefix            string
	appsChangeChannel         chan []apps.App
	reconcileFinishChannel    chan struct{}
	ticker                    *time.Ticker
	apps                      []apps.App
	receivedAppsConfiguration bool
	buildProcessor            *queues.UniqueJobProcessor
}

func NewContainerManager(logger *slog.Logger, dockerClient *client.Client, resourcePrefix string) ContainerManager {
	appsChangeChannel := make(chan []apps.App)
	reconcileFinishChannel := make(chan struct{})
	ticker := time.NewTicker(tickInterval)
	buildProcessor := queues.NewUniqueJobProcessor(1)

	return ContainerManager{
		logger:                    logger,
		dockerClient:              dockerClient,
		resourcePrefix:            resourcePrefix,
		appsChangeChannel:         appsChangeChannel,
		reconcileFinishChannel:    reconcileFinishChannel,
		ticker:                    ticker,
		apps:                      nil,
		receivedAppsConfiguration: false,
		buildProcessor:            buildProcessor,
	}
}

func (c ContainerManager) Start(ctx context.Context) {
	go c.buildProcessor.Start()

	reconcileIsRunning := false

	for {
		select {
		case newApps := <-c.appsChangeChannel:
			c.apps = newApps
			c.receivedAppsConfiguration = true
		case <-c.ticker.C:
		case event := <-c.buildProcessor.JobFinishedChannel:
			// TODO: retry
			if event.Result != nil {
				continue
			}
		case <-c.reconcileFinishChannel:
			reconcileIsRunning = false
			continue
		}

		if !c.receivedAppsConfiguration {
			c.logger.Debug("Haven't received configuration yet, skipping reconcile")
			continue
		}

		if reconcileIsRunning {
			c.logger.Debug("Reconcile is already running, skipping reconcile")
			continue
		}

		reconcileIsRunning = true
		go runReconcile(ctx, c.logger, c.dockerClient, c.buildProcessor, c.reconcileFinishChannel, c.resourcePrefix, c.apps)
	}
}

func (c ContainerManager) UpdateApps(apps []apps.App) {
	c.appsChangeChannel <- apps
}
