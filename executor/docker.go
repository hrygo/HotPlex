package executor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

type DockerConfig struct {
	Image       string
	WorkDir     string
	Timeout     time.Duration
	CPU_LIMIT   int64
	MemoryLimit int64
	Env         []string
}

type DockerExecutor struct {
	client *client.Client
	logger *slog.Logger
	mu     sync.RWMutex
}

func NewDockerExecutor(logger *slog.Logger) (*DockerExecutor, error) {
	if logger == nil {
		logger = slog.Default()
	}

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	return &DockerExecutor{
		client: cli,
		logger: logger,
	}, nil
}

func (e *DockerExecutor) Execute(ctx context.Context, cfg DockerConfig, cmd []string, stdin io.Reader, stdout io.Writer) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	_ = imagePull(ctx, e.client, cfg.Image)

	containerCfg := &container.Config{
		Image: cfg.Image,
		Cmd:   cmd,
		Env:   cfg.Env,
		Tty:   false,
	}

	hostCfg := &container.HostConfig{
		Binds: []string{fmt.Sprintf("%s:/workspace", cfg.WorkDir)},
		Resources: container.Resources{
			NanoCPUs: cfg.CPU_LIMIT,
			Memory:   cfg.MemoryLimit,
		},
	}

	resp, err := e.client.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, "")
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	defer func() {
		_ = e.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	}()

	if err := e.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	go func() {
		if stdin != nil {
			hijacked, err := e.client.ContainerAttach(ctx, resp.ID, container.AttachOptions{Stream: true, Stdin: true})
			if err == nil {
				_, _ = io.Copy(hijacked.Conn, stdin)
				_ = hijacked.CloseWrite()
			}
		}
	}()

	if stdout != nil {
		out, _ := e.client.ContainerLogs(ctx, resp.ID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
		})
		_, _ = io.Copy(stdout, out)
	}

	statusCh, errCh := e.client.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("container wait: %w", err)
		}
	case <-statusCh:
	}

	return nil
}

func (e *DockerExecutor) Close() error {
	return e.client.Close()
}

func imagePull(ctx context.Context, cli *client.Client, img string) error {
	reader, err := cli.ImagePull(ctx, img, image.PullOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	_, _ = io.Copy(io.Discard, reader)
	return nil
}
