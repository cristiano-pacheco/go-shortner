//go:build integration

package fixture

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Application owns a server process started with the production command.
type Application struct {
	t       *testing.T
	command *exec.Cmd
	done    chan error
	logPath string
	stopped bool
}

// StartApplication starts the executable, waits for its health endpoint, and
// registers cleanup for any test that does not stop it explicitly.
func StartApplication(t *testing.T, binary string, env []string, healthURL string) *Application {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "server.log")
	output, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "server")
	command.Env = env
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	app := &Application{t: t, command: command, done: make(chan error, 1), logPath: logPath}
	go func() {
		err := command.Wait()
		_ = output.Close()
		app.done <- err
	}()
	t.Cleanup(func() {
		if !app.stopped {
			_ = app.command.Process.Kill()
			<-app.done
			app.stopped = true
		}
	})

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, err := client.Get(healthURL)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("health endpoint returned status %d", resp.StatusCode)
			}
			return app
		}
		select {
		case err := <-app.done:
			app.stopped = true
			logs, _ := os.ReadFile(logPath)
			t.Fatalf("server exited before HTTP startup: %v\n%s", err, logs)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not become ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Stop sends SIGTERM and waits for a successful graceful shutdown.
func (app *Application) Stop() {
	app.t.Helper()
	if app.stopped {
		app.t.Fatal("server is already stopped")
	}
	if err := app.command.Process.Signal(syscall.SIGTERM); err != nil {
		app.t.Fatal(err)
	}
	select {
	case err := <-app.done:
		app.stopped = true
		if err != nil {
			logs, _ := os.ReadFile(app.logPath)
			app.t.Fatalf("server shutdown: %v\n%s", err, logs)
		}
	case <-time.After(15 * time.Second):
		app.t.Fatal("server did not stop after SIGTERM")
	}
}
