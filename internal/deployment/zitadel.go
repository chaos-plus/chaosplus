package deployment

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/chaos-plus/chaosplus/internal/app"
	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	zclient "github.com/zitadel/zitadel-go/v3/pkg/client"
	zapp "github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/app"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/management"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/object"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/project"
	"github.com/zitadel/zitadel-go/v3/pkg/client/zitadel/user"
	"github.com/zitadel/zitadel-go/v3/pkg/zitadel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type managementAPI interface {
	ListProjects(context.Context, *management.ListProjectsRequest, ...grpc.CallOption) (*management.ListProjectsResponse, error)
	AddProject(context.Context, *management.AddProjectRequest, ...grpc.CallOption) (*management.AddProjectResponse, error)
	ListApps(context.Context, *management.ListAppsRequest, ...grpc.CallOption) (*management.ListAppsResponse, error)
	AddOIDCApp(context.Context, *management.AddOIDCAppRequest, ...grpc.CallOption) (*management.AddOIDCAppResponse, error)
	UpdateOIDCAppConfig(context.Context, *management.UpdateOIDCAppConfigRequest, ...grpc.CallOption) (*management.UpdateOIDCAppConfigResponse, error)
	ListUsers(context.Context, *management.ListUsersRequest, ...grpc.CallOption) (*management.ListUsersResponse, error)
}

type zitadelProvisioner struct {
	client *zclient.Client
	api    managementAPI
	cfg    app.BootstrapZitadel
}

func newZitadelProvisioner(ctx context.Context, issuer string, cfg app.BootstrapZitadel) (*zitadelProvisioner, error) {
	if cfg.MachineKeyFile == "" || cfg.ProjectName == "" || cfg.ApplicationName == "" || cfg.ResourcesOutputFile == "" {
		return nil, fmt.Errorf("Zitadel bootstrap key, project, application, and resources output are required")
	}
	if len(cfg.RedirectURIs) == 0 || len(cfg.PostLogoutURIs) == 0 {
		return nil, fmt.Errorf("Zitadel redirect and post-logout URI lists are required")
	}
	u, err := url.Parse(issuer)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" {
		return nil, fmt.Errorf("invalid public Zitadel issuer %q", issuer)
	}
	port := u.Port()
	providerOpts := make([]zitadel.Option, 0, 1)
	if u.Scheme == "http" {
		if port == "" {
			port = "80"
		}
		providerOpts = append(providerOpts, zitadel.WithInsecure(port))
	} else if port != "" {
		parsed, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid Zitadel issuer port: %w", err)
		}
		providerOpts = append(providerOpts, zitadel.WithPort(uint16(parsed)))
	}
	provider := zitadel.New(u.Hostname(), providerOpts...)
	auth := zclient.DefaultServiceUserAuthentication(cfg.MachineKeyFile, oidc.ScopeOpenID, zclient.ScopeZitadelAPI())
	client, err := zclient.New(ctx, provider, zclient.WithAuth(auth))
	if err != nil {
		return nil, fmt.Errorf("create Zitadel client: %w", err)
	}
	return &zitadelProvisioner{client: client, api: client.ManagementService(), cfg: cfg}, nil
}

func (p *zitadelProvisioner) Close() error {
	if p == nil || p.client == nil {
		return nil
	}
	return p.client.Close()
}

func (p *zitadelProvisioner) EnsureResources(ctx context.Context) (authn.RuntimeResources, error) {
	ctx, cancel := context.WithTimeout(ctx, durationOr(p.cfg.Timeout, 30*time.Second))
	defer cancel()
	projectID, err := p.ensureProject(ctx)
	if err != nil {
		return authn.RuntimeResources{}, err
	}
	clientID, err := p.ensureApplication(ctx, projectID)
	if err != nil {
		return authn.RuntimeResources{}, err
	}
	return authn.RuntimeResources{Version: authn.RuntimeResourcesVersion, ProjectID: projectID, ClientID: clientID}, nil
}

