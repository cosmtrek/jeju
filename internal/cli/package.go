package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cosmtrek/jeju/internal/agentpkg"
	"github.com/spf13/cobra"
)

func newPackageCommand(ctx context.Context) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "package",
		Short:        "Manage distributable agent packages",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newPackageInitCommand())
	cmd.AddCommand(newPackageValidateCommand())
	cmd.AddCommand(newPackagePackCommand())
	cmd.AddCommand(newPackageAddCommand(ctx))
	cmd.AddCommand(newPackageUpdateCommand(ctx))
	cmd.AddCommand(newPackageListCommand())
	cmd.AddCommand(newPackageInspectCommand())
	cmd.AddCommand(newPackageRemoveCommand())
	return cmd
}

func newPackageInitCommand() *cobra.Command {
	var agent string
	var id string
	var version string
	var name string
	var description string
	cmd := &cobra.Command{
		Use:          "init <package-root> --agent <agent.yaml> --id <namespace/name> --version <semver>",
		Short:        "Create jeju.package.yaml for an existing agent",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			pkg, err := agentpkg.Init(args[0], agentpkg.InitOptions{
				Agent:       agent,
				ID:          id,
				Version:     version,
				Name:        name,
				Description: description,
			})
			if err != nil {
				return err
			}
			fmt.Printf("created package manifest %s\n", pkg.ManifestPath)
			printPackageWarnings(pkg.Warnings)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "agent.yaml", "relative path to the agent manifest")
	cmd.Flags().StringVar(&id, "id", "", "package id in namespace/name form")
	cmd.Flags().StringVar(&version, "version", "", "semantic package version")
	cmd.Flags().StringVar(&name, "name", "", "human-readable package name")
	cmd.Flags().StringVar(&description, "description", "", "short package description")
	return cmd
}

func newPackageValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "validate <package-root>",
		Short:        "Validate an agent package root",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			pkg, err := agentpkg.Validate(args[0], agentpkg.ValidateOptions{JejuVersion: version})
			if err != nil {
				return err
			}
			fmt.Printf("package ok %s@%s\n", pkg.Manifest.Metadata.ID, pkg.Manifest.Metadata.Version)
			fmt.Printf("agent %s\n", pkg.AgentManifestPath)
			printPackageWarnings(pkg.Warnings)
			return nil
		},
	}
}

func newPackagePackCommand() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:          "pack <package-root> --out <dir>",
		Short:        "Create a local .jpkg artifact",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := agentpkg.Pack(args[0], out, agentpkg.ValidateOptions{JejuVersion: version})
			if err != nil {
				return err
			}
			absPath, err := filepath.Abs(result.Path)
			if err == nil {
				result.Path = absPath
			}
			fmt.Printf("packed %s@%s\n", result.ID, result.Version)
			fmt.Printf("artifact %s\n", result.Path)
			fmt.Printf("digest %s\n", result.Digest)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", ".", "output directory")
	return cmd
}

func newPackageAddCommand(ctx context.Context) *cobra.Command {
	var replace bool
	cmd := &cobra.Command{
		Use:          "add <source>",
		Short:        "Add a package source to the local package store",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := agentpkg.DefaultStore()
			if err != nil {
				return err
			}
			result, err := store.AddWithOptions(ctx, args[0], agentpkg.AddOptions{
				Activate:    true,
				JejuVersion: version,
				Replace:     replace,
			})
			if err != nil {
				return err
			}
			printAddResult("added", result)
			return nil
		},
	}
	cmd.Flags().BoolVar(&replace, "replace", false, "replace an installed package when the same version resolves to new content")
	return cmd
}

