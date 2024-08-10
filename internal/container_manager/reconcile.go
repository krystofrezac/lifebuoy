package containermanager

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/krystofrezac/lifebuoy/internal/apps"
	"github.com/krystofrezac/lifebuoy/internal/queues"
)

type reconcile struct {
	ctx            context.Context
	logger         *slog.Logger
	dockerClient   *client.Client
	buildProcessor *queues.UniqueJobProcessor
	resourcePrefix string
	apps           []apps.App
}

func runReconcile(
	ctx context.Context,
	logger *slog.Logger,
	dockerClient *client.Client,
	buildProcessor *queues.UniqueJobProcessor,
	reconcileFinishChannel chan<- struct{},
	resourcePrefix string,
	apps []apps.App,
) {
	logger.Debug("Container reconcile started")

	r := reconcile{
		ctx:            ctx,
		logger:         logger,
		dockerClient:   dockerClient,
		buildProcessor: buildProcessor,
		resourcePrefix: resourcePrefix,
		apps:           apps,
	}

	r.createContainers(ctx)
	r.startContainers(ctx)

	// TODO: stop and remove apps that don't exist anymore
	// TODO: remove unused images

	logger.Debug("Container reconcile finished")
	reconcileFinishChannel <- struct{}{}
}

func (r reconcile) createContainers(ctx context.Context) {
	for _, app := range r.apps {
		configuration := app.Configuration()
		containerName := r.getContainerName(configuration)

		containers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
			All:   true,
			Limit: 1,
			Filters: filters.NewArgs(
				getContainerFilters(
					containerName,
					configuration.Image,
					nil,
				)...,
			),
		})
		if err != nil {
			r.logger.Error("Failed to list containers", "err", err)
			continue
		}
		if len(containers) != 0 {
			r.logger.Debug("Container already exists, skipping creation", "appName", configuration.AppName)
			continue
		}

		if !app.IsBuilt(ctx) {
			r.logger.Info("App build queued", "appName", configuration.AppName)
			r.buildProcessor.Process(configuration.AppName, func() error {
				return app.Build(ctx)
			})
			continue
		}

		r.logger.Info("Creating container", "appName", configuration.AppName)
		_, err = r.dockerClient.ContainerCreate(
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
			containerName,
		)
		if err != nil {
			r.logger.Error("Failed to create container", "appName", configuration.AppName, "err", err)
		}
	}
}

func (r reconcile) startContainers(ctx context.Context) {
	for _, app := range r.apps {
		configuration := app.Configuration()
		containerName := r.getContainerName(configuration)

		runningContainers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
			Limit: 1,
			Filters: filters.NewArgs(
				getContainerFilters(
					containerName,
					configuration.Image,
					[]filters.KeyValuePair{
						{Key: "status", Value: "running"},
					},
				)...,
			),
		})
		if err != nil {
			r.logger.Error("Failed to list containers", "err", err)
			return
		}
		if len(runningContainers) > 0 {
			r.logger.Debug("Container already running, skipping start", "appName", configuration.AppName)
		}

		createdContainers, err := r.dockerClient.ContainerList(ctx, container.ListOptions{
			All:     true,
			Limit:   1,
			Filters: filters.NewArgs(getContainerFilters(containerName, configuration.Image, nil)...),
		})
		if err != nil {
			r.logger.Error("Failed to list containers", "err", err)
			return
		}
		if len(createdContainers) == 0 {
			r.logger.Debug("Container doesn't exist yet, skipping start", "appName", configuration.AppName)
			return
		}

		err = r.dockerClient.ContainerStart(ctx, containerName, container.StartOptions{})
		if err != nil {
			r.logger.Error("Failed to start container", "err", err, "appName", configuration.AppName)
			return
		}
	}
}

func (r reconcile) getContainerName(configuration apps.AppConfiguration) string {
	imageVersion := ""
	if split := strings.Split(configuration.Image, ":"); len(split) > 0 {
		imageVersion = split[1]
	}

	return fmt.Sprintf("%s%s_%s", r.resourcePrefix, configuration.AppName, imageVersion)
}

func getContainerFilters(containerName string, image string, additional []filters.KeyValuePair) []filters.KeyValuePair {
	res := []filters.KeyValuePair{
		{Key: "label", Value: managedLabel},
		{Key: "name", Value: containerName},
		{Key: "ancestor", Value: image},
	}
	res = append(res, additional...)
	return res
}
