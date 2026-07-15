package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/core/extension/humax/respx"
)

type meOutput struct {
	Subject           string `json:"subject"`
	SpiceDBSubject    string `json:"spicedb_subject"`
	Issuer            string `json:"issuer"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Email             string `json:"email,omitempty"`
	OrganizationID    string `json:"organization_id,omitempty"`
}

type meInput struct {
	Authorization string `header:"Authorization" doc:"Bearer access token issued by Zitadel"`
}

func RegisterREST(a huma.API, verifier *authnext.Verifier) {
	huma.Register(a, huma.Operation{
		OperationID: "authn-me",
		Method:      http.MethodGet,
		Path:        "/authn/me",
		Summary:     "Return the authenticated Zitadel subject",
		Tags:        []string{"authn"},
	}, func(ctx context.Context, in *meInput) (*respx.Body[meOutput], error) {
		claims, err := verifier.VerifyAuthorization(ctx, in.Authorization)
		if err != nil {
			return nil, huma.Error401Unauthorized("unauthorized", err)
		}
		return respx.OK(ctx, meOutput{
			Subject:           claims.Subject,
			SpiceDBSubject:    claims.SubjectRef().String(),
			Issuer:            claims.Issuer,
			PreferredUsername: claims.PreferredUsername,
			Email:             claims.Email,
			OrganizationID:    claims.OrganizationID,
		}), nil
	})
}
