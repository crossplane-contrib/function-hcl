package main

import (
	"fmt"
	"os"

	"github.com/crossplane-contrib/function-hcl/function/internal/composition"
	"github.com/crossplane-contrib/function-hcl/function/internal/format"
	"github.com/spf13/cobra"
)

func getDir(args []string) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("zero or exactly one argument expected, found %d", len(args))
	}
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}
	return dir, nil
}

func analyzeCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "analyze [dir]",
		Short: "perform a static analysis of the supplied directory (default is current directory)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := getDir(args)
			if err != nil {
				return err
			}
			cmd.SilenceUsage = true
			return composition.Analyze(dir)
		},
	}
	return c
}

func packageScriptCommand() *cobra.Command {
	var skipAnalysis bool
	c := &cobra.Command{
		Use:   "package [dir]",
		Short: "generate a txtar script for the supplied directory (default is current directory)",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := getDir(args)
			if err != nil {
				return err
			}
			cmd.SilenceUsage = true
			b, err := composition.Package(dir, skipAnalysis)
			if err != nil {
				return err
			}
			_, _ = os.Stdout.Write(b)
			return nil
		},
	}
	f := c.Flags()
	f.BoolVar(&skipAnalysis, "skip-analysis", false, "skip analysis of files before packaging")
	return c
}

func formatCommand() *cobra.Command {
	fc := format.FormatCmd{
		Check:     false,
		Recursive: true,
		Opts: format.Options{
			StandardizeObjectLiterals: true,
		},
	}
	c := &cobra.Command{
		Use:   "fmt file1.hcl file2.hcl dir/ ...",
		Short: "format HCL files",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			return fc.Execute(args)
		},
	}
	f := c.Flags()
	f.BoolVar(&fc.Opts.StandardizeObjectLiterals, "normalize-literals", fc.Opts.StandardizeObjectLiterals, "normalize object literals to always use key = value syntax")
	f.BoolVarP(&fc.Check, "check", "c", fc.Check, "check if files are formatted, log names of unformatted files and exit appropriately")
	f.BoolVarP(&fc.Recursive, "recursive", "r", fc.Recursive, "recursively process directories")
	return c
}
