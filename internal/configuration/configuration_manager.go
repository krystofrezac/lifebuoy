package configuration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/krystofrezac/lifebuoy/internal/apps"
	containermanager "github.com/krystofrezac/lifebuoy/internal/container_manager"
	"github.com/krystofrezac/lifebuoy/internal/github"
	"gopkg.in/yaml.v3"
)

type ConfigurationManager struct {
	logger                    *slog.Logger
	repositoryOwner           string
	repositoryName            string
	repositoryRevision        *string
	githubToken               *string
	managedStoragePath        string
	resourcePrefix            string
	repositoryBuildAppCreator apps.RepositoryBuildAppCreator
	dockefileAppCreator       apps.DockerFileAppCreator
	containerManager          containermanager.ContainerManager
	ticker                    *time.Ticker
	iterTimeout               time.Duration
	downloadDir               string
	appsConfigurationDir      string
	// nil = don't have apps yet
	apps              []apps.App
	lastRepositorySha string
}

type appConfiguration struct {
	Version int `validate:"required,oneof=1"`
	Source  struct {
		Github struct {
			Owner      string `validate:"required"`
			Repository string `valiadte:"required"`
			Revision   string `validate:"required"`
		}
	}
}

func NewConfigurationManager(
	logger *slog.Logger,
	repositoryOwner string,
	repositoryName string,
	repositoryRevision *string,
	githubToken *string,
	managedStoragePath string,
	resourcePrefix string,
	repositoryBuildAppCreator apps.RepositoryBuildAppCreator,
	dockefileAppCreator apps.DockerFileAppCreator,
	containerManager containermanager.ContainerManager,
) *ConfigurationManager {
	// TODO: make it configurable, beware the rate limit
	const tickInterval = 60 * time.Second
	const iterTimeout = 10 * time.Second
	const downloadDir = "configuration"
	const appsConfigurationDir = "apps"

	ticker := time.NewTicker(tickInterval)

	return &ConfigurationManager{
		logger:                    logger,
		repositoryOwner:           repositoryOwner,
		repositoryName:            repositoryName,
		repositoryRevision:        repositoryRevision,
		githubToken:               githubToken,
		managedStoragePath:        managedStoragePath,
		resourcePrefix:            resourcePrefix,
		repositoryBuildAppCreator: repositoryBuildAppCreator,
		dockefileAppCreator:       dockefileAppCreator,
		containerManager:          containerManager,
		ticker:                    ticker,
		iterTimeout:               iterTimeout,
		downloadDir:               downloadDir,
		appsConfigurationDir:      appsConfigurationDir,
		apps:                      nil,
		lastRepositorySha:         "",
	}
}

func (c *ConfigurationManager) Start(ctx context.Context) {
	c.checkForChanges(ctx)
	for range c.ticker.C {
		c.checkForChanges(ctx)
	}
}

func (c *ConfigurationManager) checkForChanges(ctx context.Context) {
	c.logger.Debug("Configuration check started")
	ctx, cancel := context.WithTimeout(ctx, c.iterTimeout)
	defer cancel()

	configPath := path.Join(c.managedStoragePath, c.downloadDir)

	revisionSha, err := github.GetSha(ctx, c.repositoryOwner, c.repositoryName, c.repositoryRevision, c.githubToken)
	if err != nil {
		c.logger.Error("Failed to get revision sha", "err", err)
		return
	}

	if c.lastRepositorySha == revisionSha {
		c.logger.Debug("Configuration sha haven't changed")
		return
	}
	c.lastRepositorySha = revisionSha

	err = github.DownloadRepository(
		ctx,
		c.repositoryOwner,
		c.repositoryName,
		c.repositoryRevision,
		c.githubToken,
		configPath,
	)
	if err != nil {
		c.logger.Error("Failed to download config repository", "err", err)
		return
	}

	apps, err := c.readAppConfigurations(path.Join(configPath, c.appsConfigurationDir))
	if err != nil {
		c.logger.Error("Failed to read app configurations", "err", err)
		return
	}
	apps = append(apps, c.getDefaultApps()...)

	didAppsChange := c.didAppsChange(apps)
	c.apps = apps

	err = c.checkAppsNameCollisions()
	if err != nil {
		c.logger.Error(err.Error())
		return
	}

	if didAppsChange {
		c.logger.Info("Apps configuration changed")
		c.containerManager.UpdateApps(apps)
	}

	c.logger.Debug("Configuration check finished")
}

func (c *ConfigurationManager) readAppConfigurations(dir string) ([]apps.App, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var appConfigurations = make([]apps.App, 0, len(entries))
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

		appName := strings.Split(fileName, ".")[0]
		decoded := appConfiguration{}

		err = yaml.NewDecoder(file).Decode(&decoded)
		if err != nil {
			return nil, fmt.Errorf("Failed to decode configuration file `%s`. Error: %s", filePath, err.Error())
		}

		err = validate.Struct(decoded)
		if err != nil {
			return nil, err
		}

		app := c.repositoryBuildAppCreator.Create(apps.RepositoryBuildAppCreateOpts{
			AppName:            appName,
			RepositoryOwner:    decoded.Source.Github.Owner,
			RepositoryName:     decoded.Source.Github.Repository,
			RepositoryRevision: decoded.Source.Github.Revision,
		})

		appConfigurations = append(appConfigurations, app)
	}

	return appConfigurations, nil
}

func (c *ConfigurationManager) getDefaultApps() []apps.App {
	return []apps.App{
		c.dockefileAppCreator.Create(apps.DockefileAppCreateOpts{
			AppName: "internal.traefik",
			Dockerfile: `
				FROM traefik:v3.1.0
				RUN mkdir /etc/traefik
				RUN echo "providers: {'docker': {}}" > /etc/traefik/traefik.yml
				`,
		}),
	}
}

func (c *ConfigurationManager) didAppsChange(newApps []apps.App) bool {
	if c.apps == nil {
		return true
	}

	if len(c.apps) != len(newApps) {
		return true
	}

	for i, lastItem := range c.apps {
		newItem := newApps[i]
		if lastItem != newItem {
			return true
		}
	}

	return false
}

func (c *ConfigurationManager) checkAppsNameCollisions() error {
	grouped := make(map[string]int, len(c.apps))
	for _, app := range c.apps {
		name := app.Configuration().AppName
		prev, prevOk := grouped[name]
		if prevOk {
			grouped[name] = prev + 1
		} else {
			grouped[name] = 0
		}
	}

	var collisions []string
	for name, count := range grouped {
		if count > 0 {
			collisions = append(collisions, name)
		}
	}

	if len(collisions) == 0 {
		return nil
	}

	return fmt.Errorf("There are multiple apps with the same name. Duplicate names %+v", collisions)
}
