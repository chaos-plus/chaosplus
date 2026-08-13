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
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authz"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
	"github.com/uptrace/bun"
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
	// A runner registers over the machine WS bridge; surface it to the run
	// gateway so run launch can discover and dispatch to connected daemons,
	// and drop it on disconnect so zombies are never dispatch targets.
	hub.SetRunnerMarker(client.Gateway().MarkRunner, client.Gateway().UnmarkRunner)
	machineModule := machine.NewModule(dependencies.Writer, hub, dependencies.Authorization)
	agentModule := agent.NewModule(dependencies.Writer, dependencies.NextID, dependencies.Authorization)
	machineModule.SetAgentDirectory(agentModule.Directory())
	channelModule := channel.NewModule(dependencies.Writer, dependencies.NextID, dependencies.Authorization, agentModule.Service())
	link := &workflow.NatsRunnerLink{G: client.Gateway()}
	artifactModule := artifact.NewModule(dependencies.Writer, dependencies.NextID, link, artifactScopeDirectory{dependencies.Writer}, config.Reconcile.Interval, dependencies.Authorization)
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
	// Reject dispatching a run onto a machine another tenant owns (PRD P10).
	workflowModule.Manager().SetRunnerScope(hub.RunnerScope)

	workspaceModules, err := workspace.NewModules(workspace.Dependencies{
		Database:         dependencies.Writer,
		NextID:           dependencies.NextID,
		Authorization:    dependencies.Authorization,
		WorkflowLauncher: workspace.NewWorkflowLauncher(workflowModule.Repository(), workflowModule.Manager()),
		Blobs:            blobs,
		ConversationExists: func(ctx context.Context, id guid.ID) error {
			_, err := channelModule.Service().Get(ctx, id)
			if errors.Is(err, channel.ErrNotFound) {
				return attachment.ErrNotFound
			}
			return err
		},
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

// artifactScopeDirectory enumerates the tenant/entity scopes that own
// artifacts so background reconciliation (PRD §12) can run without a request
// context. It reads distinct artifact scopes; the reconcile pass itself checks
// per-artifact existence/checksum.
type artifactScopeDirectory struct {
	db *bun.DB
}

var _ artifact.ScopeDirectory = artifactScopeDirectory{}

func (d artifactScopeDirectory) ActiveArtifactScopes(ctx context.Context) ([]authn.Claims, error) {
	rows := make([]struct {
		TenantID guid.ID `bun:"tenant_id"`
		EntityID guid.ID `bun:"entity_id"`
	}, 0)
	if err := d.db.NewSelect().Table("artifacts").
		Column("tenant_id", "entity_id").
		Group("tenant_id", "entity_id").
		Scan(ctx, &rows); err != nil {
		return nil, fmt.Errorf("enumerate artifact scopes: %w", err)
	}
	out := make([]authn.Claims, 0, len(rows))
	for _, r := range rows {
		if r.TenantID.Zero() || r.EntityID.Zero() {
			continue
		}
		// The reconciliation pass is a trusted, tenant/entity-scoped system
		// scan; it needs a non-zero principal only to satisfy the repository's
		// claim guard. A fixed sentinel keeps it from being a real user.
		out = append(out, authn.Claims{TenantID: r.TenantID, EntityID: r.EntityID, PrincipalID: 1})
	}
	return out, nil
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
