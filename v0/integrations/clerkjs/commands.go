package clerkjs

import (
	"context"
	"fmt"
)

// =============================================================================
// CLI COMMANDS
// =============================================================================

func (p *ClerkPlugin) HandleSyncCommand(ctx context.Context, args []string, flags map[string]interface{}) error {
	if p.userService == nil {
		return fmt.Errorf("user service not configured")
	}

	dryRun, _ := flags["dry-run"].(bool)
	batchSize, _ := flags["batch-size"].(int)

	if batchSize <= 0 {
		batchSize = 100
	}

	fmt.Printf("Starting user sync (batch size: %d, dry run: %t)\n", batchSize, dryRun)

	result, err := p.userService.SyncUsersFromClerk(ctx, batchSize, dryRun)
	if err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	fmt.Printf("Sync completed:\n")
	fmt.Printf("  Total processed: %d\n", result.TotalProcessed)
	fmt.Printf("  Created: %d\n", result.Created)
	fmt.Printf("  Updated: %d\n", result.Updated)
	fmt.Printf("  Duration: %v\n", result.Duration)

	if len(result.Errors) > 0 {
		fmt.Printf("  Errors: %d\n", len(result.Errors))
		for _, err := range result.Errors {
			fmt.Printf("    - %s\n", err)
		}
	}

	return nil
}
