package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cosmtrek/jeju/internal/agentpkg"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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
	var pathOnly bool
	var showAgent bool
	cmd := &cobra.Command{
		Use:          "inspect <package-id[@version]>",
		Short:        "Inspect an installed package",
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
			if pathOnly {
				fmt.Println(result.StorePath)
				return nil
			}
			var agentContent string
			if showAgent {
				data, err := os.ReadFile(result.AgentManifestPath)
				if err != nil {
					return fmt.Errorf("read agent manifest: %w", err)
				}
				agentContent = string(data)
			}
			output := newPackageInspectOutput(result, agentContent)
			data, err := formatPackageInspectOutput(output)
			if err != nil {
				return fmt.Errorf("format package inspection: %w", err)
			}
			fmt.Print(data)
			return nil
		},
	}
	cmd.Flags().BoolVar(&pathOnly, "path", false, "print only the installed package directory")
	cmd.Flags().BoolVar(&showAgent, "show-agent", false, "include the agent manifest content")
	cmd.MarkFlagsMutuallyExclusive("path", "show-agent")
	return cmd
}

type packageInspectOutput struct {
	Package       agentpkg.Metadata             `yaml:"package"`
	Compatibility *agentpkg.CompatibilityConfig `yaml:"compatibility,omitempty"`
	Agent         packageInspectAgent           `yaml:"agent"`
	Installation  packageInspectInstallation    `yaml:"installation"`
	Risk          packageInspectRisk            `yaml:"risk"`
	Source        *packageInspectSource         `yaml:"source,omitempty"`
	Warnings      []string                      `yaml:"warnings,omitempty"`
}

type packageInspectAgent struct {
	Manifest string `yaml:"manifest"`
	Content  string `yaml:"content,omitempty"`
}

type packageInspectInstallation struct {
	Active bool   `yaml:"active"`
	Path   string `yaml:"path"`
	Digest string `yaml:"digest"`
}

type packageInspectRisk struct {
	Level        string   `yaml:"level"`
	Access       string   `yaml:"access"`
	Approval     string   `yaml:"approval"`
	Capabilities []string `yaml:"capabilities,omitempty"`
}

type packageInspectSource struct {
	Requested string `yaml:"requested,omitempty"`
	Canonical string `yaml:"canonical,omitempty"`
	Type      string `yaml:"type,omitempty"`
	URL       string `yaml:"url,omitempty"`
	Ref       string `yaml:"ref,omitempty"`
	Commit    string `yaml:"commit,omitempty"`
	Subdir    string `yaml:"subdir,omitempty"`
	Registry  string `yaml:"registry,omitempty"`
	Unstable  bool   `yaml:"unstable,omitempty"`
}

func newPackageInspectOutput(result agentpkg.InspectResult, agentContent string) packageInspectOutput {
	var compatibility *agentpkg.CompatibilityConfig
	if result.Manifest.Compatibility.Jeju != "" {
		value := result.Manifest.Compatibility
		compatibility = &value
	}
	var source *packageInspectSource
	if result.Source != "" || len(result.Resolved.Map()) > 0 {
		source = &packageInspectSource{
			Requested: result.Source,
			Canonical: result.Resolved.CanonicalSource,
			Type:      result.Resolved.Type,
			URL:       result.Resolved.URL,
			Ref:       result.Resolved.Ref,
			Commit:    result.Resolved.Commit,
			Subdir:    result.Resolved.Subdir,
			Registry:  result.Resolved.Registry,
			Unstable:  result.Resolved.Unstable,
		}
	}
	return packageInspectOutput{
		Package:       result.Manifest.Metadata,
		Compatibility: compatibility,
		Agent: packageInspectAgent{
			Manifest: result.Manifest.Agent.Manifest,
			Content:  agentContent,
		},
		Installation: packageInspectInstallation{
			Active: result.Active,
			Path:   result.StorePath,
			Digest: result.Digest,
		},
		Risk: packageInspectRisk{
			Level:        result.Risk.Level,
			Access:       result.Risk.Access,
			Approval:     result.Risk.Approval,
			Capabilities: result.Risk.Capabilities,
		},
		Source:   source,
		Warnings: result.Warnings,
	}
}

func formatPackageInspectOutput(output packageInspectOutput) (string, error) {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(output); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}
	return buffer.String(), nil
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
