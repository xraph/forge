package state

import (
	"errors"
	"fmt"
	"strings"
)

// CheckRollback includes migrations completed by interrupted attempts.
func CheckRollback(snap Snapshot, journal Journal, destination Release) error {
	destinationIndex := -1

	activeIndex := len(snap.Releases) - 1
	for i, release := range snap.Releases {
		if release.ID == destination.ID {
			destinationIndex = i
		}

		if release.PlanHash == snap.ActivePlanHash {
			activeIndex = i
		}
	}

	if destinationIndex < 0 {
		return errors.New("rollback release is unavailable")
	}

	if snap.ActivePlanHash != "" && destinationIndex >= activeIndex && snap.ActivePlanHash == snap.Releases[activeIndex].PlanHash {
		return errors.New("rollback must select an earlier release")
	}

	for _, release := range snap.Releases[destinationIndex+1:] {
		for id, reversible := range release.Migrations {
			if !reversible {
				return fmt.Errorf("%s is not marked reversible; roll forward", id)
			}
		}
	}

	events, err := journal.Events()
	if err != nil {
		return err
	}

	for _, event := range events {
		if strings.HasPrefix(event.Op, "migrate:") && event.Status == StatusAccepted && event.Time.After(destination.AppliedAt) && !strings.HasPrefix(event.IdempotencyKey, destination.PlanHash+":") {
			return fmt.Errorf("%s completed after this release; migration rollback requires an approved reversible executor", event.Op)
		}
	}

	return nil
}
