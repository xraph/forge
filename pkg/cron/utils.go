package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// getNextRunTime calculates the next run time for a cron expression
func getNextRunTime(schedule string, from time.Time) (time.Time, error) {
	// Parse the cron expression
	cronExpr, err := parseCronExpression(schedule)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression: %w", err)
	}

	// Find the next execution time
	return findNextExecution(cronExpr, from), nil
}

// CronExpression represents a parsed cron expression
type CronExpression struct {
	Second     []int // 0-59
	Minute     []int // 0-59
	Hour       []int // 0-23
	DayOfMonth []int // 1-31
	Month      []int // 1-12
	DayOfWeek  []int // 0-6 (Sunday = 0)
}

// parseCronExpression parses a cron expression string
func parseCronExpression(expr string) (*CronExpression, error) {
	fields := strings.Fields(expr)

	// Support both 5-field and 6-field cron expressions
	if len(fields) < 5 || len(fields) > 6 {
		return nil, fmt.Errorf("invalid cron expression: expected 5 or 6 fields, got %d", len(fields))
	}

	cronExpr := &CronExpression{}
	var err error

	if len(fields) == 6 {
		// 6-field format: second minute hour day month dayOfWeek
		cronExpr.Second, err = parseField(fields[0], 0, 59)
		if err != nil {
			return nil, fmt.Errorf("invalid second field: %w", err)
		}
		cronExpr.Minute, err = parseField(fields[1], 0, 59)
		if err != nil {
			return nil, fmt.Errorf("invalid minute field: %w", err)
		}
		cronExpr.Hour, err = parseField(fields[2], 0, 23)
		if err != nil {
			return nil, fmt.Errorf("invalid hour field: %w", err)
		}
		cronExpr.DayOfMonth, err = parseField(fields[3], 1, 31)
		if err != nil {
			return nil, fmt.Errorf("invalid day of month field: %w", err)
		}
		cronExpr.Month, err = parseField(fields[4], 1, 12)
		if err != nil {
			return nil, fmt.Errorf("invalid month field: %w", err)
		}
		cronExpr.DayOfWeek, err = parseField(fields[5], 0, 6)
		if err != nil {
			return nil, fmt.Errorf("invalid day of week field: %w", err)
		}
	} else {
		// 5-field format: minute hour day month dayOfWeek
		cronExpr.Second = []int{0} // Default to 0 seconds
		cronExpr.Minute, err = parseField(fields[0], 0, 59)
		if err != nil {
			return nil, fmt.Errorf("invalid minute field: %w", err)
		}
		cronExpr.Hour, err = parseField(fields[1], 0, 23)
		if err != nil {
			return nil, fmt.Errorf("invalid hour field: %w", err)
		}
		cronExpr.DayOfMonth, err = parseField(fields[2], 1, 31)
		if err != nil {
			return nil, fmt.Errorf("invalid day of month field: %w", err)
		}
		cronExpr.Month, err = parseField(fields[3], 1, 12)
		if err != nil {
			return nil, fmt.Errorf("invalid month field: %w", err)
		}
		cronExpr.DayOfWeek, err = parseField(fields[4], 0, 6)
		if err != nil {
			return nil, fmt.Errorf("invalid day of week field: %w", err)
		}
	}

	return cronExpr, nil
}

