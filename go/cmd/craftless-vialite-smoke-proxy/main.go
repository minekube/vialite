package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	vialite "go.minekube.com/vialite"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "craftless-vialite-smoke-proxy: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	backendPort := os.Getenv("CRAFTLESS_SMOKE_SERVER_PORT")
	if backendPort == "" {
		return errors.New("CRAFTLESS_SMOKE_SERVER_PORT is required")
	}
	backendHost := envDefault("VIALITE_SMOKE_BACKEND_HOST", "127.0.0.1")
	backendAddress := net.JoinHostPort(backendHost, backendPort)

	mode, err := smokeMode(os.Getenv("VIALITE_SMOKE_MODE"))
	if err != nil {
		return err
	}
	clientCommand, err := clientCommand()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, err := vialite.New(vialite.Options{
		Mode:         mode,
		GateProtocol: envDefault("VIALITE_SMOKE_GATE_PROTOCOL", "auto"),
		LibraryPath:  os.Getenv("VIALITE_SMOKE_LIBRARY_PATH"),
		BinaryPath:   os.Getenv("VIALITE_SMOKE_BINARY_PATH"),
		Version:      envDefault("VIALITE_SMOKE_VERSION", "latest"),
		Backends: []vialite.Backend{
			{
				Name:       "craftless",
				Address:    backendAddress,
				Version:    envDefault("VIALITE_SMOKE_BACKEND_VERSION", "auto"),
				Forwarding: vialite.ForwardingNone,
			},
		},
	})
	if err != nil {
		return err
	}

	runErr := make(chan error, 1)
	go func() { runErr <- server.Start(ctx) }()
	readyCtx, readyCancel := context.WithTimeout(ctx, envDuration("VIALITE_SMOKE_READY_TIMEOUT", 90*time.Second))
	defer readyCancel()
	if err := server.WaitReady(readyCtx); err != nil {
		cancel()
		return fmt.Errorf("wait for vialite: %w", err)
	}
	dialAddress, err := server.BackendDialAddress("craftless")
	if err != nil {
		cancel()
		return fmt.Errorf("resolve vialite backend address: %w", err)
	}
	host, port, err := net.SplitHostPort(dialAddress)
	if err != nil {
		cancel()
		return fmt.Errorf("parse vialite dial address %q: %w", dialAddress, err)
	}
	fmt.Fprintf(os.Stderr, "vialite %s smoke proxy listening at %s for backend %s\n", modeName(mode), dialAddress, backendAddress)

	cmd := exec.CommandContext(ctx, clientCommand[0], clientCommand[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	cmd.Env = appendEnv(cmd.Env, "CRAFTLESS_SMOKE_SERVER_HOST", host)
	cmd.Env = appendEnv(cmd.Env, "CRAFTLESS_SMOKE_SERVER_PORT", port)
	if err := cmd.Run(); err != nil {
		cancel()
		return fmt.Errorf("craftless client command: %w", err)
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), envDuration("VIALITE_SMOKE_STOP_TIMEOUT", 30*time.Second))
	defer stopCancel()
	if err := server.Stop(stopCtx); err != nil && !errors.Is(err, vialite.ErrNotStarted) {
		cancel()
		return fmt.Errorf("stop vialite: %w", err)
	}
	cancel()
	select {
	case err := <-runErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("vialite runtime: %w", err)
		}
	case <-time.After(5 * time.Second):
		return errors.New("vialite runtime did not exit after stop")
	}
	return nil
}

func smokeMode(value string) (vialite.Mode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "embedded":
		return vialite.ModeEmbedded, nil
	case "subprocess":
		return vialite.ModeSubprocess, nil
	default:
		return 0, fmt.Errorf("unsupported VIALITE_SMOKE_MODE %q", value)
	}
}

func modeName(mode vialite.Mode) string {
	switch mode {
	case vialite.ModeEmbedded:
		return "embedded"
	case vialite.ModeSubprocess:
		return "subprocess"
	default:
		return fmt.Sprintf("mode-%d", mode)
	}
}

func clientCommand() ([]string, error) {
	raw := os.Getenv("VIALITE_SMOKE_CLIENT_COMMAND_JSON")
	if raw == "" {
		return nil, errors.New("VIALITE_SMOKE_CLIENT_COMMAND_JSON is required")
	}
	var command []string
	if err := json.Unmarshal([]byte(raw), &command); err != nil {
		return nil, fmt.Errorf("parse VIALITE_SMOKE_CLIENT_COMMAND_JSON: %w", err)
	}
	if len(command) == 0 {
		return nil, errors.New("VIALITE_SMOKE_CLIENT_COMMAND_JSON must not be empty")
	}
	for _, part := range command {
		if strings.TrimSpace(part) == "" {
			return nil, errors.New("VIALITE_SMOKE_CLIENT_COMMAND_JSON contains a blank argument")
		}
	}
	return command, nil
}

func envDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}

func appendEnv(env []string, key string, value string) []string {
	prefix := key + "="
	filtered := env[:0]
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, prefix+value)
}
