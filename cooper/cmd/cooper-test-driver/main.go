package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/rickchristie/govner/cooper/internal/testdocker"
	"github.com/rickchristie/govner/cooper/internal/testdriver"
)

func main() {
	var (
		scenario             string
		imagePrefix          string
		keepArtifacts        bool
		disableHostClipboard bool
		timeout              time.Duration
	)

	flag.StringVar(&scenario, "scenario", "clipboard-smoke", "Scenario to run")
	flag.StringVar(&imagePrefix, "prefix", testdriver.DefaultImagePrefix, "Docker image/container prefix")
	flag.BoolVar(&keepArtifacts, "keep", false, "Keep the temporary cooper directory after exit")
	flag.BoolVar(&disableHostClipboard, "disable-host-clipboard", true, "Disable host clipboard prerequisite checks")
	flag.DurationVar(&timeout, "timeout", 2*time.Minute, "Overall scenario timeout")
	flag.Parse()

	if imagePrefix == testdriver.DefaultImagePrefix {
		if err := testdocker.EnsureTestImages(); err != nil {
			fmt.Fprintf(os.Stderr, "build shared test images: %v\n", err)
			os.Exit(1)
		}
	}

	driver, err := testdriver.New(testdriver.Options{
		ImagePrefix:          imagePrefix,
		DisableHostClipboard: disableHostClipboard,
		KeepArtifactsOnClose: keepArtifacts,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create test driver: %v\n", err)
		os.Exit(1)
	}
	defer driver.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	switch scenario {
	case "clipboard-smoke":
		err = testdriver.RunClipboardSmoke(ctx, driver)
	default:
		err = fmt.Errorf("unknown scenario %q", scenario)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "scenario %s failed: %v\n", scenario, err)
		fmt.Fprintf(os.Stderr, "cooper dir: %s\n", driver.CooperDir())
		if closeErr := driver.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "cleanup error: %v\n", closeErr)
		}
		os.Exit(1)
	}

	if closeErr := driver.Close(); closeErr != nil {
		fmt.Fprintf(os.Stderr, "cleanup failed: %v\n", closeErr)
		os.Exit(1)
	}

	fmt.Printf("scenario %s passed\n", scenario)
	if keepArtifacts {
		fmt.Printf("cooper dir: %s\n", driver.CooperDir())
	}
}
