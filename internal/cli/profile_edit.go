package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/service/validator"
	"github.com/spf13/cobra"
)

// profileInput holds the parsed flag values for create/edit. The set* booleans
// distinguish "flag provided" from "zero value", so edit only overrides what
// the user passed.
type profileInput struct {
	id, name, model, backend, description string
	args                                  map[string]string
	extraArgs                             []string
	env                                   []domain.EnvVar
	setName, setModel, setBackend, setDesc bool
}

func init() {
	profileCmd.AddCommand(newProfileWriteCmd("create", "Create a new profile"))
	profileCmd.AddCommand(newProfileWriteCmd("edit", "Edit an existing profile"))
}

func newProfileWriteCmd(verb, short string) *cobra.Command {
	var (
		file      string
		name      string
		model     string
		backend   string
		desc      string
		idFlag    string
		argPairs  []string
		extraArgs []string
		envPairs  []string
	)
	use := verb
	args := cobra.NoArgs
	if verb == "edit" {
		use = "edit <id|name>"
		args = cobra.ExactArgs(1)
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE: func(cmd *cobra.Command, posArgs []string) error {
			argMap, err := parseKVPairs(argPairs)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return &ExitError{Code: 1}
			}
			envMap, err := parseKVPairs(envPairs)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return &ExitError{Code: 1}
			}
			in := profileInput{
				id:          idFlag,
				name:        name,
				model:       model,
				backend:     backend,
				description: desc,
				args:        argMap,
				extraArgs:   extraArgs,
				env:         kvToEnv(envMap),
				setName:     cmd.Flags().Changed("name"),
				setModel:    cmd.Flags().Changed("model"),
				setBackend:  cmd.Flags().Changed("backend"),
				setDesc:     cmd.Flags().Changed("description"),
			}

			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()

			isEdit := verb == "edit"
			ref := ""
			if isEdit {
				ref = posArgs[0]
			}
			code := runProfileWrite(cmd.OutOrStdout(), cmd.ErrOrStderr(), profileWriteDeps{
				store:    svc.Store,
				resolver: svc.Resolver,
				val:      svc.Val,
				dir:      svc.Cfg.Paths.ProfilesDir,
			}, isEdit, ref, file, in)
			if code != 0 {
				return &ExitError{Code: code}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "read base profile JSON from a file, or '-' for stdin")
	cmd.Flags().StringVar(&name, "name", "", "profile display name")
	cmd.Flags().StringVar(&model, "model", "", "model path / repo")
	cmd.Flags().StringVar(&backend, "backend", "", "backend id (e.g. llama-server, vllm)")
	cmd.Flags().StringVar(&desc, "description", "", "profile description")
	cmd.Flags().StringArrayVar(&argPairs, "arg", nil, "backend arg as key=value (repeatable)")
	cmd.Flags().StringArrayVar(&extraArgs, "extra-arg", nil, "raw extra CLI arg (repeatable)")
	cmd.Flags().StringArrayVar(&envPairs, "env", nil, "launch env var as KEY=value (repeatable)")
	if verb == "create" {
		cmd.Flags().StringVar(&idFlag, "id", "", "explicit profile id (default: slug of name)")
	}
	return cmd
}

type profileWriteDeps struct {
	store    profilestore.Store
	resolver backendcatalog.Resolver
	val      validator.Validator
	dir      string
}