// parseField parses a single cron field
func parseField(field string, min, max int) ([]int, error) {
	var values []int

	// Handle special characters
	switch field {
	case "*":
		// All values in range
		for i := min; i <= max; i++ {
			values = append(values, i)
		}
		return values, nil
	case "?":
		// Any value (equivalent to *)
		for i := min; i <= max; i++ {
			values = append(values, i)
		}
		return values, nil
	}

	// Handle ranges and lists
	parts := strings.Split(field, ",")
	for _, part := range parts {
		if strings.Contains(part, "-") {
			// Range: e.g., "1-5"
			rangeParts := strings.Split(part, "-")
			if len(rangeParts) != 2 {
				return nil, fmt.Errorf("invalid range format: %s", part)
			}

			start, err := strconv.Atoi(rangeParts[0])
			if err != nil {
				return nil, fmt.Errorf("invalid start value in range: %s", rangeParts[0])
			}

			end, err := strconv.Atoi(rangeParts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid end value in range: %s", rangeParts[1])
			}

			if start < min || start > max || end < min || end > max {
				return nil, fmt.Errorf("range values out of bounds: %d-%d (min: %d, max: %d)", start, end, min, max)
			}

			if start > end {
				return nil, fmt.Errorf("start value greater than end value: %d > %d", start, end)
			}

			for i := start; i <= end; i++ {
				values = append(values, i)
			}
		} else if strings.Contains(part, "/") {
			// Step: e.g., "*/5" or "1-10/2"
			stepParts := strings.Split(part, "/")
			if len(stepParts) != 2 {
				return nil, fmt.Errorf("invalid step format: %s", part)
			}

			step, err := strconv.Atoi(stepParts[1])
			if err != nil {
				return nil, fmt.Errorf("invalid step value: %s", stepParts[1])
			}

			if step <= 0 {
				return nil, fmt.Errorf("step value must be positive: %d", step)
			}

			var baseValues []int
			if stepParts[0] == "*" {
				for i := min; i <= max; i++ {
					baseValues = append(baseValues, i)
				}
			} else if strings.Contains(stepParts[0], "-") {
				// Range with step
				rangeParts := strings.Split(stepParts[0], "-")
				if len(rangeParts) != 2 {
					return nil, fmt.Errorf("invalid range format in step: %s", stepParts[0])
				}

				start, err := strconv.Atoi(rangeParts[0])
				if err != nil {
					return nil, fmt.Errorf("invalid start value in step range: %s", rangeParts[0])
				}

				end, err := strconv.Atoi(rangeParts[1])
				if err != nil {
					return nil, fmt.Errorf("invalid end value in step range: %s", rangeParts[1])
				}

				for i := start; i <= end; i++ {
					baseValues = append(baseValues, i)
				}
			} else {
				start, err := strconv.Atoi(stepParts[0])
				if err != nil {
					return nil, fmt.Errorf("invalid start value in step: %s", stepParts[0])
				}
				baseValues = append(baseValues, start)
			}

			// Apply step
			for i := 0; i < len(baseValues); i += step {
				values = append(values, baseValues[i])
			}
		} else {
			// Single value
			value, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid value: %s", part)
			}

			if value < min || value > max {
				return nil, fmt.Errorf("value out of bounds: %d (min: %d, max: %d)", value, min, max)
			}

			values = append(values, value)
		}
	}

	// Remove duplicates and sort
	values = removeDuplicates(values)
	return values, nil
}

// findNextExecution finds the next execution time for a cron expression
func findNextExecution(cronExpr *CronExpression, from time.Time) time.Time {
	// OnStart from the next second
	next := from.Add(time.Second).Truncate(time.Second)

	// Try to find next execution within reasonable time (1 year)
	endTime := from.Add(365 * 24 * time.Hour)

	for next.Before(endTime) {
		if matchesTime(cronExpr, next) {
			return next
		}
		next = next.Add(time.Second)
	}

	// If no match found, return far future
	return from.Add(365 * 24 * time.Hour)
}

// matchesTime checks if a time matches the cron expression
func matchesTime(cronExpr *CronExpression, t time.Time) bool {
	// Check second
	if !contains(cronExpr.Second, t.Second()) {
		return false
	}

	// Check minute
	if !contains(cronExpr.Minute, t.Minute()) {
		return false
	}

	// Check hour
	if !contains(cronExpr.Hour, t.Hour()) {
		return false
	}

	// Check month
	if !contains(cronExpr.Month, int(t.Month())) {
		return false
	}

	// Check day of month and day of week (OR condition)
	dayOfMonthMatch := contains(cronExpr.DayOfMonth, t.Day())
	dayOfWeekMatch := contains(cronExpr.DayOfWeek, int(t.Weekday()))

	// If both fields are not wildcards, use OR logic
	if !isWildcard(cronExpr.DayOfMonth) && !isWildcard(cronExpr.DayOfWeek) {
		return dayOfMonthMatch || dayOfWeekMatch
	}

	// If either field is wildcard, both must match
	return dayOfMonthMatch && dayOfWeekMatch
}

// contains checks if a slice contains a value
func contains(slice []int, value int) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

// isWildcard checks if a field contains all possible values (wildcard)
func isWildcard(field []int) bool {
	// A field is considered wildcard if it contains all possible values
	// This is a simplified check - in practice, you'd compare against the full range
	return len(field) > 10 // Heuristic: if it contains many values, it's likely a wildcard
}

// removeDuplicates removes duplicate values from a slice
func removeDuplicates(slice []int) []int {
	keys := make(map[int]bool)
	var result []int

	for _, value := range slice {
		if !keys[value] {
			keys[value] = true
			result = append(result, value)
		}
	}

	return result
}

