package main

import (
	"errors"
	"fmt"

	"github.com/rickchristie/govner/cooper/internal/profiles"
	"github.com/spf13/cobra"
)

func newProfileMigrateCommand() *cobra.Command {
	command := &cobra.Command{Use: "migrate", Short: "Convert saved profiles to live directories and host links", Args: cobra.NoArgs,
		Long: "Linux cooper build converts this complete profile store automatically.\nUse this command to preview the conversion or resolve a conflict.\nStop host agents and Cooper sessions before conversion. It makes one verified copy\nand retains original data for recovery. Future load operations switch directory\nlinks. Host writes then change profiles immediately. Use --dry-run to inspect it."}
	command.Flags().Bool("dry-run", false, "Check state and print the migration without changing it")
	command.Flags().String("conflict", "", "Choose host or saved when both copies changed")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		service, err := hostProfileService()
		if err != nil {
			return err
		}
		dry, _ := cmd.Flags().GetBool("dry-run")
		choice, _ := cmd.Flags().GetString("conflict")
		if dry {
			preview, err := service.PreviewMigration(cmd.Context(), profiles.ConflictChoice(choice))
			if err != nil {
				return err
			}
			if preview.Managed {
				fmt.Fprintln(cmd.OutOrStdout(), "Profiles already use live directories.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Profile file bytes to copy: %d (plus metadata and file allocation).\n", preview.Bytes)
			for _, root := range preview.Roots {
				fmt.Fprintf(cmd.OutOrStdout(), "%s/%s: %s\n  %s\n", root.Harness, root.Profile, root.Path, root.Method)
			}
			for _, warning := range preview.Warnings {
				fmt.Fprintln(cmd.OutOrStdout(), warning)
			}
			return nil
		}
		result, err := service.Migrate(cmd.Context(), profiles.ConflictChoice(choice))
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Profiles now use live directories. Load a new profile before changing accounts.")
		printProfileResult(cmd, "", result)
		return nil
	}
	return command
}

func newProfileRecoverCommand() *cobra.Command {
	return &cobra.Command{Use: "recover", Short: "Finish or undo an interrupted profile operation", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		service, err := hostProfileService()
		if err != nil {
			return err
		}
		if err := service.Recover(cmd.Context()); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Profile recovery is complete.")
		return nil
	}}
}

func newProfileBackupCommand() *cobra.Command {
	return &cobra.Command{Use: "backup <new-absolute-directory>", Short: "Copy all live profiles to an independent restore store", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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

func newProfileDetachCommand() *cobra.Command {
	return &cobra.Command{Use: "detach", Short: "Restore ordinary host directories for recovery or export",
		Long: "Restore ordinary host directories and independent profile copies.\nUse this for recovery or export. The next Linux cooper build converts this store\nto live directory profiles again.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			service, err := hostProfileService()
			if err != nil {
				return err
			}
			result, err := service.Detach(cmd.Context())
			if err != nil {
				return err
			}
			printProfileResult(cmd, "", result)
			return nil
		}}
}

func newProfilePruneCommand() *cobra.Command {
	command := &cobra.Command{Use: "prune-recovery", Short: "Remove retained managed recovery data after a backup", Args: cobra.NoArgs}
	command.Flags().Bool("yes", false, "Delete retained originals and old profile generations")
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
		fmt.Fprintln(cmd.OutOrStdout(), "Retained managed recovery data removed. Current profile state is unchanged.")
		return nil
	}
	return command
}

func newProfileRestoreCommand() *cobra.Command {
	return &cobra.Command{Use: "restore <harness> <profile> <backup-directory>", Short: "Restore one live profile from its independent backup", Args: cobra.ExactArgs(3), RunE: func(cmd *cobra.Command, args []string) error {
		service, err := hostProfileService()
		if err != nil {
			return err
		}
		result, err := service.Restore(cmd.Context(), args[0], args[1], args[2])
		if err != nil {
			return err
		}
		printProfileResult(cmd, args[0], result)
		return nil
	}}
}