func (p *zitadelProvisioner) ensureProject(ctx context.Context) (string, error) {
	find := func() ([]*project.Project, error) {
		resp, err := retryRPC(ctx, func() (*management.ListProjectsResponse, error) {
			return p.api.ListProjects(ctx, &management.ListProjectsRequest{Queries: []*project.ProjectQuery{{Query: &project.ProjectQuery_NameQuery{NameQuery: &project.ProjectNameQuery{Name: p.cfg.ProjectName, Method: object.TextQueryMethod_TEXT_QUERY_METHOD_EQUALS}}}}})
		})
		if err != nil {
			return nil, err
		}
		return resp.Result, nil
	}
	projects, err := find()
	if err != nil {
		return "", err
	}
	if len(projects) > 1 {
		return "", fmt.Errorf("multiple Zitadel projects named %q", p.cfg.ProjectName)
	}
	if len(projects) == 1 {
		return projects[0].Id, nil
	}
	created, err := retryRPC(ctx, func() (*management.AddProjectResponse, error) {
		return p.api.AddProject(ctx, &management.AddProjectRequest{Name: p.cfg.ProjectName})
	})
	if err != nil {
		return "", err
	}
	if created.Id == "" {
		return "", fmt.Errorf("Zitadel returned an empty project ID")
	}
	if err := waitUntil(ctx, func() (bool, error) {
		visible, err := find()
		return err == nil && len(visible) == 1 && visible[0].Id == created.Id, err
	}); err != nil {
		return "", fmt.Errorf("wait for Zitadel project projection: %w", err)
	}
	return created.Id, nil
}

func (p *zitadelProvisioner) ensureApplication(ctx context.Context, projectID string) (string, error) {
	resp, err := retryRPC(ctx, func() (*management.ListAppsResponse, error) {
		return p.api.ListApps(ctx, &management.ListAppsRequest{ProjectId: projectID, Queries: []*zapp.AppQuery{{Query: &zapp.AppQuery_NameQuery{NameQuery: &zapp.AppNameQuery{Name: p.cfg.ApplicationName, Method: object.TextQueryMethod_TEXT_QUERY_METHOD_EQUALS}}}}})
	})
	if err != nil {
		return "", err
	}
	if len(resp.Result) > 1 {
		return "", fmt.Errorf("multiple Zitadel applications named %q", p.cfg.ApplicationName)
	}
	desired := p.oidcRequest(projectID, "")
	if len(resp.Result) == 0 {
		created, err := retryRPC(ctx, func() (*management.AddOIDCAppResponse, error) {
			return p.api.AddOIDCApp(ctx, &management.AddOIDCAppRequest{
				ProjectId: desired.ProjectId, Name: p.cfg.ApplicationName, RedirectUris: desired.RedirectUris,
				ResponseTypes: desired.ResponseTypes, GrantTypes: desired.GrantTypes, AppType: desired.AppType,
				AuthMethodType: desired.AuthMethodType, PostLogoutRedirectUris: desired.PostLogoutRedirectUris,
				DevMode: desired.DevMode, AccessTokenType: desired.AccessTokenType, SkipNativeAppSuccessPage: true,
			})
		})
		if err != nil {
			return "", err
		}
		if created.ClientId == "" {
			return "", fmt.Errorf("Zitadel returned an empty OIDC client ID")
		}
		return created.ClientId, nil
	}
	existing := resp.Result[0]
	oidcConfig := existing.GetOidcConfig()
	if oidcConfig == nil {
		return "", fmt.Errorf("name-matched Zitadel application is not OIDC")
	}
	if oidcConfig.AppType != zapp.OIDCAppType_OIDC_APP_TYPE_NATIVE || oidcConfig.AuthMethodType != zapp.OIDCAuthMethodType_OIDC_AUTH_METHOD_TYPE_NONE {
		return "", fmt.Errorf("name-matched Zitadel application has incompatible type or auth method")
	}
	if oidcConfig.ClientId == "" {
		return "", fmt.Errorf("name-matched Zitadel application has no client ID")
	}
	if oidcConfigMatches(oidcConfig, desired) {
		return oidcConfig.ClientId, nil
	}
	desired.AppId = existing.Id
	if _, err := retryRPC(ctx, func() (*management.UpdateOIDCAppConfigResponse, error) {
		return p.api.UpdateOIDCAppConfig(ctx, desired)
	}); err != nil && !isNoChanges(err) {
		return "", err
	}
	return oidcConfig.ClientId, nil
}

