package main

import (
	"errors"
	"fmt"

	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/spf13/cobra"
)

func newProfileRecoverCommand() *cobra.Command {
	command := &cobra.Command{Use: "recover", Short: "Finish or undo an interrupted profile operation", Args: cobra.NoArgs}
	command.Flags().Bool("yes", false, "Confirm that affected CLI instances, apps, and Cooper runtimes are stopped")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		service, err := hostProfileService()
		if err != nil {
			return err
		}
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			accepted, err := confirmProfileAction(cmd, "Close all affected CLI instances and apps. Stop affected Cooper runtimes. Keep them closed until recovery completes.")
			if err != nil || !accepted {
				return err
			}
		}
		if err := service.Recover(cmd.Context()); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Profile recovery is complete.")
		return nil
	}
	return command
}

func newProfileBackupCommand() *cobra.Command {
	return &cobra.Command{Use: "backup <new-absolute-directory>", Short: "Copy all profiles to an independent backup", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		service, err := hostProfileService()
		if err != nil {
			return err
		}
		if err := service.Backup(cmd.Context(), args[0]); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Independent profile backup: %s\n", args[0])
		return nil
	}}
}

func newProfilePruneCommand() *cobra.Command {
	command := &cobra.Command{Use: "prune-recovery", Short: "Remove retained restore and deletion data", Args: cobra.NoArgs}
	command.Flags().Bool("yes", false, "Confirm permanent deletion of retained recovery data")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		yes, _ := cmd.Flags().GetBool("yes")
		if !yes {
			return errors.New("recovery deletion requires --yes; make an independent backup first")
		}
		service, err := hostProfileService()
		if err != nil {
			return err
		}
		if err := service.PruneRecovery(cmd.Context()); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Retained recovery data removed permanently. Current profile state is unchanged.")
		return nil
	}
	return command
}

func newProfileRestoreCommand() *cobra.Command {
	command := &cobra.Command{Use: "restore <harness> <profile> <backup-directory>", Short: "Restore one profile from its independent backup", Args: cobra.ExactArgs(3)}
	command.Flags().Bool("yes", false, "Confirm that affected CLI instances, apps, and Cooper runtimes are stopped")
	command.RunE = func(cmd *cobra.Command, args []string) error {
		service, err := hostProfileService()
		if err != nil {
			return err
		}
		yes, _ := cmd.Flags().GetBool("yes")
		request := profiles.RestoreRequest{Harness: args[0], Name: args[1], Backup: args[2], Confirmed: yes}
		result, err := service.Restore(cmd.Context(), request)
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
			result, err = service.Restore(cmd.Context(), request)
		}
		if err != nil {
			return err
		}
		printProfileResult(cmd, args[0], result)
		return nil
	}
	return command
}
