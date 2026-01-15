package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

type TestEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Elapsed float64 `json:"Elapsed"`
}

func main() {
	testPath := flag.String("path", "./extensions/...", "Test path pattern")
	timeout := flag.String("timeout", "10m", "Test timeout")
	race := flag.Bool("race", true, "Enable race detector")

	flag.Parse()

	// Build test command
	args := []string{"test", "-json"}
	if *race {
		args = append(args, "-race")
	}

	args = append(args, "-timeout="+*timeout, *testPath)

	fmt.Fprintf(os.Stdout, "Running: go %s\n\n", strings.Join(args, " "))

	cmd := exec.CommandContext(context.Background(), "go", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating pipe: %v\n", err)
		os.Exit(1)
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting tests: %v\n", err)
		os.Exit(1)
	}

	// Parse JSON output
	failedPackages := make(map[string][]string)
	scanner := bufio.NewScanner(stdout)

	for scanner.Scan() {
		var event TestEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		if event.Action == "fail" && event.Package != "" {
			if event.Test != "" {
				failedPackages[event.Package] = append(failedPackages[event.Package], event.Test)
			} else {
				// Package-level failure
				if _, exists := failedPackages[event.Package]; !exists {
					failedPackages[event.Package] = []string{}
				}
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		if len(failedPackages) == 0 {
			fmt.Fprintf(os.Stderr, "Tests failed but couldn't parse failures: %v\n", err)
			os.Exit(1)
		}

		// Sort packages for consistent output
		packages := make([]string, 0, len(failedPackages))
		for pkg := range failedPackages {
			packages = append(packages, pkg)
		}

		sort.Strings(packages)

		fmt.Fprintln(os.Stdout, "\033[0;31m✗ Tests failed!\033[0m")
		fmt.Fprintln(os.Stdout, "\033[0;31mFailing packages:\033[0m")

		for i, pkg := range packages {
			fmt.Fprintf(os.Stdout, "  %d. %s\n", i+1, pkg)
		}

		fmt.Fprintf(os.Stdout, "\n\033[1;33mTotal: %d failing package(s)\033[0m\n", len(packages))

		// Show failed tests per package
		if len(packages) > 0 {
			fmt.Fprintln(os.Stdout, "\n\033[0;34mFailed tests per package:\033[0m")

			for _, pkg := range packages {
				tests := failedPackages[pkg]
				if len(tests) > 0 {
					fmt.Fprintf(os.Stdout, "\n\033[1;33m%s:\033[0m\n", pkg)
					sort.Strings(tests)

					for _, test := range tests {
						fmt.Fprintf(os.Stdout, "    - %s\n", test)
					}
				}
			}
		}

		os.Exit(1)
	}

	fmt.Fprintln(os.Stdout, "\033[0;32m✓ All tests passed!\033[0m")
}
