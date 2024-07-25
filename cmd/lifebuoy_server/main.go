package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"

	"github.com/docker/docker/client"
	"github.com/go-playground/validator/v10"
	containermanager "github.com/krystofrezac/lifebuoy/internal/container_manager"
	"github.com/krystofrezac/lifebuoy/internal/docker"
	"github.com/krystofrezac/lifebuoy/internal/github"
	"gopkg.in/yaml.v3"
)

func main() {
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	flags := loadFlags(logger)

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
	)
	repositoryBuildAppCreator := containermanager.NewRepositoryBuilderAppCreator(logger, dockerClient, dockerConf, flags.managedStoragePath)

	go containerManagerInstance.Start(ctx)

	containerManagerInstance.UpdateApps([]containermanager.App{
		repositoryBuildAppCreator.Create(containermanager.RepositoryBuildAppCreateOpts{
			// TODO: domain should be in manager
			Name:               "lifebuoy.dev.rylis",
			RepositoryOwner:    "krystofrezac",
			RepositoryName:     "rylis",
			RepositoryRevision: "main",
		}),
	})

	select {}

	err = dockerConf.BuildImage("dev.lifebuoy.internal.traefik", "assets/traefik")
	if err != nil {
		os.Exit(1)
	}
	err = dockerConf.UpsertContainer("dev.lifebuoy.internal.traefik", docker.DockerRunOpts{
		VolumeBinds:  []string{"/var/run/docker.sock:/var/run/docker.sock"},
		PortMappings: []string{"80:80", "443:443"},
	})
	if err != nil {
		os.Exit(1)
	}

	configurationDir := path.Join(flags.managedStoragePath, "configuration")
	for i := 0; i == 0; i++ {
		err = github.DownloadRepository(
			context.Background(),
			flags.confRepositoryOwner,
			flags.confRepositoryName,
			flags.confRepositoryRevision,
			flags.githubToken,
			configurationDir,
		)
		if err != nil {
			logger.Error("Failed to download configuration repository", "err", err)
			// TODO: If this isn't the first time than it's okay and continue
			continue
		}

		servicesConfigurations, err := readServicesConfigurations(path.Join(configurationDir, "services"))
		if err != nil {
			logger.Error("Failed read services configurations", "err", err)
			continue
		}

		for _, serviceConfiguration := range servicesConfigurations {
			repoName := path.Join(flags.managedStoragePath, "service-repositories", serviceConfiguration.name)
			err = github.DownloadRepository(context.Background(),
				serviceConfiguration.Source.Github.Owner,
				serviceConfiguration.Source.Github.Repository,
				&serviceConfiguration.Source.Github.Revision,
				flags.githubToken,
				repoName,
			)
			if err != nil {
				logger.Error("Failed to download repository", "err", err)
				// TODO: is it okay?
				continue
			}

			dockerName := "dev.lifebuoy." + serviceConfiguration.name
			err = dockerConf.BuildImage(dockerName, repoName)
			if err != nil {
				logger.Error("Failed to build image", "err", err)
				// TODO:
				continue
			}

			err = dockerConf.UpsertContainer(dockerName, docker.DockerRunOpts{
				Labels: []string{
					// TODO:
					"traefik.http.routers.rylis.rule=Host(`rylis.localhost`)",
					"traefik.http.services.myservice.loadbalancer.server.port=8000",
				},
			})
			if err != nil {
				logger.Error("Failed to upsert container", "err", err)
				continue
			}
		}

		logger.Info("configurations", "conf", servicesConfigurations)
	}
}

type serviceConfiguration struct {
	name    string `yaml:"-"`
	Version int    `validate:"required,oneof=1"`
	Source  struct {
		Github struct {
			Owner      string `validate:"required"`
			Repository string `valiadte:"required"`
			Revision   string `validate:"required"`
		}
	}
}

func readServicesConfigurations(dir string) ([]serviceConfiguration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	validate := validator.New(validator.WithRequiredStructEnabled())

	var servicesConfigurations = make([]serviceConfiguration, 0, len(entries))

	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}

		fileName := entry.Name()
		filePath := path.Join(dir, fileName)
		file, err := os.Open(filePath)
		if err != nil {
			return nil, err
		}

		nameWithoutExtension := strings.Split(fileName, ".")[0]
		decoded := serviceConfiguration{name: nameWithoutExtension}

		err = yaml.NewDecoder(file).Decode(&decoded)
		if err != nil {
			return nil, fmt.Errorf("Failed to decode configuration file `%s`. Error: %s", filePath, err.Error())
		}

		err = validate.Struct(decoded)
		if err != nil {
			return nil, err
		}

		servicesConfigurations = append(servicesConfigurations, decoded)
	}

	return servicesConfigurations, nil
}
