package apps

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/krystofrezac/lifebuoy/internal/docker"
	"github.com/krystofrezac/lifebuoy/internal/github"
)

// Creator
type RepositoryBuildAppCreator struct {
	logger             *slog.Logger
	dockerClient       *client.Client
	customDockerClient docker.Docker
	managedStoragePath string
	resourcePrefix     string
}

type RepositoryBuildAppCreateOpts struct {
	AppName            string
	RepositoryOwner    string
	RepositoryName     string
	RepositoryRevision string
}

func NewRepositoryBuilderAppCreator(
	logger *slog.Logger,
	dockerClient *client.Client,
	customDockerClient docker.Docker,
	managedStoragePath string,
	resourcePrefix string,
) RepositoryBuildAppCreator {
	return RepositoryBuildAppCreator{
		logger:             logger,
		customDockerClient: customDockerClient,
		dockerClient:       dockerClient,
		managedStoragePath: managedStoragePath,
		resourcePrefix:     resourcePrefix,
	}
}

func (r RepositoryBuildAppCreator) Create(opts RepositoryBuildAppCreateOpts) App {
	return repositoryBuildApp{
		RepositoryBuildAppCreator:    r,
		RepositoryBuildAppCreateOpts: opts,
	}
}

// App
type repositoryBuildApp struct {
	RepositoryBuildAppCreator
	RepositoryBuildAppCreateOpts
}

func (r repositoryBuildApp) IsBuilt(ctx context.Context) bool {
	imageReference := r.getImage()
	filters := filters.NewArgs(
		filters.KeyValuePair{
			Key:   "reference",
			Value: imageReference,
		},
		filters.KeyValuePair{
			Key:   "label",
			Value: managedLabel,
		},
	)

	images, err := r.dockerClient.ImageList(
		ctx,
		image.ListOptions{
			Filters: filters,
		},
	)
	if err != nil {
		r.logger.Error("Failed to list images", "err", err, "filters", filters)
		return false
	}

	return len(images) > 0
}

func (r repositoryBuildApp) Build(ctx context.Context) error {
	buildDir := path.Join(r.managedStoragePath, "/build")

	defer func() {
		removeErr := os.RemoveAll(buildDir)
		if removeErr != nil {
			r.logger.Error("Failed to remove build dir", "path", buildDir)
		}
	}()

	err := github.DownloadRepository(context.Background(), r.RepositoryOwner, r.RepositoryName, &r.RepositoryRevision, nil, buildDir)
	if err != nil {
		return err
	}

	r.logger.Info("Starting to build image")
	err = r.customDockerClient.BuildImage(
		r.getImage(),
		buildDir,
	)

	return err
}

func (r repositoryBuildApp) Configuration() AppConfiguration {
	return AppConfiguration{
		AppName: r.AppName,
		Image:   r.getImage(),
	}
}

func (r repositoryBuildApp) getImage() string {
	return fmt.Sprintf("%s%s:%s", r.resourcePrefix, r.AppName, r.RepositoryRevision)
}
