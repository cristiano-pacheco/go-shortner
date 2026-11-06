package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

const profileSocket = "/tmp/go-shortner-profile.sock"

// Profiling is explicitly enabled for diagnostic runs and never binds a TCP port.
func startProfiling() (*http.Server, error) {
	if os.Getenv("LOAD_ENABLE_PROFILING") != "1" {
		return nil, nil
	}

	listener, err := net.Listen("unix", profileSocket)
	if err != nil {
		return nil, fmt.Errorf("profile socket: %w", err)
	}
	if err := os.Chmod(profileSocket, 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("protect profile socket: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/heap", pprof.Handler("heap").ServeHTTP)
	mux.HandleFunc("/debug/pprof/goroutine", pprof.Handler("goroutine").ServeHTTP)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		_ = server.Serve(listener)
	}()
	return server, nil
}

func NewProfileCaptureCommand() *cobra.Command {
	return &cobra.Command{
		Use: "profile-capture [cpu|heap|goroutine] [seconds]", Hidden: true, Args: cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			seconds, err := strconv.Atoi(args[1])
			if err != nil || seconds < 1 || seconds > 60 {
				return fmt.Errorf("seconds must be between 1 and 60")
			}
			profile := args[0]
			if profile != "cpu" && profile != "heap" && profile != "goroutine" {
				return fmt.Errorf("profile must be cpu, heap or goroutine")
			}
			if profile == "cpu" {
				profile = "profile"
			}
			transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", profileSocket)
			}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: time.Duration(seconds+10) * time.Second}
			request, err := http.NewRequestWithContext(command.Context(), http.MethodGet,
				fmt.Sprintf("http://profile/debug/pprof/%s?seconds=%d", profile, seconds), nil)
			if err != nil {
				return err
			}
			response, err := client.Do(request)
			if err != nil {
				return err
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				return fmt.Errorf("profile request returned %s", response.Status)
			}
			_, err = io.Copy(command.OutOrStdout(), response.Body)
			return err
		},
	}
}
