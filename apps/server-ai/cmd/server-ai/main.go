package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	serverai "github.com/chaos-plus/chaosplus/apps/server-ai/internal/app"
	sharedapp "github.com/chaos-plus/chaosplus/internal/app"
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
	config := new(serverai.Config)
	root := &cobra.Command{
		Use:          "server-ai",
		Short:        "AI resource server",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runServer(*config)
		},
	}
	root.AddCommand(newConfigCommand())
	if !isConfigCommand(args) {
		flagger := configurator.New()
		flagger.UseFlags(root.PersistentFlags())
		flagger.UseConfigFileArgDefault()
		parseArgs := args
		if len(parseArgs) == 0 {
			parseArgs = []string{"--"}
		}
		if err := flagger.Parse(config, parseArgs...); err != nil && !errors.Is(err, pflag.ErrHelp) {
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
	var output string
	var input string
	command := &cobra.Command{Use: "config", Short: "Configuration utilities"}
	generate := &cobra.Command{
		Use: "generate", Short: "Generate a configuration template",
		RunE: func(cmd *cobra.Command, _ []string) error {
			data, err := configurator.GenerateYAML(&serverai.Config{})
			if err != nil {
				return err
			}
			if err := os.WriteFile(output, data, 0o600); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", output)
			return err
		},
	}
	validate := &cobra.Command{
		Use: "validate", Short: "Validate a configuration file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := configurator.LoadStrict(input, &serverai.Config{}); err != nil {
				return fmt.Errorf("%s is invalid: %w", input, err)
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s is valid\n", input)
			return err
		},
	}
	generate.Flags().StringVarP(&output, "output", "o", "config.yaml", "output file")
	validate.Flags().StringVarP(&input, "config", "c", "config.yaml", "configuration file")
	command.AddCommand(generate, validate)
	return command
}

func runServer(config serverai.Config) error {
	application := sharedapp.NewResourceApp(config.Server, serverai.Extension(config))
	if err := application.Run(); err != nil {
		return fmt.Errorf("run AI resource server: %w", err)
	}
	return nil
}