func newPackageUpdateCommand(ctx context.Context) *cobra.Command {
	var all bool
	var selectedVersion string
	var replace bool
	cmd := &cobra.Command{
		Use:          "update [package-id]",
		Short:        "Re-resolve saved package sources",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := agentpkg.DefaultStore()
			if err != nil {
				return err
			}
			if all {
				if len(args) != 0 {
					return fmt.Errorf("package update --all does not accept a package id")
				}
				results, err := store.UpdateAll(ctx, version, replace)
				if err != nil {
					return err
				}
				for _, result := range results {
					printAddResult("updated", result)
				}
				if len(results) == 0 {
					fmt.Println("no active packages")
				}
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("package update requires a package id or --all")
			}
			result, err := store.Update(ctx, args[0], selectedVersion, version, replace)
			if err != nil {
				return err
			}
			printAddResult("updated", result)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "update all active local package refs")
	cmd.Flags().StringVar(&selectedVersion, "version", "", "update a specific semantic version")
	cmd.Flags().BoolVar(&replace, "replace", false, "replace an installed package when the same version resolves to new content")
	return cmd
}

func newPackageListCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "ls",
		Short:        "List local package refs",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := agentpkg.DefaultStore()
			if err != nil {
				return err
			}
			items, err := store.List()
			if err != nil {
				return err
			}
			if len(items) == 0 {
				fmt.Println("no packages")
				return nil
			}
			fmt.Println("PACKAGE VERSION ACTIVE DIGEST SOURCE")
			for _, item := range items {
				active := "-"
				if item.Active {
					active = "*"
				}
				fmt.Printf("%s %s %s %s %s\n", item.ID, item.Version, active, item.Digest, item.Source)
			}
			return nil
		},
	}
}

func newPackageInspectCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "inspect <package-id[@version]>",
		Short:        "Show package metadata and resolved source",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := agentpkg.DefaultStore()
			if err != nil {
				return err
			}
			result, err := store.Inspect(args[0], version)
			if err != nil {
				return err
			}
			fmt.Printf("id: %s\n", result.ID)
			fmt.Printf("version: %s\n", result.Version)
			fmt.Printf("active: %t\n", result.Active)
			fmt.Printf("digest: %s\n", result.Digest)
			fmt.Printf("store: %s\n", result.StorePath)
			fmt.Printf("agent: %s\n", result.Manifest.Agent.Manifest)
			if result.Source != "" {
				fmt.Printf("source: %s\n", result.Source)
			}
			fmt.Println("risk:")
			fmt.Printf("  level: %s\n", result.Risk.Level)
			fmt.Printf("  access: %s\n", result.Risk.Access)
			fmt.Printf("  approval: %s\n", result.Risk.Approval)
			if len(result.Risk.Capabilities) > 0 {
				fmt.Printf("  capabilities: %s\n", strings.Join(result.Risk.Capabilities, ","))
			}
			if resolved := result.Resolved.Map(); len(resolved) > 0 {
				fmt.Println("resolved:")
				for _, line := range sortedMapLines(resolved) {
					fmt.Printf("  %s\n", line)
				}
			}
			printPackageWarnings(result.Warnings)
			return nil
		},
	}
}

func newPackageRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:          "rm <package-id[@version]>",
		Short:        "Remove local package refs",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := agentpkg.DefaultStore()
			if err != nil {
				return err
			}
			if err := store.Remove(args[0]); err != nil {
				return err
			}
			fmt.Printf("removed %s\n", args[0])
			return nil
		},
	}
}

func printAddResult(verb string, result agentpkg.AddResult) {
	fmt.Printf("%s %s@%s\n", verb, result.ID, result.Version)
	fmt.Printf("digest %s\n", result.Digest)
	fmt.Printf("store %s\n", result.StorePath)
	if result.Source != "" {
		fmt.Printf("source %s\n", result.Source)
	}
	printPackageWarnings(result.Warnings)
}

func printPackageWarnings(warnings []string) {
	for _, warning := range warnings {
		fmt.Printf("warning: %s\n", warning)
	}
}

func sortedMapLines(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sortStrings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s: %v", key, values[key]))
	}
	return lines
}

func sortStrings(values []string) {
	if len(values) < 2 {
		return
	}
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && strings.Compare(values[j-1], values[j]) > 0; j-- {
			values[j-1], values[j] = values[j], values[j-1]
		}
	}
}
