package di

import (
	"context"
	"testing"
)

// Benchmark service registration
func BenchmarkRegister_Singleton(b *testing.B) {
	c := NewContainer()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := "service"
		_ = c.Register(name, func(c Container) (any, error) {
			return "value", nil
		}, Singleton())
	}
}

func BenchmarkRegister_Transient(b *testing.B) {
	c := NewContainer()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := "service"
		_ = c.Register(name, func(c Container) (any, error) {
			return "value", nil
		}, Transient())
	}
}

func BenchmarkRegister_Scoped(b *testing.B) {
	c := NewContainer()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := "service"
		_ = c.Register(name, func(c Container) (any, error) {
			return "value", nil
		}, Scoped())
	}
}

// Benchmark service resolution
func BenchmarkResolve_Singleton_Cached(b *testing.B) {
	c := NewContainer()
	_ = c.Register("service", func(c Container) (any, error) {
		return "value", nil
	}, Singleton())

	// Warm up cache
	_, _ = c.Resolve("service")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Resolve("service")
	}
}

func BenchmarkResolve_Singleton_Uncached(b *testing.B) {
	// Benchmark first-time resolution (uncached) by creating fresh containers
	// Limited to reasonable iteration count
	c := NewContainer()
	_ = c.Register("service", func(c Container) (any, error) {
		return "value", nil
	}, Singleton())

	b.ResetTimer()
	// First resolve - measures uncached path
	_, _ = c.Resolve("service")
}

func BenchmarkResolve_Transient(b *testing.B) {
	c := NewContainer()
	_ = c.Register("service", func(c Container) (any, error) {
		return "value", nil
	}, Transient())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Resolve("service")
	}
}

// Benchmark scope operations
func BenchmarkScope_Create(b *testing.B) {
	c := NewContainer()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scope := c.BeginScope()
		_ = scope.End()
	}
}

func BenchmarkScope_Resolve_Cached(b *testing.B) {
	c := NewContainer()
	_ = c.Register("service", func(c Container) (any, error) {
		return "value", nil
	}, Scoped())

	scope := c.BeginScope()
	defer scope.End()

	// Warm up cache
	_, _ = scope.Resolve("service")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = scope.Resolve("service")
	}
}

func BenchmarkScope_Resolve_Uncached(b *testing.B) {
	c := NewContainer()
	_ = c.Register("service", func(c Container) (any, error) {
		return "value", nil
	}, Scoped())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		scope := c.BeginScope()
		b.StartTimer()

		_, _ = scope.Resolve("service")

		b.StopTimer()
		_ = scope.End()
		b.StartTimer()
	}
}

// Benchmark lifecycle operations
func BenchmarkStart_10Services(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		c := NewContainer()
		for j := 0; j < 10; j++ {
			name := string(rune('a' + j))
			_ = c.Register(name, func(c Container) (any, error) {
				return &mockService{name: name, healthy: true}, nil
			})
		}
		b.StartTimer()

		ctx := context.Background()
		_ = c.Start(ctx)
	}
}

func BenchmarkStart_100Services(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		c := NewContainer()
		for j := 0; j < 100; j++ {
			name := string(rune('a' + (j % 26)))
			_ = c.Register(name, func(c Container) (any, error) {
				return &mockService{name: name, healthy: true}, nil
			})
		}
		b.StartTimer()

		ctx := context.Background()
		_ = c.Start(ctx)
	}
}

func BenchmarkHealth_10Services(b *testing.B) {
	c := NewContainer()
	for i := 0; i < 10; i++ {
		name := string(rune('a' + i))
		_ = c.Register(name, func(c Container) (any, error) {
			return &mockService{name: name, healthy: true}, nil
		})
	}

	ctx := context.Background()
	_ = c.Start(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.Health(ctx)
	}
}

func BenchmarkHealth_100Services(b *testing.B) {
	c := NewContainer()
	for i := 0; i < 100; i++ {
		name := string(rune('a' + (i % 26)))
		_ = c.Register(name, func(c Container) (any, error) {
			return &mockService{name: name, healthy: true}, nil
		})
	}

	ctx := context.Background()
	_ = c.Start(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.Health(ctx)
	}
}

// Benchmark generic helpers
func BenchmarkResolveGeneric(b *testing.B) {
	c := NewContainer()
	_ = RegisterSingleton(c, "service", func(c Container) (*mockService, error) {
		return &mockService{name: "test"}, nil
	})

	// Warm up cache
	_, _ = Resolve[*mockService](c, "service")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Resolve[*mockService](c, "service")
	}
}

func BenchmarkMust(b *testing.B) {
	c := NewContainer()
	_ = RegisterSingleton(c, "service", func(c Container) (*mockService, error) {
		return &mockService{name: "test"}, nil
	})

	// Warm up cache
	_ = Must[*mockService](c, "service")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Must[*mockService](c, "service")
	}
}

// Benchmark concurrent access
func BenchmarkConcurrentResolve(b *testing.B) {
	c := NewContainer()
	_ = c.Register("service", func(c Container) (any, error) {
		return "value", nil
	}, Singleton())

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = c.Resolve("service")
		}
	})
}

func BenchmarkConcurrentScope(b *testing.B) {
	c := NewContainer()
	_ = c.Register("service", func(c Container) (any, error) {
		return "value", nil
	}, Scoped())

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			scope := c.BeginScope()
			_, _ = scope.Resolve("service")
			_ = scope.End()
		}
	})
}
