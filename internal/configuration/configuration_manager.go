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
	repositoryBuildAppCreator containermanager.RepositoryBuildAppCreator
	containerManager          containermanager.ContainerManager
	ticker                    *time.Ticker
	iterTimeout               time.Duration
	downloadDir               string
	appsConfigurationDir      string
	// nil = don't have apps yet
	lastApps          []containermanager.App
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
	repositoryBuildAppCreator containermanager.RepositoryBuildAppCreator,
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
		containerManager:          containerManager,
		ticker:                    ticker,
		iterTimeout:               iterTimeout,
		downloadDir:               downloadDir,
		appsConfigurationDir:      appsConfigurationDir,
		lastApps:                  nil,
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
	didAppsChange := c.didAppsChange(apps)
	c.lastApps = apps

	// TODO: Check unique names
	// TODO: Add default apps (treafik)

	if didAppsChange {
		c.logger.Info("Apps configuration changed")
		c.containerManager.UpdateApps(apps)
	}

	c.logger.Debug("Configuration check finished")
}

func (c *ConfigurationManager) readAppConfigurations(dir string) ([]containermanager.App, error) {
	validate := validator.New(validator.WithRequiredStructEnabled())

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var appConfigurations = make([]containermanager.App, 0, len(entries))
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

		app := c.repositoryBuildAppCreator.Create(containermanager.RepositoryBuildAppCreateOpts{
			AppName:            appName,
			ResourceName:       c.resourcePrefix + appName,
			RepositoryOwner:    decoded.Source.Github.Owner,
			RepositoryName:     decoded.Source.Github.Repository,
			RepositoryRevision: decoded.Source.Github.Revision,
		})

		appConfigurations = append(appConfigurations, app)
	}

	return appConfigurations, nil
}

func (c *ConfigurationManager) didAppsChange(newApps []containermanager.App) bool {
	if c.lastApps == nil {
		return true
	}

	if len(c.lastApps) != len(newApps) {
		return true
	}

	for i, lastItem := range c.lastApps {
		newItem := newApps[i]
		if lastItem != newItem {
			return true
		}
	}

	return false
}
