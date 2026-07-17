package deployment

import (
	"context"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/app"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	zapp "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/app"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/management"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/project"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/user"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestProvisionerCreatesAndReconcilesResources(t *testing.T) {
	fake := &fakeManagement{}
	p := &zitadelProvisioner{api: fake, cfg: app.BootstrapZitadel{
		ProjectName: "Chaosplus API", ApplicationName: "Admin", RedirectURIs: []string{"https://app/callback"},
		PostLogoutURIs: []string{"https://app"},
	}}
	resources, err := p.EnsureResources(context.Background())
	require.NoError(t, err)
	assert.Equal(t, authn.RuntimeResources{Version: 1, ProjectID: "project-1", ClientID: "client-1"}, resources)
	assert.Equal(t, 1, fake.addProjectCalls)
	assert.Equal(t, 1, fake.addAppCalls)

	resources, err = p.EnsureResources(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "client-1", resources.ClientID)
	assert.Zero(t, fake.updateCalls)

	fake.apps[0].GetOidcConfig().RedirectUris = []string{"https://old/callback"}
	fake.updateErr = status.Error(codes.FailedPrecondition, "No changes")
	_, err = p.EnsureResources(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, fake.updateCalls)
}

func TestProvisionerFindsHumanAndRejectsDrift(t *testing.T) {
	fake := &fakeManagement{
		projects: []*project.Project{{Id: "p", Name: "Chaosplus API"}},
		apps:     []*zapp.App{{Id: "bad", Name: "Admin", Config: &zapp.App_ApiConfig{}}},
		users:    []*user.User{{Id: "subject", PreferredLoginName: "admin@example.com", Type: &user.User_Human{Human: &user.Human{Profile: &user.Profile{DisplayName: "Admin"}, Email: &user.Email{Email: "admin@example.com"}}}}},
	}
	p := &zitadelProvisioner{api: fake, cfg: app.BootstrapZitadel{ProjectName: "Chaosplus API", ApplicationName: "Admin", RedirectURIs: []string{"https://app/callback"}, PostLogoutURIs: []string{"https://app"}}}
	_, err := p.EnsureResources(context.Background())
	assert.ErrorContains(t, err, "not OIDC")
	admin, err := p.FindHuman(context.Background(), "admin@example.com")
	require.NoError(t, err)
	assert.Equal(t, identity{Subject: "subject", DisplayName: "Admin", Email: "admin@example.com"}, admin)

	fake.users = append(fake.users, fake.users[0])
	_, err = p.FindHuman(context.Background(), "admin@example.com")
	assert.ErrorContains(t, err, "expected one")
}

func TestProvisionerRejectsDuplicates(t *testing.T) {
	p := &zitadelProvisioner{api: &fakeManagement{projects: []*project.Project{{Id: "1"}, {Id: "2"}}}, cfg: app.BootstrapZitadel{ProjectName: "same"}}
	_, err := p.ensureProject(context.Background())
	assert.ErrorContains(t, err, "multiple")
}

type fakeManagement struct {
	projects        []*project.Project
	apps            []*zapp.App
	users           []*user.User
	addProjectCalls int
	addAppCalls     int
	updateCalls     int
	updateErr       error
}

func (f *fakeManagement) ListProjects(context.Context, *management.ListProjectsRequest, ...grpc.CallOption) (*management.ListProjectsResponse, error) {
	return &management.ListProjectsResponse{Result: f.projects}, nil
}
func (f *fakeManagement) AddProject(_ context.Context, req *management.AddProjectRequest, _ ...grpc.CallOption) (*management.AddProjectResponse, error) {
	f.addProjectCalls++
	f.projects = []*project.Project{{Id: "project-1", Name: req.Name}}
	return &management.AddProjectResponse{Id: "project-1"}, nil
}
func (f *fakeManagement) ListApps(context.Context, *management.ListAppsRequest, ...grpc.CallOption) (*management.ListAppsResponse, error) {
	return &management.ListAppsResponse{Result: f.apps}, nil
}
func (f *fakeManagement) AddOIDCApp(_ context.Context, req *management.AddOIDCAppRequest, _ ...grpc.CallOption) (*management.AddOIDCAppResponse, error) {
	f.addAppCalls++
	f.apps = []*zapp.App{{Id: "app-1", Name: req.Name, Config: &zapp.App_OidcConfig{OidcConfig: &zapp.OIDCConfig{
		ClientId: "client-1", RedirectUris: req.RedirectUris, ResponseTypes: req.ResponseTypes, GrantTypes: req.GrantTypes,
		AppType: req.AppType, AuthMethodType: req.AuthMethodType, PostLogoutRedirectUris: req.PostLogoutRedirectUris,
		DevMode: req.DevMode, AccessTokenType: req.AccessTokenType, SkipNativeAppSuccessPage: req.SkipNativeAppSuccessPage,
	}}}}
	return &management.AddOIDCAppResponse{AppId: "app-1", ClientId: "client-1"}, nil
}
func (f *fakeManagement) UpdateOIDCAppConfig(context.Context, *management.UpdateOIDCAppConfigRequest, ...grpc.CallOption) (*management.UpdateOIDCAppConfigResponse, error) {
	f.updateCalls++
	return &management.UpdateOIDCAppConfigResponse{}, f.updateErr
}
func (f *fakeManagement) ListUsers(context.Context, *management.ListUsersRequest, ...grpc.CallOption) (*management.ListUsersResponse, error) {
	return &management.ListUsersResponse{Result: f.users}, nil
}
