package discovery

import (
	"os"
	"strings"
)

// advertisedAddress never publishes a wildcard listener as a service endpoint.
func advertisedAddress(explicit string) string {
	valid := func(host string) bool { return host != "" && host != "0.0.0.0" && host != "::" && host != "[::]" }
	if valid(explicit) {
		return strings.Trim(explicit, "[]")
	}

	for _, key := range []string{"FORGE_ADVERTISE_ADDR", "POD_IP"} {
		if host := os.Getenv(key); valid(host) {
			return strings.Trim(host, "[]")
		}
	}

	if host, err := os.Hostname(); err == nil && valid(host) {
		return host
	}

	return "localhost"
}
