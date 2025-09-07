package config

import (
	"context"
	"time"

	"github.com/xraph/forge/pkg/common"
)

type testConfig struct{}

func NewTestConfig() common.ConfigManager {
	return &testConfig{}
}

func (tc *testConfig) Get(key string) interface{}                { return nil }
func (tc *testConfig) GetString(key string) string               { return "" }
func (tc *testConfig) GetInt(key string) int                     { return 0 }
func (tc *testConfig) GetBool(key string) bool                   { return false }
func (tc *testConfig) GetDuration(key string) time.Duration      { return 0 }
func (tc *testConfig) Set(key string, value interface{})         {}
func (tc *testConfig) Bind(key string, target interface{}) error { return nil }
func (tc *testConfig) Watch(ctx context.Context) error {
	return nil
}
func (tc *testConfig) WatchWithCallback(key string, callback func(key string, value interface{})) {}

func (tc *testConfig) Reload() error { return nil }
