package main

import (
	"context"
	"log"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/cron"
)

func main() {
	// Create Forge app
	app := forge.New()

	// Register cron extension
	cronExt := cron.NewExtension(
		cron.WithMode("simple"),
		cron.WithStorage("memory"),
		cron.WithMaxConcurrentJobs(5),
		cron.WithDefaultTimeout(5*time.Minute),
		cron.WithMaxRetries(3),
		cron.WithAPI(true),
		cron.WithMetrics(true),
	)
	app.RegisterExtension(cronExt)

	// Get job registry from DI container
	registry, err := forge.Inject[*cron.JobRegistry](app.Container())
	if err != nil {
		log.Fatal(err)
	}

	// Register job handlers
	registry.Register("sayHello", func(ctx context.Context, job *cron.Job) error {
		log.Printf("Hello from job: %s", job.Name)
		return nil
	})

	registry.Register("doBackup", func(ctx context.Context, job *cron.Job) error {
		log.Printf("Running backup job: %s", job.Name)
		// Simulate backup work
		time.Sleep(2 * time.Second)
		log.Printf("Backup completed")
		return nil
	})

	registry.Register("sendReport", func(ctx context.Context, job *cron.Job) error {
		log.Printf("Sending report: %s", job.Name)
		// Simulate report generation
		time.Sleep(1 * time.Second)
		log.Printf("Report sent")
		return nil
	})

	// Get scheduler from DI container
	scheduler, err := forge.Inject[cron.Scheduler](app.Container())
	if err != nil {
		log.Fatal(err)
	}

	// Add jobs programmatically
	jobs := []*cron.Job{
		{
			ID:          "hello-job",
			Name:        "Hello Job",
			Schedule:    "*/10 * * * * *", // Every 10 seconds
			HandlerName: "sayHello",
			Enabled:     true,
			MaxRetries:  3,
			Timeout:     30 * time.Second,
		},
		{
			ID:          "backup-job",
			Name:        "Backup Job",
			Schedule:    "0 */2 * * * *", // Every 2 minutes
			HandlerName: "doBackup",
			Enabled:     true,
			MaxRetries:  5,
			Timeout:     5 * time.Minute,
		},
		{
			ID:          "report-job",
			Name:        "Daily Report",
			Schedule:    "0 0 9 * * *", // Every day at 9 AM
			HandlerName: "sendReport",
			Enabled:     false, // Disabled for demo
			MaxRetries:  3,
			Timeout:     2 * time.Minute,
		},
	}

	for _, job := range jobs {
		if err := scheduler.AddJob(job); err != nil {
			log.Printf("Failed to add job %s: %v", job.ID, err)
		} else {
			log.Printf("Added job: %s (%s)", job.Name, job.Schedule)
		}
	}

	// Run the app
	log.Println("Starting Forge app with cron extension...")
	log.Println("API available at http://localhost:8080/api/cron")
	log.Println("Press Ctrl+C to stop")

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
