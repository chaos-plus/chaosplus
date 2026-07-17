package main

import (
	"context"
	"log"

	"github.com/chaos-plus/chaosplus/internal/app"
	"github.com/chaos-plus/chaosplus/internal/deployment"
	"github.com/chaos-plus/chaosplus/pkg/configurator"
)

func main() {
	flags := configurator.New()
	cfg := app.Config{}
	flags.UseConfigFileArgDefault()
	if err := flags.Parse(&cfg); err != nil {
		log.Fatalf("load bootstrap config: %v", err)
	}
	if err := deployment.Run(context.Background(), cfg); err != nil {
		log.Fatalf("bootstrap failed: %v", err)
	}
}
