package containermanager

import (
	"context"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/krystofrezac/lifebuoy/internal/apps"
	"github.com/krystofrezac/lifebuoy/internal/queues"
)

var tickInterval = 10 * time.Second

const managedLabel = "dev.lifebuoy.managed"
const appNameLabel = "dev.lifebuoy.app-name"

type ContainerManager struct {
	logger                    *slog.Logger
	dockerClient              *client.Client
	appsChangeChannel         chan []apps.App
	ticker                    *time.Ticker
	apps                      []apps.App
	receivedAppsConfiguration bool
	buildProcessor            *queues.UniqueJobProcessor
}

func NewContainerManager(logger *slog.Logger, dockerClient *client.Client) ContainerManager {
	appsChangeChannel := make(chan []apps.App)
	ticker := time.NewTicker(tickInterval)
	buildProcessor := queues.NewUniqueJobProcessor(1)

	return ContainerManager{
		logger:                    logger,
		dockerClient:              dockerClient,
		appsChangeChannel:         appsChangeChannel,
		ticker:                    ticker,
		apps:                      nil,
		receivedAppsConfiguration: false,
		buildProcessor:            buildProcessor,
	}
}

func (c ContainerManager) Start(ctx context.Context) {
	go c.buildProcessor.Start()

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
		}

		if !c.receivedAppsConfiguration {
			c.logger.Debug("Haven't received configuration yet, skipping reconcile")
			continue
		}

		c.logger.Debug("Container reconcile started")
		c.reconcile(ctx)
		c.logger.Debug("Container reconcile finished")
	}
}

func (c ContainerManager) UpdateApps(apps []apps.App) {
	c.appsChangeChannel <- apps
}

func (c ContainerManager) reconcile(ctx context.Context) {
	c.createContainers(ctx)
	c.startContainers(ctx)

	// TODO: stop and remove apps that don't exist anymore
	// TODO: remove unused images
}

func (c ContainerManager) createContainers(ctx context.Context) {
	for _, app := range c.apps {
		configuration := app.Configuration()
		containers, err := c.dockerClient.ContainerList(ctx, container.ListOptions{
			All:   true,
			Limit: 1,
			Filters: filters.NewArgs(
				filters.KeyValuePair{Key: "label", Value: managedLabel},
				filters.KeyValuePair{Key: "name", Value: configuration.ContainerName},
			),
		})
		if err != nil {
			c.logger.Error("Failed to list containers", "err", err)
			continue
		}
		if len(containers) != 0 {
			c.logger.Debug("Container already exists, skipping creation", "appName", configuration.AppName)
			continue
		}

		if !app.IsBuilt(ctx) {
			c.logger.Info("App build queued", "appName", configuration.AppName)
			c.buildProcessor.Process(configuration.AppName, func() error {
				return app.Build(ctx)
			})
			continue
		}

		c.logger.Info("Creating container", "appName", configuration.AppName)
		_, err = c.dockerClient.ContainerCreate(
			ctx,
			&container.Config{
				Image: configuration.Image,
				Labels: map[string]string{
					managedLabel: "true",
				},
			},
			nil,
			nil,
			nil,
			configuration.ContainerName,
		)
		if err != nil {
			c.logger.Error("Failed to create container", "appName", configuration.AppName, "err", err)
		}
	}
}

func (c ContainerManager) startContainers(ctx context.Context) {
	for _, app := range c.apps {
		configuration := app.Configuration()
		runningContainers, err := c.dockerClient.ContainerList(ctx, container.ListOptions{
			Limit: 1,
			Filters: filters.NewArgs(
				filters.KeyValuePair{Key: "label", Value: managedLabel},
				filters.KeyValuePair{Key: "name", Value: configuration.ContainerName},
				filters.KeyValuePair{Key: "status", Value: "running"},
			),
		})
		if err != nil {
			c.logger.Error("Failed to list containers", "err", err)
			return
		}
		if len(runningContainers) > 0 {
			c.logger.Debug("Container already running, skipping start", "appName", configuration.AppName)
		}

		createdContainers, err := c.dockerClient.ContainerList(ctx, container.ListOptions{
			All:   true,
			Limit: 1,
			Filters: filters.NewArgs(
				filters.KeyValuePair{Key: "label", Value: managedLabel},
				filters.KeyValuePair{Key: "ancestor", Value: configuration.Image},
			),
		})
		if err != nil {
			c.logger.Error("Failed to list containers", "err", err)
			return
		}
		if len(createdContainers) == 0 {
			c.logger.Debug("Container doesn't exist yet, skipping start", "appName", configuration.AppName)
			return
		}

		err = c.dockerClient.ContainerStart(ctx, configuration.ContainerName, container.StartOptions{})
		if err != nil {
			c.logger.Error("Failed to start container", "err", err, "appName", configuration.AppName)
			return
		}
	}
}
