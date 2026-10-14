package exporters

// ParseMetricKeyForTest exposes parseMetricKey to tests in other packages.
func ParseMetricKeyForTest(key string) (string, map[string]string) {
	return parseMetricKey(key)
}
