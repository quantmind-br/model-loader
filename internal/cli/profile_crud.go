package cli

import (
	"fmt"
	"io"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/spf13/cobra"
)

func init() {
	profileCmd.AddCommand(&cobra.Command{
		Use:   "delete <id|name>",
		Short: "Delete a profile",
		Args:  cobra.ExactArgs(1),
		RunE: profileMutationRunE(func(out io.Writer, s profilestore.Store, dir string, args []string) error {
			return deleteProfile(out, s, args[0])
		}),
	})
	profileCmd.AddCommand(&cobra.Command{
		Use:   "duplicate <id|name> [newid]",
		Short: "Duplicate a profile",
		Args:  cobra.RangeArgs(1, 2),
		RunE: profileMutationRunE(func(out io.Writer, s profilestore.Store, dir string, args []string) error {
			newID := ""
			if len(args) == 2 {
				newID = args[1]
			}
			return duplicateProfile(out, s, args[0], newID)
		}),
	})
	profileCmd.AddCommand(&cobra.Command{
		Use:   "rename <id|name> <newname>",
		Short: "Rename a profile's display name",
		Args:  cobra.ExactArgs(2),
		RunE: profileMutationRunE(func(out io.Writer, s profilestore.Store, dir string, args []string) error {
			return renameProfile(out, s, dir, args[0], args[1])
		}),
	})
	profileCmd.AddCommand(&cobra.Command{
		Use:   "pin <id|name>",
		Short: "Pin a profile",
		Args:  cobra.ExactArgs(1),
		RunE: profileMutationRunE(func(out io.Writer, s profilestore.Store, dir string, args []string) error {
			return setPinned(out, s, dir, args[0], true)
		}),
	})
	profileCmd.AddCommand(&cobra.Command{
		Use:   "unpin <id|name>",
		Short: "Unpin a profile",
		Args:  cobra.ExactArgs(1),
		RunE: profileMutationRunE(func(out io.Writer, s profilestore.Store, dir string, args []string) error {
			return setPinned(out, s, dir, args[0], false)
		}),
	})
}

// profileMutationRunE wires Bootstrap + error→ExitError for a mutation closure.
func profileMutationRunE(fn func(out io.Writer, s profilestore.Store, dir string, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		svc, err := app.Bootstrap(logLevel)
		if err != nil {
			return &ExitError{Code: 1}
		}
		defer svc.Close()
		if err := fn(cmd.OutOrStdout(), svc.Store, svc.Cfg.Paths.ProfilesDir, args); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			return &ExitError{Code: 1}
		}
		return nil
	}
}

func deleteProfile(out io.Writer, s profilestore.Store, ref string) error {
	p, err := resolveProfileRef(s, ref)
	if err != nil {
		return err
	}
	if err := s.Delete(p.ID); err != nil {
		return err
	}
	fmt.Fprintf(out, "deleted profile %s\n", p.ID)
	return nil
}

func duplicateProfile(out io.Writer, s profilestore.Store, ref, newID string) error {
	p, err := resolveProfileRef(s, ref)
	if err != nil {
		return err
	}
	if newID == "" {
		newID = p.ID + "-copy"
	}
	dup, err := s.Duplicate(p.ID, newID)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "duplicated %s as %s\n", p.ID, dup.ID)
	return nil
}

func renameProfile(out io.Writer, s profilestore.Store, dir, ref, newName string) error {
	p, err := resolveProfileRef(s, ref)
	if err != nil {
		return err
	}
	err = withProfileLock(dir, p.ID, func() error {
		p.Name = newName
		return s.Save(p)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "renamed %s to %q\n", p.ID, newName)
	return nil
}

func setPinned(out io.Writer, s profilestore.Store, dir, ref string, pinned bool) error {
	p, err := resolveProfileRef(s, ref)
	if err != nil {
		return err
	}
	err = withProfileLock(dir, p.ID, func() error {
		p.Pinned = pinned
		return s.Save(p)
	})
	if err != nil {
		return err
	}
	verb := "pinned"
	if !pinned {
		verb = "unpinned"
	}
	fmt.Fprintf(out, "%s %s\n", verb, p.ID)
	return nil
}
