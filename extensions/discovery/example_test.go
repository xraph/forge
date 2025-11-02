package discovery_test

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/discovery"
)

func ExampleBasicUsage() {
	app := forge.NewApp(forge.AppConfig{
		Extensions: []forge.Extension{
			discovery.NewExtension(
				discovery.WithEnabled(true),
				discovery.WithBackend("memory"),
				discovery.WithServiceName("my-api"),
				discovery.WithServiceAddress("localhost", 8080),
				discovery.WithServiceTags("api", "v1"),
				discovery.WithHTTPHealthCheck("http://localhost:8080/_/health", 10*time.Second),
			),
		},
	})

	// Get discovery service
	discoveryService := forge.Must[*discovery.Service](app.Container(), "discovery.Service")

	router := app.Router()
	router.GET("/call-user-service", func(ctx forge.Context) error {
		// Find a healthy user-service instance
		instance, err := discoveryService.SelectInstance(
			ctx.Context(),
			"user-service",
			discovery.LoadBalanceRoundRobin,
		)
		if err != nil {
			return err
		}

		// Make request to selected instance
		url := instance.URL("http") + "/users"
		resp, err := http.Get(url)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		return ctx.JSON(200, map[string]interface{}{
			"called_instance": instance.ID,
		})
	})

	app.Run()
}

func ExampleConsulBackend() {
	app := forge.NewApp(forge.AppConfig{
		Extensions: []forge.Extension{
			discovery.NewExtension(
				discovery.WithConsul("consul.company.com:8500", "consul-token"),
				discovery.WithServiceName("my-api"),
				discovery.WithServiceAddress("10.0.1.5", 8080),
				discovery.WithServiceTags("production", "v1"),
				discovery.WithHTTPHealthCheck("http://10.0.1.5:8080/_/health", 10*time.Second),
			),
		},
	})

	app.Run()
}

func ExampleServiceWatch() {
	app := forge.NewApp(forge.AppConfig{
		Extensions: []forge.Extension{
			discovery.NewExtension(
				discovery.WithBackend("memory"),
				discovery.WithWatch([]string{"user-service"}, func(instances []*discovery.ServiceInstance) {
					fmt.Printf("user-service instances changed: %d instances\n", len(instances))
					// Update load balancer pool, etc.
				}),
			),
		},
	})

	app.Run()
}

func ExampleManualRegistration() {
	app := forge.NewApp(forge.AppConfig{
		Extensions: []forge.Extension{
			discovery.NewExtension(
				discovery.WithEnabled(true),
				discovery.WithBackend("memory"),
			),
		},
	})

	discoveryService := forge.Must[*discovery.Service](app.Container(), "discovery.Service")

	// Manually register a service instance
	instance := &discovery.ServiceInstance{
		ID:      "my-service-1",
		Name:    "my-service",
		Version: "1.0.0",
		Address: "10.0.1.5",
		Port:    8080,
		Tags:    []string{"api"},
		Metadata: map[string]string{
			"region": "us-east-1",
		},
		Status: discovery.HealthStatusPassing,
	}

	err := discoveryService.Register(context.Background(), instance)
	if err != nil {
		panic(err)
	}

	app.Run()
}

func ExampleLoadBalancing() {
	app := forge.NewApp(forge.AppConfig{
		Extensions: []forge.Extension{
			discovery.NewExtension(
				discovery.WithEnabled(true),
				discovery.WithBackend("memory"),
			),
		},
	})

	discoveryService := forge.Must[*discovery.Service](app.Container(), "discovery.Service")

	router := app.Router()

	// Round-robin load balancing
	router.GET("/round-robin", func(ctx forge.Context) error {
		instance, err := discoveryService.SelectInstance(
			ctx.Context(),
			"user-service",
			discovery.LoadBalanceRoundRobin,
		)
		if err != nil {
			return err
		}
		return ctx.JSON(200, instance)
	})

	// Random load balancing
	router.GET("/random", func(ctx forge.Context) error {
		instance, err := discoveryService.SelectInstance(
			ctx.Context(),
			"user-service",
			discovery.LoadBalanceRandom,
		)
		if err != nil {
			return err
		}
		return ctx.JSON(200, instance)
	})

	app.Run()
}

