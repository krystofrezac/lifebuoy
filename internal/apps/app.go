package apps

import "context"

// TODO: duplicated in container_manager
const managedLabel = "dev.lifebuoy.managed"
const appNameLabel = "dev.lifebuoy.app-name"

type AppConfiguration struct {
	AppName       string
	ContainerName string
	Image         string
	Volumes       map[string]struct{}
}

type App interface {
	// If false `Build` will be called
	IsBuilt(context.Context) bool
	// Be prepared that this function can be called multiple times
	Build(context.Context) error
	Configuration() AppConfiguration
}
