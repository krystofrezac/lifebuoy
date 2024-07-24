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

	managedStoragePath string
}

func loadFlags(logger *slog.Logger) flags {
	confRepositoryOwner := flag.String("confRepositoryOwner", "", "required: Owner of Github repository used for configuration")
	confRepositoryName := flag.String("confRepositoryName", "", "required: Name of Github repository used for configuration")
	confRepositoryRevision := flag.String("confRepositoryRevision", "", "Revision of the configuration repository. By default the default branch")
	githubToken := flag.String("githubToken", "", "Token used for fetching repositories from Github")

	managedStoragePath := flag.String("managedStoragePath", "tmp", "Path to a directory where Lifebuoy will store data")

	flag.Parse()

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
		managedStoragePath:     *managedStoragePath,
	}
}
