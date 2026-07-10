package main

import (
	"context"
	"fmt"

	"github.com/danielgtaylor/huma/v2"
	"gofr.dev/pkg/gofr"

	_ "github.com/danielgtaylor/huma/v2/formats/cbor"

	humagofr "github.com/chaos-plus/chaosplus/pkg/extension/huma/adapters"
	"github.com/chaos-plus/chaosplus/pkg/extension/huma/docs"
)

// defaultHTTPPort matches GoFr's own default when HTTP_PORT is not configured.
const defaultHTTPPort = "8000"

// GreetingOutput represents the greeting operation response.
type GreetingOutput struct {
	Body struct {
		Message string `json:"message" example:"Hello, world!" doc:"Greeting message"`
	}
}

func main() {
	// Create the GoFr application. GoFr owns the server lifecycle and provides
	// structured startup/request logging. Configure the port via the HTTP_PORT
	// env var or configs/.env (defaults to 8000).
	app := gofr.New()

	// Build the Huma config and let DOCS_RENDERER (env / configs/.env) pick the
	// docs UI at runtime without a rebuild.
	config, multiDocs := docs.Apply(app, huma.DefaultConfig("Chaosplus API", "1.0.0"))

	// Create the Huma API backed by the GoFr adapter. This registers the
	// bridging middleware and makes each Huma route participate in GoFr's
	// router, so routes appear in GoFr's startup log and access logs.
	api := humagofr.New(app, config)

	// When in multi mode, serve the tabbed page + each standalone renderer.
	if multiDocs {
		docs.Register(app, config)
	}

	// Register operations on the API.
	huma.Get(api, "/greeting/{name}", func(ctx context.Context, input *struct {
		Name string `path:"name" maxLength:"30" example:"world" doc:"Name to greet"`
	}) (*GreetingOutput, error) {
		resp := &GreetingOutput{}
		resp.Body.Message = fmt.Sprintf("Hello, %s!", input.Name)
		return resp, nil
	})

	app.OnStart(func(ctx *gofr.Context) error {
		// Print where to reach the service before handing control to GoFr's
		// blocking Run loop.
		port := app.Config.GetOrDefault("HTTP_PORT", defaultHTTPPort)
		base := fmt.Sprintf("http://localhost:%s", port)
		app.Logger().Infof("Chaosplus API listening on %s", base)
		switch {
		case multiDocs:
			app.Logger().Infof("API docs (tabs): %s/docs  →  scalar | swagger | redoc | stoplight | openapi-ui", base)
		case config.DocsPath != "":
			app.Logger().Infof("API docs (%s): %s%s", config.DocsRenderer, base, config.DocsPath)
		default:
			app.Logger().Infof("API docs: disabled (%s=none)", docs.RendererEnv)
		}
		app.Logger().Infof("OpenAPI:   %s/openapi.json", base)
		return nil
	})
	// Start all servers (blocks until interrupted).
	app.Run()
}
