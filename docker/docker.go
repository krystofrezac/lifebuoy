package docker

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

const domain = "dev.lifebuoy"

var optsShaLabelName = domain + ".opts_sha"
var managedLabel = domain + ".managed=true"

var ContainerNotFoundError = errors.New("Container not found")

type Conf struct {
	Logger *slog.Logger
}

type DockerRunOpts struct {
	// In standard Docker format <from>:<to>
	VolumeBinds []string
	// In standard Docker format <from>:<to>
	PortMappings []string
	// In standard Docker format <name>=<value>
	Labels []string
}

type containerInfo struct {
	status  string
	imageId string
	optsSha string
}

func (conf Conf) BuildImage(name string, dir string) error {
	stdout, stderr, err := runCommand(
		"docker", "build", dir,
		"--tag", name,
		"--label", managedLabel,
	)
	if err != nil {
		conf.Logger.Error("Build failed", "stdout", stdout.String(), "stderr", stderr.String())
		return err
	}

	conf.Logger.Info("Build finished", "stdout", stdout.String(), "stderr", stderr.String())
	return nil
}

// Ensures that container is running with given configuration
func (conf Conf) UpsertContainer(name string, opts DockerRunOpts) error {
	containerInfo, err := conf.getContainerInfo(name)
	containerExists := !errors.Is(err, ContainerNotFoundError)
	if err != nil && containerExists {
		conf.Logger.Error("Failed to get container info", "err", err)
		return err
	}

	if containerInfo.status == "running" && containerExists {
		newOptsSha := getSha(opts)
		newImageId, err := conf.getImageId(name)
		if err != nil {
			return err
		}

		newContainerSettingsId := getContainerSettingsId(newImageId, newOptsSha)
		currentContainerSettingsId := getContainerSettingsId(containerInfo.imageId, containerInfo.optsSha)

		if newContainerSettingsId == currentContainerSettingsId {
			conf.Logger.Info("Nothing changed in container configuration, keeping it running", "currentState", currentContainerSettingsId, "newContainerState", newContainerSettingsId)
			return nil
		}
		conf.Logger.Info("Something changed in container configuration, re-running it", "currentState", currentContainerSettingsId, "newContainerState", newContainerSettingsId)

		err = StopContainer(name)
		if err != nil {
			conf.Logger.Error("Failed to stop container", "err", err)
			return err
		}

		err = RemoveContainer(name)
		if err != nil {
			conf.Logger.Error("Failed to remove container", "err", err)
			return err
		}
	}

	if containerExists {
		conf.Logger.Info("Removing stopped container")

		err = RemoveContainer(name)
		if err != nil {
			conf.Logger.Error("Failed to remove container", "err", err)
			return err
		}
	}

	return conf.RunContainer(name, opts)
}

// name: name of the image and name of the container
func (conf Conf) RunContainer(name string, opts DockerRunOpts) error {
	var optsVolumeBindsArgs = make([]string, 0, len(opts.VolumeBinds)*2)
	for _, volumeBind := range opts.VolumeBinds {
		optsVolumeBindsArgs = append(optsVolumeBindsArgs, "--volume", volumeBind)
	}

	var optsPortMappingsArgs = make([]string, 0, len(opts.PortMappings)*2)
	for _, portMapping := range opts.PortMappings {
		optsPortMappingsArgs = append(optsPortMappingsArgs, "-p", portMapping)
	}

	var labelsArgs = make([]string, 0, len(opts.Labels)*2)
	for _, label := range opts.Labels {
		labelsArgs = append(labelsArgs, "--label", label)
	}

	optsSha := getSha(opts)

	var args = []string{
		"run",
		"--label", managedLabel,
		"--detach",
	}
	args = append(args, optsVolumeBindsArgs...)
	args = append(args, optsPortMappingsArgs...)
	args = append(args, labelsArgs...)
	args = append(args, "--name", name)
	args = append(args, "--label", optsShaLabelName+"="+optsSha)
	args = append(args, name)

	stdout, stderr, err := runCommand(
		"docker", args...,
	)
	if err != nil {
		conf.Logger.Error("Run failed", "stdout", stdout.String(), "stderr", stderr.String())
		return err
	}

	conf.Logger.Info("Run finished", "stdout", stdout.String(), "stderr", stderr.String())
	return nil
}

func StopContainer(name string) error {
	stdout, sterr, err := runCommand("docker", "container", "stop", name)
	if err != nil {
		return fmt.Errorf("Failed to stop container\nstdout=%s stderr=%s", stdout.String(), sterr.String())
	}
	return nil
}

func RemoveContainer(name string) error {
	stdout, sterr, err := runCommand("docker", "container", "rm", name)
	if err != nil {
		return fmt.Errorf("Failed to remove container\nstdout=%s stderr=%s", stdout.String(), sterr.String())
	}
	return nil
}

// TODO: context
func runCommand(name string, arg ...string) (bytes.Buffer, bytes.Buffer, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.Command(name, arg...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	if err == nil && cmd.ProcessState.ExitCode() != 0 {
		err = errors.New("Process returned non-zero exit code")
	}

	return stdout, stderr, err
}

func (conf Conf) getContainerStatus(name string) (string, error) {
	stdout, stderr, err := runCommand(
		"docker", "container", "inspect", name,
		"--format", `{{.State.Status}}`,
	)
	conf.Logger.Debug("container status result", "stdout", stdout.String(), "stderr", stderr.String(), "err", err)

	if err != nil {
		return "", err
	}

	trimmed := strings.Trim(stdout.String(), "\n")
	return trimmed, nil
}

func (conf Conf) getContainerInfo(name string) (containerInfo, error) {
	stdout, stderr, err := runCommand(
		"docker", "container", "inspect", name,
		"--format", `{{.State.Status}}-{{index .Image}}-{{index .Config.Labels "`+optsShaLabelName+`"}}`,
	)
	conf.Logger.Debug("container info result", "stdout", stdout.String(), "stderr", stderr.String(), "err", err)

	if err != nil {
		if err.Error() == "exit status 1" {
			return containerInfo{}, ContainerNotFoundError
		}
		return containerInfo{}, err
	}

	split := strings.Split(stdout.String(), "-")
	if len(split) != 3 {
		return containerInfo{}, errors.New("Container info didn't contain exactly 3 information)")
	}

	status := strings.Trim(split[0], "\n")
	imageId := strings.Trim(split[1], "\n")
	optsSha := strings.Trim(split[2], "\n")

	return containerInfo{status: status, imageId: imageId, optsSha: optsSha}, nil
}

func (conf Conf) getImageId(name string) (string, error) {
	stdout, stderr, err := runCommand(
		"docker", "image", "inspect", name,
		"--format", "{{.Id}}",
	)

	if err != nil {
		conf.Logger.Error("Failed to get image id", "stdout", stdout, "stderr", stderr, "err", err)
	}

	return strings.Trim(stdout.String(), "\n"), nil
}

func getContainerSettingsId(imageId string, optsSha string) string {
	return imageId + "-" + optsSha
}

func getSha(value any) string {
	optsShaRaw := sha256.Sum256([]byte(fmt.Sprintf("%v", value)))
	return fmt.Sprintf("%x", optsShaRaw)
}
