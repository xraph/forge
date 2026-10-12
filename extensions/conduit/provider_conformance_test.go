package conduit_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/conduit/core"
	"github.com/xraph/forge/extensions/conduit/providers/conformance"
	"github.com/xraph/forge/extensions/conduit/providers/jetstream"
	"github.com/xraph/forge/extensions/conduit/providers/kafka"
	"github.com/xraph/forge/extensions/conduit/providers/memory"
	"github.com/xraph/forge/extensions/conduit/providers/postgres"
	"github.com/xraph/forge/extensions/conduit/providers/redisstreams"
)

func TestProviderConformance(t *testing.T) {
	const lease = 2 * time.Second

	t.Run("memory", func(t *testing.T) {
		b := memory.NewWithOptions(memory.Options{LeaseTTL: lease})
		conformance.Run(t, func() core.Provider { return b }, conformance.WithLeaseTTL(lease))
	})
	t.Run("jetstream", func(t *testing.T) {
		srv := brokerServer(t, t.TempDir(), -1)
		conformance.Run(t, func() core.Provider { return jetstream.New(jetstream.Options{URL: srv.ClientURL(), LeaseTTL: lease}) }, conformance.WithLeaseTTL(lease))
	})

	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv("CONDUIT_TEST_POSTGRES")
		if dsn == "" {
			if os.Getenv("CONDUIT_REQUIRE_PROVIDERS") == "1" {
				t.Fatal("CONDUIT_TEST_POSTGRES is required")
			}

			t.Skip("set CONDUIT_TEST_POSTGRES to run real broker conformance")
		}

		migrator := postgres.New(postgres.Options{URL: dsn, AutoMigrate: true})
		if err := migrator.Connect(t.Context()); err != nil {
			t.Fatal(err)
		}

		if err := migrator.Close(t.Context()); err != nil {
			t.Fatal(err)
		}

		conformance.Run(t, func() core.Provider {
			return postgres.New(postgres.Options{URL: dsn, LeaseTTL: lease, PollInterval: 100 * time.Millisecond})
		}, conformance.WithLeaseTTL(lease))
	})

	for _, name := range []string{"redis", "kafka"} {
		t.Run(name, func(t *testing.T) {
			variable := "CONDUIT_TEST_REDIS"
			if name == "kafka" {
				variable = "CONDUIT_TEST_KAFKA"
			}

			address := os.Getenv(variable)
			if address == "" {
				if os.Getenv("CONDUIT_REQUIRE_PROVIDERS") == "1" {
					t.Fatalf("%s is required", variable)
				}

				t.Skip("set " + variable + " to run real broker conformance")
			}

			conformance.Run(t, func() core.Provider {
				if name == "redis" {
					return redisstreams.New(redisstreams.Options{URL: address, LeaseTTL: lease})
				}

				return kafka.New(kafka.Options{Brokers: strings.Split(address, ",")})
			}, conformance.WithLeaseTTL(lease))
		})
	}
}
