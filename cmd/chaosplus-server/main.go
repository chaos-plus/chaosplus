package main

import (
	"fmt"
	"log"
	"os"

	"github.com/chaos-plus/chaosplus/internal/app"
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
		app := app.NewApp(*c)
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
	rootCmd.AddCommand(configCmd)

	// The `config` subcommands do not need (and their flags would break) the
	// server's struct-driven flag parsing, so skip it for them.
	if len(os.Args) > 1 && os.Args[1] == "config" {
		return
	}

	f.UseFlags(rootCmd.Flags())
	f.UseConfigFileArgDefault()
	if err := f.Parse(c); err != nil {
		panic(err)
	}
}
