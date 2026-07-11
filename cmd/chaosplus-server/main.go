package main

import (
	"log"
	"os"

	"github.com/chaos-plus/chaosplus/internal/app"
	"github.com/chaos-plus/chaosplus/pkg/configurator"
	"github.com/spf13/cobra"
)

var (
	f = configurator.New()
	c = &app.Config{}
)

var rootCmd = &cobra.Command{
	Use:   "chaosplus",
	Short: "ChaosPlus Server",
	Long:  `ChaosPlus Server`,
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
	f.UseFlags(rootCmd.Flags())
	f.UseConfigFileArgDefault()
	err := f.Parse(c)
	if err != nil {
		panic(err)
	}
}
