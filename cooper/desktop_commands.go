package main

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

func showDesktopViewer(cmd *cobra.Command, viewerURL, stopHint string) {
	fmt.Fprintf(cmd.OutOrStdout(), "Desktop: %s\nClosing the viewer keeps the desktop running. %s\n", viewerURL, stopHint)
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	opener := exec.Command(name, viewerURL)
	if err := opener.Start(); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "Open the desktop link in your browser.")
		return
	}
	go opener.Wait()
}
