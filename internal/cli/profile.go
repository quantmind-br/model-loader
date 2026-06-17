package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/spf13/cobra"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage llama.cpp profiles",
}

func init() {
	rootCmd.AddCommand(profileCmd)

	profileCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()
			if err := listProfiles(cmd.OutOrStdout(), svc.Store, jsonOut); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return &ExitError{Code: 1}
			}
			return nil
		},
	})

	profileCmd.AddCommand(&cobra.Command{
		Use:   "show <id|name>",
		Short: "Print a profile's canonical JSON / details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()
			if err := showProfile(cmd.OutOrStdout(), svc.Store, args[0], jsonOut); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return &ExitError{Code: 1}
			}
			return nil
		},
	})
}

// resolveProfileRef looks up a profile by id, then falls back to a
// case-insensitive Name match (exact, else unique prefix). Ambiguous name
// matches are an error.
func resolveProfileRef(store profilestore.Store, ref string) (domain.Profile, error) {
	if p, err := store.Get(ref); err == nil {
		return p, nil
	} else if !errors.Is(err, profilestore.ErrNotFound) {
		return domain.Profile{}, err
	}
	all, err := store.List()
	if err != nil {
		return domain.Profile{}, err
	}
	p, err := resolveByPrefix(all, ref, func(p domain.Profile) []string { return []string{p.Name} })
	if err != nil {
		var amb *ambiguousMatchError[domain.Profile]
		if errors.As(err, &amb) {
			ids := make([]string, len(amb.Matches))
			for i, m := range amb.Matches {
				ids[i] = m.ID
			}
			return domain.Profile{}, fmt.Errorf("ambiguous profile name %q matches %d profiles: %s; use the id", ref, len(amb.Matches), formatCandidates(ids))
		}
		return domain.Profile{}, fmt.Errorf("profile not found: %s", ref)
	}
	return p, nil
}

func listProfiles(w io.Writer, store profilestore.Store, asJSON bool) error {
	profiles, err := store.List()
	if err != nil {
		return err
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	if asJSON {
		if profiles == nil {
			profiles = []domain.Profile{}
		}
		return emitJSON(w, profiles)
	}
	rows := make([][]string, 0, len(profiles))
	for _, p := range profiles {
		pin := ""
		if p.Pinned {
			pin = "★"
		}
		rows = append(rows, []string{p.ID, clip(p.Name, 32), clip(p.Model, 40), dashOr(p.Launch.BackendID), pin})
	}
	printTable(w, []string{"ID", "NAME", "MODEL", "BACKEND", "PIN"}, rows)
	if len(rows) == 0 {
		fmt.Fprintln(w, "(no profiles)")
	}
	return nil
}

func showProfile(w io.Writer, store profilestore.Store, ref string, asJSON bool) error {
	p, err := resolveProfileRef(store, ref)
	if err != nil {
		return err
	}
	if asJSON {
		return emitJSON(w, p)
	}
	fmt.Fprintf(w, "ID:          %s\n", p.ID)
	fmt.Fprintf(w, "Name:        %s\n", p.Name)
	fmt.Fprintf(w, "Model:       %s\n", dashOr(p.Model))
	fmt.Fprintf(w, "Backend:     %s\n", dashOr(p.Launch.BackendID))
	fmt.Fprintf(w, "Pinned:      %t\n", p.Pinned)
	if p.Description != "" {
		fmt.Fprintf(w, "Description: %s\n", p.Description)
	}
	fmt.Fprintln(w, "Args:")
	keys := make([]string, 0, len(p.Args))
	for k := range p.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "  %s = %v\n", k, p.Args[k])
	}
	if len(p.ExtraArgs) > 0 {
		fmt.Fprintf(w, "ExtraArgs:   %s\n", strings.Join(p.ExtraArgs, " "))
	}
	return nil
}