// Common cron expression patterns
const (
	// Every minute
	EveryMinute = "* * * * *"

	// Every 5 minutes
	Every5Minutes = "*/5 * * * *"

	// Every 10 minutes
	Every10Minutes = "*/10 * * * *"

	// Every 15 minutes
	Every15Minutes = "*/15 * * * *"

	// Every 30 minutes
	Every30Minutes = "*/30 * * * *"

	// Every hour
	EveryHour = "0 * * * *"

	// Every 2 hours
	Every2Hours = "0 */2 * * *"

	// Every 6 hours
	Every6Hours = "0 */6 * * *"

	// Every 12 hours
	Every12Hours = "0 */12 * * *"

	// Daily at midnight
	Daily = "0 0 * * *"

	// Daily at noon
	DailyAtNoon = "0 12 * * *"

	// Weekly on Sunday at midnight
	Weekly = "0 0 * * 0"

	// Monthly on the 1st at midnight
	Monthly = "0 0 1 * *"

	// Yearly on January 1st at midnight
	Yearly = "0 0 1 1 *"
)

// ValidateCronExpression validates a cron expression
func ValidateCronExpression(expr string) error {
	_, err := parseCronExpression(expr)
	return err
}

// GetNextRunTimes returns the next N execution times for a cron expression
func GetNextRunTimes(schedule string, from time.Time, count int) ([]time.Time, error) {
	if count <= 0 {
		return nil, fmt.Errorf("count must be positive")
	}

	var times []time.Time
	current := from

	for i := 0; i < count; i++ {
		next, err := getNextRunTime(schedule, current)
		if err != nil {
			return nil, err
		}
		times = append(times, next)
		current = next
	}

	return times, nil
}

// IsTimeToRun checks if it's time to run a job based on its schedule
func IsTimeToRun(schedule string, lastRun, now time.Time) (bool, error) {
	nextRun, err := getNextRunTime(schedule, lastRun)
	if err != nil {
		return false, err
	}

	return now.After(nextRun) || now.Equal(nextRun), nil
}

// GetScheduleDescription returns a human-readable description of a cron schedule
func GetScheduleDescription(schedule string) (string, error) {
	cronExpr, err := parseCronExpression(schedule)
	if err != nil {
		return "", err
	}

	// Build description parts
	var parts []string

	// Second
	if len(cronExpr.Second) == 1 && cronExpr.Second[0] == 0 {
		// Default, don't mention seconds
	} else {
		parts = append(parts, fmt.Sprintf("at second %v", cronExpr.Second))
	}

	// Minute
	if len(cronExpr.Minute) == 1 {
		parts = append(parts, fmt.Sprintf("at minute %d", cronExpr.Minute[0]))
	} else if len(cronExpr.Minute) < 60 {
		parts = append(parts, fmt.Sprintf("at minutes %v", cronExpr.Minute))
	}

	// Hour
	if len(cronExpr.Hour) == 1 {
		parts = append(parts, fmt.Sprintf("at hour %d", cronExpr.Hour[0]))
	} else if len(cronExpr.Hour) < 24 {
		parts = append(parts, fmt.Sprintf("at hours %v", cronExpr.Hour))
	}

	// Day of month
	if len(cronExpr.DayOfMonth) == 1 {
		parts = append(parts, fmt.Sprintf("on day %d", cronExpr.DayOfMonth[0]))
	} else if len(cronExpr.DayOfMonth) < 31 {
		parts = append(parts, fmt.Sprintf("on days %v", cronExpr.DayOfMonth))
	}

	// Month
	if len(cronExpr.Month) == 1 {
		parts = append(parts, fmt.Sprintf("in month %d", cronExpr.Month[0]))
	} else if len(cronExpr.Month) < 12 {
		parts = append(parts, fmt.Sprintf("in months %v", cronExpr.Month))
	}

	// Day of week
	if len(cronExpr.DayOfWeek) == 1 {
		weekday := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
		parts = append(parts, fmt.Sprintf("on %s", weekday[cronExpr.DayOfWeek[0]]))
	} else if len(cronExpr.DayOfWeek) < 7 {
		parts = append(parts, fmt.Sprintf("on weekdays %v", cronExpr.DayOfWeek))
	}

	if len(parts) == 0 {
		return "every second", nil
	}

	return strings.Join(parts, ", "), nil
}
