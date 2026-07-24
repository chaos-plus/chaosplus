package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/chaos-plus/chaosplus/internal/app"
	"github.com/chaos-plus/chaosplus/internal/deployment"
	"github.com/chaos-plus/chaosplus/pkg/configurator"
	"github.com/spf13/cobra"
)

var (
	f = configurator.New()
	c = &app.Config{}

	genOutput    string
	validateFile string
)

// configCmd groups config-file utilities. Its subcommands never load the server's
// runtime config, so they work before any config file exists.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Config file utilities (generate, validate)",
}

var migrationCmd = &cobra.Command{
	Use:   "migration",
	Short: "Manage embedded Goose migrations",
}

var migrationUpCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply all pending embedded migrations",
	RunE: func(_ *cobra.Command, _ []string) error {
		return deployment.Migrate(context.Background(), *c)
	},
}

var migrationDownCmd = &cobra.Command{
	Use:   "down <module>",
	Short: "Roll back the latest migration for dlock, wuid, or iam",
	Args:  cobra.ExactArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		return deployment.Rollback(context.Background(), *c, args[0], nil)
	},
}

var migrationDownToCmd = &cobra.Command{
	Use:   "down-to <module> <version>",
	Short: "Roll a module back to an embedded Goose version",
	Args:  cobra.ExactArgs(2),
	RunE: func(_ *cobra.Command, args []string) error {
		version, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || version < 0 {
			return fmt.Errorf("invalid migration version %q", args[1])
		}
		return deployment.Rollback(context.Background(), *c, args[0], &version)
	},
}

// configGenerateCmd writes a YAML config template built from the Config struct
// (defaults + descriptions). It can never drift from the schema.
var configGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a config file template from the schema",
	RunE: func(cmd *cobra.Command, _ []string) error {
		data, err := configurator.GenerateYAML(&app.Config{})
		if err != nil {
			return err
		}
		if err := os.WriteFile(genOutput, data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", genOutput)
		return nil
	},
}

// configValidateCmd loads a config file strictly (unknown keys are errors) to
// catch typos and stale keys before the server is started with it.
var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a config file against the schema",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if err := configurator.LoadStrict(validateFile, &app.Config{}); err != nil {
			return fmt.Errorf("%s is invalid: %w", validateFile, err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s is valid\n", validateFile)
		return nil
	},
}

var rootCmd = &cobra.Command{
	Use:   "chaosplus",
	Short: "ChaosPlus Server",
	Long:  `ChaosPlus Server`,
	// On a RunE error (e.g. config validate), print just the error, not the usage.
	SilenceUsage: true,
	Run: func(cmd *cobra.Command, args []string) {
		log.Printf("%+v\n", c)
		ctx := context.Background()
		if c.Migrations.Auto {
			if err := deployment.Migrate(ctx, *c); err != nil {
				log.Fatalf("database migration failed: %v", err)
			}
		}
		if c.Bootstrap.Auto {
			if err := deployment.Provision(ctx, *c); err != nil {
				log.Fatalf("deployment provisioning failed: %v", err)
			}
		}
		runtimeConfig := *c
		// The release migration used the privileged migration datasource above.
		// Runtime modules must only use the low-privilege application datasource.
		runtimeConfig.Migrations.Auto = false
		app := app.NewApp(runtimeConfig)
		err := app.Run()
		if err != nil {
			log.Fatalf("Failed to run app: %v", err)
		}
	},
}

func main() {
	Execute()
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	configGenerateCmd.Flags().StringVarP(&genOutput, "output", "o", "config.yaml", "output file path or name")
	configValidateCmd.Flags().StringVarP(&validateFile, "config", "c", "config.yaml", "config file to validate")
	configCmd.AddCommand(configGenerateCmd, configValidateCmd)
	migrationCmd.AddCommand(migrationUpCmd, migrationDownCmd, migrationDownToCmd)
	rootCmd.AddCommand(configCmd, migrationCmd)

	// The `config` subcommands do not need (and their flags would break) the
	// server's struct-driven flag parsing, so skip it for them.
	if len(os.Args) > 1 && os.Args[1] == "config" {
		return
	}
	if preset := os.Getenv(app.ConfigPresetEnv); preset != "" {
		data, err := app.ConfigPreset(preset)
		if err != nil {
			panic(err)
		}
		f.UseDefaultConfig(data, "yaml")
	}

	f.UseFlags(rootCmd.Flags())
	f.UseConfigFileArgDefault()
	if err := f.Parse(c); err != nil {
		panic(err)
	}
}
