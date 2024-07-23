package main

import (
	"log/slog"
	"os"

	"github.com/krystofrezac/lifebuoy/docker"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	dockerConf := docker.Conf{
		Logger: logger,
	}

	err := dockerConf.BuildImage("dev.lifebuoy.internal.traefik", "assets/traefik")
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

	err = dockerConf.BuildImage("dev.lifebuoy.rylis", "/Users/krystof/workspace/krystofrezac/rylis")
	if err != nil {
		os.Exit(1)
	}
	err = dockerConf.UpsertContainer("dev.lifebuoy.rylis", docker.DockerRunOpts{
		Labels: []string{
			"traefik.http.routers.rylis.rule=Host(`rylis.localhost`)",
			"traefik.http.services.myservice.loadbalancer.server.port=8000",
		},
	})
	if err != nil {
		os.Exit(1)
	}
}
