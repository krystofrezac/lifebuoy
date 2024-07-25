package containermanager

import "context"

type AppImage struct {
}

type AppConfigurationRoute struct {
	port string
	url  string
}

type AppConfiguration struct {
	AppName      string
	ImageName    string
	ImageVersion string

	// TODO: goal state
	// Routes []AppConfigurationRoute
}

type App interface {
	Configuration() AppConfiguration
	// If false `Build` will be called
	IsBuilt(context.Context) bool
	// Be prepared that this function can be called multiple times
	Build(context.Context) error
}
