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
		Long: "Copy the complete host state for this harness. The first profile is Default.\nLater saves use the detected account mapping. A new name can only create a new\nprofile for an unmapped account; it can never replace another named account."}
	command.Flags().String("name", "", "New unused profile name for an unmapped account")
	command.Flags().String("conflict", "", "Resolve different changes with host or saved; retain recovery copies")
	command.RunE = func(cmd *cobra.Command, args []string) error {
		service, err := hostProfileService()
		if err != nil {
			return err
		}
		name, _ := cmd.Flags().GetString("name")
		choice, _ := cmd.Flags().GetString("conflict")
		result, err := service.Save(cmd.Context(), profiles.SaveRequest{Harness: args[0], NewName: name, ConflictChoice: profiles.ConflictChoice(choice)})
		if needsName(err) && interactiveInput(cmd) {
			name, err = promptProfileName(cmd)
			if err == nil {
				result, err = service.Save(cmd.Context(), profiles.SaveRequest{Harness: args[0], NewName: name, ConflictChoice: profiles.ConflictChoice(choice)})
			}
		}
		if needsName(err) {
			return fmt.Errorf("%w; use --name <new-unused-name>", err)
		}
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
		Long: "Save the current account before replacing host state with a saved profile.\nA missing profile starts with empty state. Log in on the host, then run\n'cooper save <harness>' to bind that new account to the pending profile."}
	command.Flags().String("name", "", "New unused name for the outgoing account if it is unmapped")
	command.Flags().String("conflict", "", "Resolve outgoing changes with host or saved; retain recovery copies")
	command.RunE = func(cmd *cobra.Command, args []string) error {
		service, err := hostProfileService()
		if err != nil {
			return err
		}
		name, _ := cmd.Flags().GetString("name")
		choice, _ := cmd.Flags().GetString("conflict")
		request := profiles.LoadRequest{Harness: args[0], Name: args[1], NewName: name, ConflictChoice: profiles.ConflictChoice(choice)}
		result, err := service.Load(cmd.Context(), request)
		if needsName(err) && interactiveInput(cmd) {
			request.NewName, err = promptProfileName(cmd)
			if err == nil {
				result, err = service.Load(cmd.Context(), request)
			}
		}
		if needsName(err) {
			return fmt.Errorf("%w; use --name <new-unused-name> for the outgoing account", err)
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
			status := "saved"
			if item.Loaded {
				status = "loaded on host"
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
	command.AddCommand(remove)
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

func needsName(err error) bool {
	var issue *profiles.Issue
	return errors.As(err, &issue) && issue.Kind == profiles.NameRequired
}

func interactiveInput(cmd *cobra.Command) bool {
	file, ok := cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

func promptProfileName(cmd *cobra.Command) (string, error) {
	fmt.Fprint(cmd.OutOrStdout(), "New profile name for the current account: ")
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(line)
	return name, profiles.ValidateName(name)
}

func printProfileResult(cmd *cobra.Command, harness string, result profiles.Result) {
	if result.Saved != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Saved %s/%s.\n", harness, result.Saved)
	}
	if result.Loaded != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Loaded %s/%s on the host.\n", harness, result.Loaded)
	}
	if result.Pending {
		fmt.Fprintf(cmd.OutOrStdout(), "Log in with %s on the host, then run cooper save %s.\n", harness, harness)
	}
	if result.Recovery != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Previous host state: %s\n", result.Recovery)
	}
	if result.Warning != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), result.Warning)
	}
}
