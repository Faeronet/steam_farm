package sandbox

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

type DockerClient struct {
	cli *client.Client
}

type ContainerConfig struct {
	Name      string
	GameType  string
	MachineID string
	Hostname  string
	VNCPort   int
	Display   string
	Username  string
	Password  string
}

func NewDockerClient() (*DockerClient, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client init: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = cli.Ping(ctx)
	if err != nil {
		return nil, fmt.Errorf("docker ping: %w", err)
	}

	return &DockerClient{cli: cli}, nil
}

func (d *DockerClient) CreateAndStart(ctx context.Context, cfg ContainerConfig) (string, error) {
	image := fmt.Sprintf("sfarm-%s:latest", cfg.GameType)

	containerCfg := &container.Config{
		Image:    image,
		Hostname: cfg.Hostname,
		Env: []string{
			fmt.Sprintf("DISPLAY=%s", cfg.Display),
			fmt.Sprintf("MACHINE_ID=%s", cfg.MachineID),
			fmt.Sprintf("STEAM_USER=%s", cfg.Username),
			fmt.Sprintf("STEAM_PASS=%s", cfg.Password),
		},
		ExposedPorts: nat.PortSet{
			nat.Port(fmt.Sprintf("%d/tcp", cfg.VNCPort)): struct{}{},
		},
	}

	hostCfg := &container.HostConfig{
		PortBindings: nat.PortMap{
			nat.Port(fmt.Sprintf("%d/tcp", 5900)): []nat.PortBinding{
				{HostPort: fmt.Sprintf("%d", cfg.VNCPort)},
			},
		},
		Resources: container.Resources{
			NanoCPUs: 1_000_000_000,       // 1 CPU core
			Memory:   2560 * 1024 * 1024,  // 2.5 GB
			Devices: []container.DeviceMapping{
				{PathOnHost: "/dev/dri", PathInContainer: "/dev/dri", CgroupPermissions: "rwm"},
			},
		},
	}

	networkCfg := &network.NetworkingConfig{}

	resp, err := d.cli.ContainerCreate(ctx, containerCfg, hostCfg, networkCfg, nil, cfg.Name)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}

	if err := d.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = d.cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return "", fmt.Errorf("start container: %w", err)
	}

	return resp.ID, nil
}

func (d *DockerClient) Stop(ctx context.Context, containerID string) error {
	timeout := 10
	return d.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

func (d *DockerClient) Remove(ctx context.Context, containerID string) error {
	return d.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

func (d *DockerClient) Stats(ctx context.Context, containerID string) (*ContainerStats, error) {
	resp, err := d.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, err
	}

	return &ContainerStats{
		Running: resp.State.Running,
		Status:  resp.State.Status,
	}, nil
}

type ContainerStats struct {
	Running    bool
	Status     string
	CPUPercent float64
	MemoryMB   int
}
