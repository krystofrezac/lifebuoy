package main

import (
	"flag"
	"log/slog"
	"os"
)

type flags struct {
	confRepositoryOwner    string
	confRepositoryName     string
	confRepositoryRevision *string
	githubToken            *string
	logLevel               slog.Level
	managedStoragePath     string
	resourcePrefix         string
}

func loadFlags(logger *slog.Logger) flags {
	confRepositoryOwner := flag.String("confRepositoryOwner", "", "required: Owner of Github repository used for configuration")
	confRepositoryName := flag.String("confRepositoryName", "", "required: Name of Github repository used for configuration")
	confRepositoryRevision := flag.String("confRepositoryRevision", "", "Revision of the configuration repository. By default the default branch")
	githubToken := flag.String("githubToken", "", "Token used for fetching repositories from Github")

	logLevelRaw := flag.String("logLevel", "INFO", "")
	managedStoragePath := flag.String("managedStoragePath", "tmp", "Path to a directory where Lifebuoy will store data")
	resourcePrefix := flag.String("resourcePrefix", "dev.lifebuoy.", "Prefix for docker resources(names/labels for images/containers)")

	flag.Parse()

	logLevel := slog.LevelVar{}
	err := logLevel.UnmarshalText([]byte(*logLevelRaw))
	if err != nil {
		logger.Error("Failed to parse flag `logLevel`", "err", err)
		os.Exit(1)
	}

	// Checking required flags
	if *confRepositoryOwner == "" {
		logger.Error("Flag 'confRepositoryOwner' is required")
		os.Exit(1)
	}
	if *confRepositoryName == "" {
		logger.Error("Flag 'confRepositoryName' is required")
		os.Exit(1)
	}
	if *managedStoragePath == "" {
		logger.Error("Flag 'managedStoragePath' is required")
		os.Exit(1)
	}

	// Nulling flags that weren't passed
	if *confRepositoryRevision == "" {
		confRepositoryRevision = nil
	}
	if *githubToken == "" {
		githubToken = nil
	}

	return flags{
		confRepositoryOwner:    *confRepositoryOwner,
		confRepositoryName:     *confRepositoryName,
		confRepositoryRevision: confRepositoryRevision,
		githubToken:            githubToken,
		logLevel:               logLevel.Level(),
		managedStoragePath:     *managedStoragePath,
		resourcePrefix:         *resourcePrefix,
	}
}