// runProfileWrite returns a process exit code (0 ok, 1 error, 2 validation).
func runProfileWrite(out, errw io.Writer, deps profileWriteDeps, isEdit bool, ref, file string, in profileInput) int {
	// 1. Establish the base profile.
	var base domain.Profile
	if isEdit {
		p, err := resolveProfileRef(deps.store, ref)
		if err != nil {
			fmt.Fprintln(errw, err)
			return 1
		}
		base = p
	}
	// 2. Overlay --file/stdin JSON onto the base.
	if file != "" {
		raw, err := readInput(file)
		if err != nil {
			fmt.Fprintf(errw, "read profile input: %v\n", err)
			return 1
		}
		if err := json.Unmarshal(raw, &base); err != nil {
			fmt.Fprintf(errw, "parse profile JSON: %v\n", err)
			return 1
		}
	}
	// 3. Compute id for create.
	if !isEdit {
		base.ID = in.id
		if base.ID == "" {
			nm := in.name
			if nm == "" {
				nm = base.Name
			}
			base.ID = domain.Slugify(nm)
		}
		if base.ID == "" {
			fmt.Fprintln(errw, "create: a profile needs --name or --id")
			return 1
		}
	}
	// 4. Resolve schema for arg coercion + validation.
	schema, kind := resolveSchema(deps.resolver, base)
	// 5. Apply flag overrides.
	final := assembleProfile(base, in, schema)
	// 6. Validate (skip when deps.val is nil).
	if deps.val != nil {
		report := deps.val.Validate(final, schema, kind)
		for _, w := range report.Warnings {
			fmt.Fprintf(errw, "warning: %s: %s\n", w.Field, w.Message)
		}
		if report.HasBlockingErrors() {
			for _, e := range report.Errors {
				fmt.Fprintf(errw, "error: %s: %s\n", e.Field, e.Message)
			}
			return 2
		}
	}
	// 7. Persist (edit serializes the RMW; create is a fresh file).
	persist := func() error {
		if isEdit {
			return deps.store.Save(final)
		}
		return deps.store.Create(final)
	}
	if isEdit {
		if err := withProfileLock(deps.dir, final.ID, persist); err != nil {
			fmt.Fprintf(errw, "save profile: %v\n", err)
			return 1
		}
	} else if err := persist(); err != nil {
		if errors.Is(err, profilestore.ErrDuplicateID) {
			fmt.Fprintf(errw, "profile id already exists: %s\n", final.ID)
			return 1
		}
		fmt.Fprintf(errw, "create profile: %v\n", err)
		return 1
	}
	if jsonOut {
		_ = emitJSON(out, final)
	} else {
		v := "created"
		if isEdit {
			v = "updated"
		}
		fmt.Fprintf(out, "%s profile %s\n", v, final.ID)
	}
	return 0
}

func resolveSchema(resolver backendcatalog.Resolver, p domain.Profile) (domain.FlagSchema, domain.BackendKind) {
	if resolver == nil {
		return domain.FlagSchema{}, ""
	}
	rb, err := resolver.Resolve(p)
	if err != nil {
		return domain.FlagSchema{}, ""
	}
	return rb.Schema.ToFlagSchema(), rb.Backend.Kind
}

// assembleProfile overlays the user's flag input onto base, coercing args by schema.
func assembleProfile(base domain.Profile, in profileInput, schema domain.FlagSchema) domain.Profile {
	if in.setName {
		base.Name = in.name
	}
	if in.setModel {
		base.Model = in.model
	}
	if in.setBackend {
		base.Launch.BackendID = in.backend
	}
	if in.setDesc {
		base.Description = in.description
	}
	if len(in.args) > 0 {
		if base.Args == nil {
			base.Args = map[string]any{}
		}
		for k, v := range coerceArgs(in.args, schema) {
			base.Args[k] = v
		}
	}
	if base.Args == nil {
		base.Args = map[string]any{}
	}
	if len(in.extraArgs) > 0 {
		base.ExtraArgs = in.extraArgs
	}
	if len(in.env) > 0 {
		base.Launch.Env = in.env
	}
	if base.Meta.CreatedAt.IsZero() {
		base.Meta.CreatedAt = time.Now().UTC()
	}
	base.Meta.UpdatedAt = time.Now().UTC()
	if base.SchemaVersion == 0 {
		base.SchemaVersion = domain.SchemaVersion
	}
	return base
}

// coerceArgs converts string flag values to typed values by schema FlagType,
// mirroring configweb's draft.coerceArgs but for CLI string inputs.
func coerceArgs(in map[string]string, schema domain.FlagSchema) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		spec, ok := schema.Lookup(domain.CanonicalFlag(k))
		if !ok {
			out[k] = v
			continue
		}
		switch spec.Type {
		case domain.FlagTypeInt:
			if n, err := strconv.Atoi(v); err == nil {
				out[k] = n
				continue
			}
			out[k] = v
		case domain.FlagTypeFloat:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				out[k] = f
				continue
			}
			out[k] = v
		case domain.FlagTypeBool:
			out[k] = v == "on" || v == "true" || v == "1"
		default:
			out[k] = v
		}
	}
	return out
}

func parseKVPairs(pairs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range pairs {
		idx := strings.IndexByte(p, '=')
		if idx < 0 {
			return nil, fmt.Errorf("expected key=value, got %q", p)
		}
		out[p[:idx]] = p[idx+1:]
	}
	return out, nil
}

func kvToEnv(m map[string]string) []domain.EnvVar {
	if len(m) == 0 {
		return nil
	}
	out := make([]domain.EnvVar, 0, len(m))
	for k, v := range m {
		out = append(out, domain.EnvVar{Key: k, Value: v})
	}
	return out
}

// readInput reads from a file path, or stdin when path is "-".
func readInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}