func isNoChanges(err error) bool {
	return status.Code(err) == codes.FailedPrecondition && strings.Contains(strings.ToLower(status.Convert(err).Message()), "no changes")
}

func oidcConfigMatches(existing *zapp.OIDCConfig, desired *management.UpdateOIDCAppConfigRequest) bool {
	return equalSet(existing.RedirectUris, desired.RedirectUris) &&
		equalSet(existing.ResponseTypes, desired.ResponseTypes) &&
		equalSet(existing.GrantTypes, desired.GrantTypes) &&
		existing.AppType == desired.AppType &&
		existing.AuthMethodType == desired.AuthMethodType &&
		equalSet(existing.PostLogoutRedirectUris, desired.PostLogoutRedirectUris) &&
		existing.DevMode == desired.DevMode &&
		existing.AccessTokenType == desired.AccessTokenType &&
		existing.SkipNativeAppSuccessPage == desired.SkipNativeAppSuccessPage
}

func equalSet[T comparable](left, right []T) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[T]int, len(left))
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	return true
}

func (p *zitadelProvisioner) oidcRequest(projectID, appID string) *management.UpdateOIDCAppConfigRequest {
	return &management.UpdateOIDCAppConfigRequest{
		ProjectId: projectID, AppId: appID, RedirectUris: p.cfg.RedirectURIs,
		ResponseTypes: []zapp.OIDCResponseType{zapp.OIDCResponseType_OIDC_RESPONSE_TYPE_CODE},
		GrantTypes:    []zapp.OIDCGrantType{zapp.OIDCGrantType_OIDC_GRANT_TYPE_AUTHORIZATION_CODE, zapp.OIDCGrantType_OIDC_GRANT_TYPE_REFRESH_TOKEN},
		AppType:       zapp.OIDCAppType_OIDC_APP_TYPE_NATIVE, AuthMethodType: zapp.OIDCAuthMethodType_OIDC_AUTH_METHOD_TYPE_NONE,
		PostLogoutRedirectUris: p.cfg.PostLogoutURIs, DevMode: p.cfg.DevMode,
		AccessTokenType: zapp.OIDCTokenType_OIDC_TOKEN_TYPE_JWT, SkipNativeAppSuccessPage: true,
	}
}

func (p *zitadelProvisioner) FindHuman(ctx context.Context, loginName string) (identity, error) {
	loginName = strings.TrimSpace(loginName)
	if loginName == "" {
		return identity{}, fmt.Errorf("initial admin login name is required")
	}
	ctx, cancel := context.WithTimeout(ctx, durationOr(p.cfg.Timeout, 30*time.Second))
	defer cancel()
	resp, err := retryRPC(ctx, func() (*management.ListUsersResponse, error) {
		return p.api.ListUsers(ctx, &management.ListUsersRequest{Queries: []*user.SearchQuery{{Query: &user.SearchQuery_LoginNameQuery{LoginNameQuery: &user.LoginNameQuery{LoginName: loginName, Method: object.TextQueryMethod_TEXT_QUERY_METHOD_EQUALS}}}}})
	})
	if err != nil {
		return identity{}, err
	}
	if len(resp.Result) != 1 {
		return identity{}, fmt.Errorf("expected one Zitadel human for login %q, found %d", loginName, len(resp.Result))
	}
	found := resp.Result[0]
	human := found.GetHuman()
	if human == nil || found.Id == "" {
		return identity{}, fmt.Errorf("Zitadel login %q is not a human user", loginName)
	}
	displayName := human.GetProfile().GetDisplayName()
	if displayName == "" {
		displayName = found.GetPreferredLoginName()
	}
	return identity{Subject: found.Id, DisplayName: displayName, Email: human.GetEmail().GetEmail()}, nil
}

func retryRPC[T any](ctx context.Context, call func() (T, error)) (T, error) {
	var zero T
	delay := 100 * time.Millisecond
	for {
		value, err := call()
		if err == nil {
			return value, nil
		}
		code := status.Code(err)
		if code != codes.Unavailable && code != codes.DeadlineExceeded {
			return zero, err
		}
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
			if delay < 2*time.Second {
				delay *= 2
			}
		}
	}
}

func waitUntil(ctx context.Context, check func() (bool, error)) error {
	for {
		ok, err := check()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func durationOr(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}