func ExampleDiscoverWithTags() {
	app := forge.NewApp(forge.AppConfig{
		Extensions: []forge.Extension{
			discovery.NewExtension(
				discovery.WithEnabled(true),
				discovery.WithBackend("memory"),
			),
		},
	})

	discoveryService := forge.Must[*discovery.Service](app.Container(), "discovery.Service")

	router := app.Router()
	router.GET("/api/services", func(ctx forge.Context) error {
		// Discover only production v2 services
		instances, err := discoveryService.DiscoverWithTags(
			ctx.Context(),
			"user-service",
			[]string{"production", "v2"},
		)
		if err != nil {
			return err
		}

		return ctx.JSON(200, map[string]interface{}{
			"instances": instances,
		})
	})

	app.Run()
}

func ExampleDiscoveryClient() {
	app := forge.NewApp(forge.AppConfig{
		Extensions: []forge.Extension{
			discovery.NewExtension(
				discovery.WithEnabled(true),
				discovery.WithBackend("memory"),
			),
		},
	})

	discoveryService := forge.Must[*discovery.Service](app.Container(), "discovery.Service")

	// Create HTTP client with service discovery
	type DiscoveryClient struct {
		service     *discovery.Service
		serviceName string
		strategy    discovery.LoadBalanceStrategy
	}

	getClient := func(ctx forge.Context, serviceName string) (*DiscoveryClient, error) {
		return &DiscoveryClient{
			service:     discoveryService,
			serviceName: serviceName,
			strategy:    discovery.LoadBalanceRoundRobin,
		}, nil
	}

	router := app.Router()
	router.GET("/users/:id", func(ctx forge.Context) error {
		client, err := getClient(ctx, "user-service")
		if err != nil {
			return err
		}

		url, err := client.service.GetServiceURL(
			ctx.Context(),
			client.serviceName,
			"http",
			client.strategy,
		)
		if err != nil {
			return err
		}

		userID := ctx.Param("id")
		resp, err := http.Get(url + "/users/" + userID)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		return ctx.JSON(200, map[string]string{
			"status": "success",
		})
	})

	app.Run()
}

func ExampleHealthyInstancesOnly() {
	app := forge.NewApp(forge.AppConfig{
		Extensions: []forge.Extension{
			discovery.NewExtension(
				discovery.WithEnabled(true),
				discovery.WithBackend("memory"),
			),
		},
	})

	discoveryService := forge.Must[*discovery.Service](app.Container(), "discovery.Service")

	router := app.Router()
	router.GET("/api/healthy-instances", func(ctx forge.Context) error {
		// Discover only healthy instances
		instances, err := discoveryService.DiscoverHealthy(ctx.Context(), "user-service")
		if err != nil {
			return err
		}

		return ctx.JSON(200, map[string]interface{}{
			"healthy_instances": instances,
			"count":             len(instances),
		})
	})

	app.Run()
}

func ExampleListAllServices() {
	app := forge.NewApp(forge.AppConfig{
		Extensions: []forge.Extension{
			discovery.NewExtension(
				discovery.WithEnabled(true),
				discovery.WithBackend("memory"),
			),
		},
	})

	discoveryService := forge.Must[*discovery.Service](app.Container(), "discovery.Service")

	router := app.Router()
	router.GET("/_/discovery/services", func(ctx forge.Context) error {
		services, err := discoveryService.ListServices(ctx.Context())
		if err != nil {
			return err
		}

		return ctx.JSON(200, map[string]interface{}{
			"services": services,
		})
	})

	app.Run()
}
