package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/rickchristie/govner/cooper/internal/profilemanager"
	"github.com/rickchristie/govner/cooper/internal/profiles"
)

func init() {
	rootCmd.AddCommand(newSaveCommand(), newLoadCommand(), newProfilesCommand())
}

func newSaveCommand() *cobra.Command {
	command := &cobra.Command{Use: "save <harness>", Short: "Save host state to the profile for the current account", Args: cobra.ExactArgs(1),
		Long: "Register or check the selected Linux account profile without copying history.\nThe first profile is Default. Load a new empty profile before changing accounts.\nSave is not a backup. Use profiles backup for an independent copy."}
	command.RunE = func(cmd *cobra.Command, args []string) error {
		service, err := hostProfileService()
		if err != nil {
			return err
		}
		result, err := service.Save(cmd.Context(), profiles.SaveRequest{Harness: args[0]})
		if err != nil {
			return err
		}
		printProfileResult(cmd, args[0], result)
		return nil
	}
	return command
}

func newLoadCommand() *cobra.Command {
	command := &cobra.Command{Use: "load <harness> <profile>", Short: "Save the current account and load another profile on the host", Args: cobra.ExactArgs(2),
		Long: "Select another Linux profile with checked sibling directory renames.\nA missing profile starts with empty state. Load it before changing accounts,\nlog in on the host, then run 'cooper save <harness>' to bind the account.\nClose affected CLI instances and apps first. Confirmation cannot override detected use."}
	command.Flags().Bool("yes", false, "Confirm that affected CLI instances, apps, and Cooper runtimes are stopped")
	command.RunE = func(cmd *cobra.Command, args []string) error {
		service, err := hostProfileService()
		if err != nil {
			return err
		}
		yes, _ := cmd.Flags().GetBool("yes")
		request := profiles.LoadRequest{Harness: args[0], Name: args[1], Confirmed: yes}
		result, err := service.Load(cmd.Context(), request)
		for needsConfirmation(err) {
			var issue *profiles.Issue
			errors.As(err, &issue)
			accepted, promptErr := confirmProfileAction(cmd, issue.Message)
			if promptErr != nil {
				return promptErr
			}
			if !accepted {
				return nil
			}
			request.Confirmed, request.ExpectedProfileID = true, issue.ProfileID
			result, err = service.Load(cmd.Context(), request)
		}
		if err != nil {
			return err
		}
		printProfileResult(cmd, args[0], result)
		return nil
	}
	return command
}

func newProfilesCommand() *cobra.Command {
	command := &cobra.Command{Use: "profiles", Short: "List and manage saved accounts", Args: cobra.NoArgs}
	command.RunE = func(cmd *cobra.Command, args []string) error {
		service, err := hostProfileService()
		if err != nil {
			return err
		}
		items, err := service.List(cmd.Context())
		if err != nil {
			return err
		}
		if len(items) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No profiles. Log in on the host, then run cooper save <harness>.")
			return nil
		}
		writer := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
		fmt.Fprintln(writer, "HARNESS\tPROFILE\tACCOUNT\tSTATE")
		for _, item := range items {
			status := "inactive"
			if item.Loaded {
				status = "loaded on host"
			}
			if item.Mismatch {
				status += ", account mismatch"
			}
			if item.Pending {
				status += ", login needed"
			}
			if item.InUse {
				status += ", in use"
			}
			fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", item.Harness, item.Name, item.Account, status)
		}
		return writer.Flush()
	}
	remove := &cobra.Command{Use: "delete <harness> <profile>", Short: "Delete an unused saved profile", Args: cobra.ExactArgs(2)}
	remove.Flags().Bool("yes", false, "Confirm deletion of this saved profile")
	remove.RunE = func(cmd *cobra.Command, args []string) error {
		service, err := hostProfileService()
		if err != nil {
			return err
		}
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			if !interactiveInput(cmd) {
				return errors.New("deletion requires --yes or an interactive confirmation")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Delete saved profile %s/%s? Type its name: ", args[0], args[1])
			line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if err != nil {
				return err
			}
			if strings.TrimSpace(line) != args[1] {
				fmt.Fprintln(cmd.OutOrStdout(), "Deletion canceled.")
				return nil
			}
		}
		if err := service.Delete(cmd.Context(), args[0], args[1]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Deleted %s/%s.\n", args[0], args[1])
		return nil
	}
	command.AddCommand(remove, newProfileRecoverCommand(), newProfileBackupCommand(), newProfilePruneCommand(), newProfileRestoreCommand())
	return command
}

func hostProfileService() (*profiles.Service, error) {
	if err := profilemanager.CheckHost(); err != nil {
		return nil, err
	}
	cooperDir, err := resolveCooperDir()
	if err != nil {
		return nil, err
	}
	workspace, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return profilemanager.New(cooperDir, workspace, home)
}

func selectLaunchProfile(ctx context.Context, cooperDir, workspace, home, harness string, args []string) (profiles.Selection, error) {
	if len(args) == 0 {
		return profilemanager.SelectID(ctx, cooperDir, workspace, home, harness, "")
	}
	service, err := profilemanager.New(cooperDir, workspace, home)
	if err != nil {
		return profiles.Selection{}, err
	}
	return service.Select(ctx, harness, args[0])
}

func interactiveInput(cmd *cobra.Command) bool {
	file, ok := cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func printProfileResult(cmd *cobra.Command, harness string, result profiles.Result) {
	if result.Saved != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Checked live profile %s/%s. Save does not make a backup.\n", harness, result.Saved)
	}
	if result.Loaded != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Loaded %s/%s on the host.\n", harness, result.Loaded)
	}
	if result.Pending {
		fmt.Fprintf(cmd.OutOrStdout(), "Log in with %s on the host, then run cooper save %s.\n", harness, harness)
	}
	if result.Recovery != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Recovery record: %s\n", result.Recovery)
	}
	if result.Warning != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), result.Warning)
	}
}

func needsConfirmation(err error) bool {
	var issue *profiles.Issue
	return errors.As(err, &issue) && issue.Kind == profiles.ConfirmationRequired
}

func confirmProfileAction(cmd *cobra.Command, message string) (bool, error) {
	if !interactiveInput(cmd) {
		return false, fmt.Errorf("%s Use --yes after closing the affected apps", message)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s\nContinue? [y/N]: ", message)
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil {
		return false, err
	}
	if !strings.EqualFold(strings.TrimSpace(line), "y") {
		fmt.Fprintln(cmd.OutOrStdout(), "Canceled. No profile was changed.")
		return false, nil
	}
	return true, nil
}
