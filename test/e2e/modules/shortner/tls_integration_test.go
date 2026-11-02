//go:build e2e

package shortner_test

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/repository"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/service"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/validator"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/database"
)

//nolint:paralleltest // TLS subtests share configuration and mutate the process environment.
func TestPostgresTLSModes(t *testing.T) {
	stack := start(t)
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.crt")
	caKey := filepath.Join(dir, "ca.key")
	serverKey := filepath.Join(dir, "server.key")
	serverCSR := filepath.Join(dir, "server.csr")
	serverCert := filepath.Join(dir, "server.crt")
	extension := filepath.Join(dir, "server.ext")
	require.NoError(
		t,
		os.WriteFile(
			extension,
			[]byte("subjectAltName=IP:127.0.0.1,DNS:localhost\nextendedKeyUsage=serverAuth\n"),
			0600,
		),
	)

	openssl := func(args ...string) {
		t.Helper()

		out, err := exec.Command("openssl", args...).CombinedOutput()
		require.NoError(t, err, "%s", out)
	}
	openssl(
		"req",
		"-x509",
		"-newkey",
		"rsa:2048",
		"-nodes",
		"-keyout",
		caKey,
		"-out",
		ca,
		"-days",
		"1",
		"-subj",
		"/CN=test-ca",
	)
	openssl("req", "-newkey", "rsa:2048", "-nodes", "-keyout", serverKey, "-out", serverCSR, "-subj", "/CN=localhost")
	openssl(
		"x509",
		"-req",
		"-in",
		serverCSR,
		"-CA",
		ca,
		"-CAkey",
		caKey,
		"-CAcreateserial",
		"-out",
		serverCert,
		"-days",
		"1",
		"-extfile",
		extension,
	)

	run := func(args ...string) string {
		t.Helper()

		out, err := stack.run(args...)
		require.NoError(t, err, "%s", out)

		return strings.TrimSpace(string(out))
	}
	run("cp", serverCert, "postgres:/tmp/test-server.crt")
	run("cp", serverKey, "postgres:/tmp/test-server.key")
	run(
		"exec",
		"-T",
		"-u",
		"0",
		"postgres",
		"chown",
		"postgres:postgres",
		"/tmp/test-server.crt",
		"/tmp/test-server.key",
	)
	run("exec", "-T", "-u", "0", "postgres", "chmod", "600", "/tmp/test-server.key")

	for _, statement := range []string{
		"ALTER SYSTEM SET ssl_cert_file = '/tmp/test-server.crt'",
		"ALTER SYSTEM SET ssl_key_file = '/tmp/test-server.key'",
		"ALTER SYSTEM SET ssl = 'on'",
		"SELECT pg_reload_conf()",
	} {
		run(
			"exec",
			"-T",
			"postgres",
			"psql",
			"-X",
			"-v",
			"ON_ERROR_STOP=1",
			"-U",
			"shortener",
			"-d",
			"shortener",
			"-c",
			statement,
		)
	}

	require.Eventually(t, func() bool {
		return run(
			"exec",
			"-T",
			"postgres",
			"psql",
			"-X",
			"-A",
			"-t",
			"-U",
			"shortener",
			"-d",
			"shortener",
			"-c",
			"SHOW ssl",
		) == "on"
	}, 10*time.Second, 200*time.Millisecond)
	t.Setenv("PGSSLROOTCERT", ca)

	host, portNumber, err := net.SplitHostPort(run("port", "postgres", "5432"))
	require.NoError(t, err)

	for _, mode := range []string{"require", "verify-ca", "verify-full"} {
		t.Run(mode, func(t *testing.T) {
			cfg := config.Config{
				DatabaseHost:     host,
				DatabasePort:     portNumber,
				DatabaseName:     "shortener",
				DatabaseUser:     "shortener",
				DatabasePassword: "e2e-only",
				DatabaseSSLMode:  mode,
				PublicBaseURL:    publicBase,
			}

			var create *usecase.LinkCreateUseCase

			app := fx.New(fx.NopLogger, fx.Supply(cfg), database.Module, fx.Provide(
				fx.Annotate(repository.NewLinkRepository, fx.As(new(port.LinkRepository))),
				fx.Annotate(service.NewCodeGeneratorService, fx.As(new(port.CodeGenerator))),
				fx.Annotate(validator.NewDestinationValidator, fx.As(new(port.DestinationValidator))),
				usecase.NewLinkCreateUseCase,
			), fx.Populate(&create))
			require.NoError(t, app.Err())

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			require.NoError(t, app.Start(ctx))
			defer func() {
				require.NoError(t, app.Stop(context.Background()))
			}()

			alias := "tls-" + mode
			result, createErr := create.Execute(
				ctx,
				usecase.LinkCreateInput{Destination: "https://example.com/tls", Alias: &alias},
			)
			require.NoError(t, createErr)
			require.Equal(t, alias, result.Code)
		})
	}
}
