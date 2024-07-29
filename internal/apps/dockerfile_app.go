package apps

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// Creator
type DockerFileAppCreator struct {
	logger       *slog.Logger
	dockerClient *client.Client
}

type DockefileAppCreateOpts struct {
	AppName      string
	ResourceName string
	Dockerfile   string
}

func NewDockefileAppCreator(
	logger *slog.Logger,
	dockerClient *client.Client,
) DockerFileAppCreator {
	return DockerFileAppCreator{
		logger:       logger,
		dockerClient: dockerClient,
	}
}

func (d DockerFileAppCreator) Create(opts DockefileAppCreateOpts) App {
	return DockeFileApp{DockerFileAppCreator: d, DockefileAppCreateOpts: opts}
}

// App
type DockeFileApp struct {
	DockerFileAppCreator
	DockefileAppCreateOpts
}

func (d DockeFileApp) IsBuilt(ctx context.Context) bool {
	imageReference := d.getImage()
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

	images, err := d.dockerClient.ImageList(
		ctx,
		image.ListOptions{
			Filters: filters,
		},
	)
	if err != nil {
		d.logger.Error("Failed to list images", "err", err, "filters", filters)
		return false
	}

	return len(images) > 0
}

// TODO: This should not log, only return errors
func (d DockeFileApp) Build(ctx context.Context) error {
	d.logger.Info("Starting to build image", "appName", d.AppName)

	var buf bytes.Buffer
	buildContext := tar.NewWriter(&buf)

	err := buildContext.WriteHeader(&tar.Header{
		Name: "Dockerfile",
		Size: int64(len(d.Dockerfile)),
		Mode: 0600,
	})
	if err != nil {
		d.logger.Error("Failed to write tar header", "err", err)
		return err
	}

	_, err = buildContext.Write([]byte(d.Dockerfile))
	if err != nil {
		d.logger.Error("Failed to write Dockefile to tar ", "err", err)
		return err
	}

	err = buildContext.Close()
	if err != nil {
		d.logger.Error("Failed to write close tar ", "err", err)
		return err
	}

	// TODO: context
	res, err := d.dockerClient.ImageBuild(ctx, &buf, types.ImageBuildOptions{
		Tags: []string{d.getImage()},
		Labels: map[string]string{
			managedLabel: "true",
			appNameLabel: d.AppName,
		},
	})
	if err != nil {
		d.logger.Error("Build failed", "err", err)
		return err
	}
	defer res.Body.Close()

	fullBody, err := io.ReadAll(res.Body)
	if err != nil {
		d.logger.Error("Failed to read body", "err", err)
		return err
	}
	d.logger.Info("Build finished", "output", string(fullBody))
	return nil
}

func (d DockeFileApp) Configuration() AppConfiguration {
	return AppConfiguration{
		AppName:       d.AppName,
		ContainerName: d.ResourceName,
		Image:         d.getImage(),
		// TODO: volumes
	}
}

func (d DockeFileApp) getImage() string {
	sha := fmt.Sprintf("%x", sha256.Sum256([]byte(d.Dockerfile)))
	return fmt.Sprintf("%s:%s", d.ResourceName, sha)
}
