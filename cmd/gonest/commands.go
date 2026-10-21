package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/omyo-group/gonest/internal/codegen"
)

// run executes the CLI with args, writing to stdout and stderr, resolving
// relative paths against workdir. It exists so tests can drive the CLI.
func run(args []string, stdout, stderr io.Writer, workdir string) error {
	cmd := newRootCmd(stdout, workdir)
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.Execute()
}

func newRootCmd(out io.Writer, workdir string) *cobra.Command {
	root := &cobra.Command{
		Use:   "gonest",
		Short: "Scaffold Gonest projects and components",
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.AddCommand(newNewCmd(out, workdir), newGenerateCmd(out, workdir), newInfoCmd(out, workdir))
	return root
}

func newNewCmd(out io.Writer, workdir string) *cobra.Command {
	var o codegen.ProjectOptions
	var force, dry bool
	cmd := &cobra.Command{
		Use:   "new <name>",
		Short: "Create a new Gonest project",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			o.Name, o.Root = args[0], workdir
			plan, err := codegen.NewProject(o)
			if err != nil {
				return err
			}
			return plan.Apply(codegen.WriteOptions{Force: force, DryRun: dry, Out: out})
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.Module, "module", "", "Go module path (default: the project name)")
	f.StringVar(&o.Dir, "dir", "", "target directory (default: ./<name>)")
	f.StringVar(&o.GonestReplace, "gonest-replace", "", "add a replace directive pointing github.com/omyo-group/gonest at this local path")
	f.BoolVar(&o.Minimal, "minimal", false, "skip config and logger module wiring (only the root gonest package is required)")
	f.BoolVar(&force, "force", false, "overwrite existing files")
	f.BoolVar(&dry, "dry-run", false, "print what would be created without writing")
	return cmd
}

func newGenerateCmd(out io.Writer, workdir string) *cobra.Command {
	var path string
	var flat, force, dry bool
	cmd := &cobra.Command{
		Use:     "generate <component> <name>",
		Aliases: []string{"g"},
		Short:   "Generate a component (module, controller, service, guard, interceptor, filter, pipe, dto)",
		Args:    cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			kind, err := codegen.ParseKind(args[0])
			if err != nil {
				return err
			}
			plan, err := codegen.Generate(codegen.GenerateOptions{Root: workdir, Kind: kind, Name: args[1], Path: path, Flat: flat})
			if err != nil {
				return err
			}
			return plan.Apply(codegen.WriteOptions{Force: force, DryRun: dry, Out: out})
		},
	}
	f := cmd.Flags()
	f.StringVar(&path, "path", "", "target directory (default: internal/<name>)")
	f.BoolVar(&flat, "flat", false, "do not create the internal/<name> folder; use --path or the current directory")
	f.BoolVar(&force, "force", false, "overwrite existing files")
	f.BoolVar(&dry, "dry-run", false, "print what would be created without writing")
	return cmd
}

func newInfoCmd(out io.Writer, workdir string) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Print Go, Gonest and Gin versions",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Fprint(out, codegen.Info(workdir))
			return err
		},
	}
}
