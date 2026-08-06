package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/chaos-plus/chaosplus/internal/app"
	"github.com/chaos-plus/chaosplus/internal/deployment"
	"github.com/chaos-plus/chaosplus/pkg/configurator"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func main() {
	if err := Execute(os.Args[1:]...); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func Execute(args ...string) error {
	root, err := newRootCommand(args)
	if err != nil {
		return err
	}
	return root.Execute()
}

func newRootCommand(args []string) (*cobra.Command, error) {
	cfg := &app.Config{}
	root := &cobra.Command{
		Use:          "chaosplus",
		Short:        "ChaosPlus Server",
		Long:         "ChaosPlus Server",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runServer(*cfg)
		},
	}

	configCommand := newConfigCommand()
	migrationCommand := newMigrationCommand(cfg)
	root.AddCommand(configCommand, migrationCommand)

	if !isConfigCommand(args) {
		flagger := configurator.New()
		flagger.UseFlags(root.PersistentFlags())
		flagger.UseConfigFileArgDefault()
		parseArgs := args
		if len(parseArgs) == 0 {
			parseArgs = []string{"--"}
		}
		if err := flagger.Parse(cfg, parseArgs...); err != nil && !errors.Is(err, pflag.ErrHelp) {
			return nil, err
		}
	}
	root.SetArgs(args)
	return root, nil
}

func isConfigCommand(args []string) bool {
	return len(args) > 0 && args[0] == "config"
}

func newConfigCommand() *cobra.Command {
	var genOutput string
	var validateFile string

	command := &cobra.Command{
		Use:   "config",
		Short: "Config file utilities (generate, validate)",
	}
	generate := &cobra.Command{
		Use:   "generate",
		Short: "Generate a config file template from the schema",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := configurator.GenerateYAML(&app.Config{})
			if err != nil {
				return err
			}
			if err := os.WriteFile(genOutput, data, 0o600); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", genOutput)
			return err
		},
	}
	validate := &cobra.Command{
		Use:   "validate",
		Short: "Validate a config file against the schema",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := configurator.LoadStrict(validateFile, &app.Config{}); err != nil {
				return fmt.Errorf("%s is invalid: %w", validateFile, err)
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s is valid\n", validateFile)
			return err
		},
	}
	generate.Flags().StringVarP(&genOutput, "output", "o", "config.yaml", "output file path or name")
	validate.Flags().StringVarP(&validateFile, "config", "c", "config.yaml", "config file to validate")
	command.AddCommand(generate, validate)
	return command
}

func newMigrationCommand(cfg *app.Config) *cobra.Command {
	command := &cobra.Command{
		Use:   "migration",
		Short: "Manage embedded Goose migrations",
	}
	up := &cobra.Command{
		Use:   "up",
		Short: "Apply all pending embedded migrations",
		RunE: func(_ *cobra.Command, _ []string) error {
			return deployment.Migrate(context.Background(), *cfg)
		},
	}
	down := &cobra.Command{
		Use:   "down <module>",
		Short: "Roll back the latest migration for dlock, wuid, iam, organization, provisioning, governance, or federation",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return deployment.Rollback(context.Background(), *cfg, args[0], nil)
		},
	}
	downTo := &cobra.Command{
		Use:   "down-to <module> <version>",
		Short: "Roll a module back to an embedded Goose version",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			version, err := strconv.ParseInt(args[1], 10, 64)
			if err != nil || version < 0 {
				return fmt.Errorf("invalid migration version %q", args[1])
			}
			return deployment.Rollback(context.Background(), *cfg, args[0], &version)
		},
	}
	command.AddCommand(up, down, downTo)
	return command
}

func runServer(cfg app.Config) error {
	ctx := context.Background()
	if cfg.Migrations.Auto {
		if err := deployment.Migrate(ctx, cfg); err != nil {
			return fmt.Errorf("database migration failed: %w", err)
		}
	}
	if cfg.Bootstrap.Auto {
		if err := deployment.Provision(ctx, cfg); err != nil {
			return fmt.Errorf("deployment provisioning failed: %w", err)
		}
	}
	runtimeConfig := cfg
	// A release migration may use a privileged datasource; runtime modules only
	// receive the low-privilege application datasource.
	runtimeConfig.Migrations.Auto = false
	application := app.NewApp(runtimeConfig)
	if err := application.Run(); err != nil {
		return fmt.Errorf("run app: %w", err)
	}
	return nil
}
