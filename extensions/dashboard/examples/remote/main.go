// Package main demonstrates how to register a remote contributor with the
// dashboard extension.
//
// A remote contributor is a separate service that exposes dashboard pages,
// widgets, and settings via HTTP fragment endpoints. The dashboard shell
// proxies requests to the remote service and embeds the HTML fragments.
//
// In production, remote contributors can be auto-discovered via the
// discovery extension. This example shows manual registration instead.
//
// NOTE: This is an illustrative stub. It requires a full Forge application
// environment (and a running remote service) to function.
package main

import (
	"log"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

func main() {
	app := forge.New(
		forge.WithAppName("remote-dashboard-example"),
		forge.WithAppVersion("1.0.0"),
	)

	// Create the dashboard extension with discovery enabled.
	// In production, services tagged with "forge-dashboard-contributor" will
	// be auto-discovered and their manifests fetched automatically.
	dashExt := dashboard.NewExtension(
		dashboard.WithTitle("Distributed Dashboard"),
		dashboard.WithBasePath("/dashboard"),
		dashboard.WithDiscovery(true),
	)

	if err := app.RegisterExtension(dashExt); err != nil {
		log.Fatalf("failed to register dashboard extension: %v", err)
	}

	// Access the typed extension to use contributor-specific APIs.
	typedDashExt := dashExt.(*dashboard.Extension)

	// Manually register a remote contributor.
	// The manifest describes the pages and navigation the remote service provides.
	remoteManifest := &contributor.Manifest{
		Name:        "billing",
		DisplayName: "Billing Service",
		Icon:        "credit-card",
		Version:     "1.0.0",
		Nav: []contributor.NavItem{
			{Label: "Invoices", Path: "/invoices", Icon: "file-text", Group: "Platform", Priority: 10},
			{Label: "Plans", Path: "/plans", Icon: "package", Group: "Platform", Priority: 11},
		},
		Widgets: []contributor.WidgetDescriptor{
			{ID: "revenue", Title: "Monthly Revenue", Size: "md", RefreshSec: 300, Group: "Platform", Priority: 5},
		},
	}

	// Create the remote contributor pointing at the billing service.
	// The service must expose:
	//   GET <baseURL>/_forge/dashboard/manifest    (JSON manifest)
	//   GET <baseURL>/_forge/dashboard/pages/*      (HTML page fragments)
	//   GET <baseURL>/_forge/dashboard/widgets/:id  (HTML widget fragments)
	remote := contributor.NewRemoteContributor(
		"http://billing-service:8081",
		remoteManifest,
		contributor.WithAPIKey("secret-api-key"),
	)

	if err := typedDashExt.Registry().RegisterRemote(remote); err != nil {
		log.Fatalf("failed to register remote billing contributor: %v", err)
	}

	// The dashboard will now include:
	//   Platform group:
	//     - Invoices -> proxied from billing-service:8081
	//     - Plans    -> proxied from billing-service:8081
	//
	// Remote page routes:
	//   GET /dashboard/remote/billing/pages/invoices
	//   GET /dashboard/remote/billing/pages/plans
	//   GET /dashboard/remote/billing/widgets/revenue
	if err := app.Run(); err != nil {
		log.Fatalf("application error: %v", err)
	}
}
