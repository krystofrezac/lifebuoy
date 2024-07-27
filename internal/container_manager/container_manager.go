package containermanager

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/krystofrezac/lifebuoy/internal/queues"
)

var tickInterval = 10 * time.Second
var reconcileTimeout = 30 * time.Second

const managedLabel = "dev.lifebuoy.managed"
const appNameLabel = "dev.lifebuoy.app-name"

type ContainerManager struct {
	logger            *slog.Logger
	dockerClient      *client.Client
	appsChangeChannel chan []App
	ticker            *time.Ticker
	// Nil = haven't received the configuratino yet
	apps           []App
	buildProcessor *queues.UniqueJobProcessor
}

func NewContainerManager(logger *slog.Logger, dockerClient *client.Client) ContainerManager {
	appsChangeChannel := make(chan []App)
	ticker := time.NewTicker(tickInterval)
	buildProcessor := queues.NewUniqueJobProcessor(1)

	return ContainerManager{
		logger:            logger,
		dockerClient:      dockerClient,
		appsChangeChannel: appsChangeChannel,
		ticker:            ticker,
		apps:              nil,
		buildProcessor:    buildProcessor,
	}
}

func (c ContainerManager) Start(ctx context.Context) {
	go c.buildProcessor.Start()

	for {
		// TODO: image build channel
		select {
		case newApps := <-c.appsChangeChannel:
			c.apps = newApps
		case <-c.ticker.C:
		case <-c.buildProcessor.JobFinishedChannel:
		}

		c.logger.Debug("Container reconcile started")
		c.reconcile(ctx)
		c.logger.Debug("Container reconcile finished")
	}
}

func (c ContainerManager) UpdateApps(apps []App) {
	c.appsChangeChannel <- apps
}

/*
TODO:
- start non existing containers
- restart stopped/exites containers - backoff
- delete containers for non existing apps
*/
func (c ContainerManager) reconcile(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	defer cancel()

	listFilters := filters.NewArgs(filters.KeyValuePair{Key: "label", Value: managedLabel + "=true"})

	containers, err := c.dockerClient.ContainerList(ctx, container.ListOptions{Filters: listFilters})
	if err != nil {
		c.logger.Error("Failed to list containers", "err", err)
		return
	}
	c.logger.Debug("Managed containers", "containers", containers)

	containersByNameLabel := groupContainersByNameLabel(containers)

	var appsToBeCreated []App
	for _, app := range c.apps {
		if _, ok := containersByNameLabel[app.Configuration().AppName]; !ok {
			appsToBeCreated = append(appsToBeCreated, app)
		}
	}
	c.logger.Debug("Apps to be created", "apps", appsToBeCreated)

	for _, app := range appsToBeCreated {
		if !app.IsBuilt(ctx) {
			c.buildProcessor.Process(app.Configuration().AppName, func() error {
				return app.Build(ctx)
			})
		} else {
			err := c.startApp(ctx, app)
			if err != nil {
				c.logger.Error("Failed to start app", "appName", app.Configuration().AppName, "imageVersion", app.Configuration().ImageVersion, "err", err)
			} else {
				c.logger.Info("App started", "appName", app.Configuration().AppName, "imageVersion", app.Configuration().ImageVersion)
			}
		}
	}
}

func (c ContainerManager) startApp(ctx context.Context, app App) error {
	appConfiguration := app.Configuration()
	image := fmt.Sprintf("%s:%s", appConfiguration.ImageName, appConfiguration.ImageVersion)

	_, err := c.dockerClient.ContainerCreate(
		ctx,
		&container.Config{
			Image: image,
			Labels: map[string]string{
				managedLabel: "true",
				appNameLabel: appConfiguration.AppName,
			},
		},
		nil,
		nil,
		nil,
		appConfiguration.ContainerName,
	)
	if err != nil {
		return err
	}

	err = c.dockerClient.ContainerStart(ctx, appConfiguration.ContainerName, container.StartOptions{})
	return err
}

func groupContainersByNameLabel(containers []types.Container) map[string][]types.Container {
	containersByName := make(map[string][]types.Container)
	for _, container := range containers {
		appName, ok := container.Labels[appNameLabel]
		if !ok {
			continue
		}

		containersByName[appName] = append(containersByName[appName], container)
	}

	return containersByName
}
