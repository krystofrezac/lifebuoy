package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/docker/docker/client"
	"github.com/krystofrezac/lifebuoy/internal/apps"
	"github.com/krystofrezac/lifebuoy/internal/configuration"
	"github.com/krystofrezac/lifebuoy/internal/container_manager"
	"github.com/krystofrezac/lifebuoy/internal/docker"
)

func main() {
	ctx := context.Background()

	logLevel := new(slog.LevelVar)
	logger := slog.New(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}),
	)

	flags := loadFlags(logger)
	logLevel.Set(flags.logLevel)

	dockerClient, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logger.Error("Failed to initialize docker client", "err", err)
		os.Exit(1)
	}

	dockerConf := docker.Docker{
		Logger: logger,
	}
	containerManagerInstance := containermanager.NewContainerManager(
		logger,
		dockerClient,
		flags.resourcePrefix,
	)
	repositoryBuildAppCreator := apps.NewRepositoryBuilderAppCreator(
		logger,
		dockerClient,
		dockerConf,
		flags.managedStoragePath,
		flags.resourcePrefix,
	)
	dockefileAppCreator := apps.NewDockefileAppCreator(logger, dockerClient)
	configurationManager := configuration.NewConfigurationManager(
		logger,
		flags.confRepositoryOwner,
		flags.confRepositoryName,
		flags.confRepositoryRevision,
		flags.githubToken,
		flags.managedStoragePath,
		flags.resourcePrefix,
		repositoryBuildAppCreator,
		dockefileAppCreator,
		containerManagerInstance,
	)

	go containerManagerInstance.Start(ctx)
	go configurationManager.Start(ctx)

	select {}
}
