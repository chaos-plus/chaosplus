package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/infra/natsclient"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/artifact"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/conversation"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/conversation/agent"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/conversation/channel"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/conversation/message"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/machine"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workflow"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace"
	"github.com/chaos-plus/chaosplus/apps/server-ai/internal/modules/workspace/attachment"
	sharedapp "github.com/chaos-plus/chaosplus/internal/app"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

// Extension declares AI bounded contexts on the shared application host.
func Extension(config Config) sharedapp.Extension {
	return sharedapp.Extension{
		Name:         "ai-control-plane",
		Actions:      actions(),
		RegisterI18n: registerI18n,
		BuildModules: func(dependencies sharedapp.ModuleDependencies) ([]any, error) {
			return buildModules(config, dependencies)
		},
	}
}

func actions() []authz.Action {
	conversationActions := conversation.Actions()
	result := make([]authz.Action, 0, len(machine.Actions)+len(conversationActions)+len(artifact.Actions)+len(workflow.Actions)+len(workspace.Actions()))
	result = append(result, machine.Actions...)
	result = append(result, conversationActions...)
	result = append(result, artifact.Actions...)
	result = append(result, workflow.Actions...)
	result = append(result, workspace.Actions()...)
	return result
}

func registerI18n() error {
	return errors.Join(machine.RegisterI18n(), conversation.RegisterI18n(), artifact.RegisterI18n(), workflow.RegisterI18n(), workspace.RegisterI18n())
}

func buildModules(config Config, dependencies sharedapp.ModuleDependencies) ([]any, error) {
	blobs, err := workspaceBlobStore(config)
	if err != nil {
		return nil, err
	}
	client, err := natsclient.Open(config.NATS)
	if err != nil {
		return nil, err
	}

	hub := machine.NewHub(client.Bridge(), machine.NewTokenStore(), nil, dependencies.NextID, dependencies.OriginPolicy)
	machineModule := machine.NewModule(dependencies.Writer, hub, dependencies.Authorization)
	agentModule := agent.NewModule(dependencies.Writer, dependencies.NextID, dependencies.Authorization)
	machineModule.SetAgentDirectory(agentModule.Directory())
	channelModule := channel.NewModule(dependencies.Writer, dependencies.NextID, dependencies.Authorization, agentModule.Service())
	link := &workflow.NatsRunnerLink{G: client.Gateway()}
	artifactModule := artifact.NewModule(dependencies.Writer, dependencies.NextID, link, nil, config.Reconcile.Interval, dependencies.Authorization)
	workflowModule := workflow.NewModule(
		dependencies.Writer,
		client.Primary(),
		link,
		config.Runner.DefaultHandle,
		0,
		dependencies.NextID,
		dependencies.Authorization,
		dependencies.OriginPolicy,
		artifact.NewWorkflowProjector(dependencies.NextID),
	)
	workflowModule.Manager().SetMachinePicker(machinePicker(hub))

	workspaceModules, err := workspace.NewModules(workspace.Dependencies{
		Database:         dependencies.Writer,
		NextID:           dependencies.NextID,
		Authorization:    dependencies.Authorization,
		WorkflowLauncher: workspace.NewWorkflowLauncher(workflowModule.Repository(), workflowModule.Manager()),
		Blobs:            blobs,
	})
	if err != nil {
		_ = client.Stop(context.Background())
		return nil, err
	}

	messageModule := message.NewModule(dependencies.Writer, dependencies.NextID, dependencies.Authorization, dependencies.OriginPolicy,
		client.Primary(), channelModule.Service(), workspaceModules.Attachments.Service())
	modules := []any{client, machineModule, agentModule, channelModule, artifactModule, workflowModule}
	modules = append(modules, workspaceModules.Items...)
	modules = append(modules, messageModule)
	return modules, nil
}

func workspaceBlobStore(config Config) (*attachment.S3BlobStore, error) {
	store, err := attachment.NewS3BlobStore(config.Storage)
	if err != nil {
		return nil, fmt.Errorf("configure workspace object storage: %w", err)
	}
	return store, nil
}

func machinePicker(hub *machine.Hub) workflow.MachinePicker {
	return func(executorType string) string {
		for _, rawID := range hub.RegisteredRunners() {
			id, err := guid.Parse(rawID)
			if err != nil {
				continue
			}
			for _, runtime := range hub.MachineRuntimes(id) {
				if runtime == executorType {
					return rawID
				}
			}
		}
		return ""
	}
}
