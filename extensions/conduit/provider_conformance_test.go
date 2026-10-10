package conduit_test

import (
	"os"
	"strings"
	"testing"

	"github.com/xraph/forge/extensions/conduit/core"
	"github.com/xraph/forge/extensions/conduit/providers/conformance"
	"github.com/xraph/forge/extensions/conduit/providers/jetstream"
	"github.com/xraph/forge/extensions/conduit/providers/kafka"
	"github.com/xraph/forge/extensions/conduit/providers/memory"
	"github.com/xraph/forge/extensions/conduit/providers/redisstreams"
)

func TestProviderConformance(t *testing.T) {
	t.Run("memory", func(t *testing.T) { b := memory.New(); conformance.Run(t, func() core.Provider { return b }) })
	t.Run("jetstream", func(t *testing.T) {
		srv := brokerServer(t, t.TempDir(), -1)
		conformance.Run(t, func() core.Provider { return jetstream.New(jetstream.Options{URL: srv.ClientURL()}) })
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
					return redisstreams.New(redisstreams.Options{URL: address})
				}

				return kafka.New(kafka.Options{Brokers: strings.Split(address, ",")})
			})
		})
	}
}
