// Package spicedbx defines the thin boundary this application uses to talk to
// SpiceDB. The first phase keeps this as an interface so domain modules can be
// wired and tested before a concrete authzed-go client is installed.
package spicedbx

import (
	"context"
	"fmt"
	"io"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	authzedv1 "github.com/authzed/authzed-go/v1"
	"github.com/authzed/grpcutil"
	"github.com/chaos-plus/chaosplus/internal/core/extension/secretx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ObjectRef names a SpiceDB object, e.g. tenant:1001 or store:9001.
type ObjectRef struct {
	Type string
	ID   string
}

func (o ObjectRef) String() string {
	if o.Type == "" || o.ID == "" {
		return ""
	}
	return o.Type + ":" + o.ID
}

// SubjectRef names a SpiceDB subject, optionally with a subject relation such as
// role:8#member.
type SubjectRef struct {
	Object   ObjectRef
	Relation string
}

func (s SubjectRef) String() string {
	base := s.Object.String()
	if base == "" || s.Relation == "" {
		return base
	}
	return base + "#" + s.Relation
}

// Relationship is the unit written to SpiceDB.
type Relationship struct {
	Resource ObjectRef
	Relation string
	Subject  SubjectRef
}

func (r Relationship) String() string {
	return fmt.Sprintf("%s#%s@%s", r.Resource, r.Relation, r.Subject)
}

// RelationshipOperation is an idempotent relationship mutation supported by
// the application outbox.
type RelationshipOperation string

const (
	RelationshipTouch  RelationshipOperation = "TOUCH"
	RelationshipDelete RelationshipOperation = "DELETE"
)

// RelationshipUpdate applies one operation to one exact relationship.
type RelationshipUpdate struct {
	Operation    RelationshipOperation
	Relationship Relationship
}

// ZedToken is stored after authorization writes and reused for at-least-as-fresh
// checks once the concrete client lands.
type ZedToken string

type Config struct {
	Enabled     bool   `mapstructure:"enabled" description:"enable SpiceDB authorization checks" default:"false"`
	Endpoint    string `mapstructure:"endpoint" description:"SpiceDB gRPC endpoint" default:"10.0.0.100:38877"`
	Token       string `mapstructure:"token" description:"SpiceDB preshared key or bearer token"`
	TokenFile   string `mapstructure:"token_file" description:"file containing the SpiceDB token; mutually exclusive with token" default:""`
	Insecure    bool   `mapstructure:"insecure" description:"use insecure gRPC transport for local SpiceDB" default:"false"`
	ApplySchema bool   `mapstructure:"apply_schema" description:"write generated authz schema on startup" default:"false"`
}

// Client is the narrow app-facing SpiceDB port.
type Client interface {
	Check(ctx context.Context, resource ObjectRef, permission string, subject SubjectRef, token ZedToken) (bool, error)
	CheckBulk(ctx context.Context, resource ObjectRef, permissions []string, subject SubjectRef, token ZedToken) (map[string]bool, error)
	WriteRelationships(ctx context.Context, rels []Relationship) (ZedToken, error)
	WriteRelationshipUpdates(ctx context.Context, updates []RelationshipUpdate) (ZedToken, error)
	LookupResources(ctx context.Context, resourceType, permission string, subject SubjectRef) ([]string, error)
	LookupSubjects(ctx context.Context, resource ObjectRef, permission, subjectType string) ([]SubjectRef, error)
}

type AuthzedClient struct {
	client spiceAPI
}

type spiceAPI interface {
	WriteSchema(context.Context, *v1.WriteSchemaRequest, ...grpc.CallOption) (*v1.WriteSchemaResponse, error)
	CheckPermission(context.Context, *v1.CheckPermissionRequest, ...grpc.CallOption) (*v1.CheckPermissionResponse, error)
	CheckBulkPermissions(context.Context, *v1.CheckBulkPermissionsRequest, ...grpc.CallOption) (*v1.CheckBulkPermissionsResponse, error)
	WriteRelationships(context.Context, *v1.WriteRelationshipsRequest, ...grpc.CallOption) (*v1.WriteRelationshipsResponse, error)
	LookupResources(context.Context, *v1.LookupResourcesRequest, ...grpc.CallOption) (v1.PermissionsService_LookupResourcesClient, error)
	LookupSubjects(context.Context, *v1.LookupSubjectsRequest, ...grpc.CallOption) (v1.PermissionsService_LookupSubjectsClient, error)
	Close() error
}

func (c *AuthzedClient) CheckBulk(ctx context.Context, resource ObjectRef, permissions []string, subject SubjectRef, token ZedToken) (map[string]bool, error) {
	if len(permissions) == 0 {
		return map[string]bool{}, nil
	}
	if len(permissions) > 100 {
		return nil, fmt.Errorf("spicedb bulk check supports at most 100 permissions")
	}
	items := make([]*v1.CheckBulkPermissionsRequestItem, 0, len(permissions))
	for _, permission := range permissions {
		if permission == "" {
			return nil, fmt.Errorf("spicedb bulk check permission is empty")
		}
		items = append(items, &v1.CheckBulkPermissionsRequestItem{Resource: objectRef(resource), Permission: permission, Subject: subjectRef(subject)})
	}
	resp, err := c.client.CheckBulkPermissions(ctx, &v1.CheckBulkPermissionsRequest{Consistency: consistency(token), Items: items})
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool, len(permissions))
	for _, pair := range resp.Pairs {
		if pair.GetError() != nil {
			return nil, fmt.Errorf("spicedb bulk check item failed: %s", pair.GetError().Message)
		}
		if pair.GetRequest() == nil || pair.GetItem() == nil {
			return nil, fmt.Errorf("spicedb bulk check returned incomplete pair")
		}
		allowed[pair.GetRequest().Permission] = pair.GetItem().Permissionship == v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION
	}
	if len(allowed) != len(permissions) {
		return nil, fmt.Errorf("spicedb bulk check returned %d of %d results", len(allowed), len(permissions))
	}
	return allowed, nil
}

func Open(cfg Config) (*AuthzedClient, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("spicedb endpoint is required")
	}
	token, err := secretx.Resolve("authz.spicedb.token", cfg.Token, cfg.TokenFile, 4096)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, fmt.Errorf("spicedb token is required")
	}
	cfg.Token = token
	opts := []grpc.DialOption{}
	if cfg.Insecure {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()), grpcutil.WithInsecureBearerToken(cfg.Token))
	} else {
		certs, err := grpcutil.WithSystemCerts(grpcutil.VerifyCA)
		if err != nil {
			return nil, err
		}
		opts = append(opts, certs, grpcutil.WithBearerToken(cfg.Token))
	}
	client, err := authzedv1.NewClient(cfg.Endpoint, opts...)
	if err != nil {
		return nil, err
	}
	return &AuthzedClient{client: client}, nil
}

func (c *AuthzedClient) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

func (c *AuthzedClient) WriteSchema(ctx context.Context, schema string) (ZedToken, error) {
	resp, err := c.client.WriteSchema(ctx, &v1.WriteSchemaRequest{Schema: schema})
	if err != nil {
		return "", err
	}
	return tokenFromProto(resp.WrittenAt), nil
}

func (c *AuthzedClient) Check(ctx context.Context, resource ObjectRef, permission string, subject SubjectRef, token ZedToken) (bool, error) {
	resp, err := c.client.CheckPermission(ctx, &v1.CheckPermissionRequest{
		Resource:    objectRef(resource),
		Permission:  permission,
		Subject:     subjectRef(subject),
		Consistency: consistency(token),
	})
	if err != nil {
		return false, err
	}
	return resp.Permissionship == v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION, nil
}

func (c *AuthzedClient) WriteRelationships(ctx context.Context, rels []Relationship) (ZedToken, error) {
	updates := make([]RelationshipUpdate, 0, len(rels))
	for _, rel := range rels {
		updates = append(updates, RelationshipUpdate{Operation: RelationshipTouch, Relationship: rel})
	}
	return c.WriteRelationshipUpdates(ctx, updates)
}

func (c *AuthzedClient) WriteRelationshipUpdates(ctx context.Context, relationshipUpdates []RelationshipUpdate) (ZedToken, error) {
	updates := make([]*v1.RelationshipUpdate, 0, len(relationshipUpdates))
	for _, update := range relationshipUpdates {
		operation, err := relationshipOperation(update.Operation)
		if err != nil {
			return "", err
		}
		updates = append(updates, &v1.RelationshipUpdate{
			Operation:    operation,
			Relationship: relationship(update.Relationship),
		})
	}
	resp, err := c.client.WriteRelationships(ctx, &v1.WriteRelationshipsRequest{Updates: updates})
	if err != nil {
		return "", err
	}
	return tokenFromProto(resp.WrittenAt), nil
}

func relationshipOperation(operation RelationshipOperation) (v1.RelationshipUpdate_Operation, error) {
	switch operation {
	case RelationshipTouch:
		return v1.RelationshipUpdate_OPERATION_TOUCH, nil
	case RelationshipDelete:
		return v1.RelationshipUpdate_OPERATION_DELETE, nil
	default:
		return v1.RelationshipUpdate_OPERATION_UNSPECIFIED, fmt.Errorf("unsupported relationship operation %q", operation)
	}
}

func (c *AuthzedClient) LookupResources(ctx context.Context, resourceType, permission string, subject SubjectRef) ([]string, error) {
	stream, err := c.client.LookupResources(ctx, &v1.LookupResourcesRequest{
		ResourceObjectType: resourceType,
		Permission:         permission,
		Subject:            subjectRef(subject),
		Consistency:        minimizeLatency(),
	})
	if err != nil {
		return nil, err
	}
	var ids []string
	for {
		resp, err := stream.Recv()
		if err == nil {
			ids = append(ids, resp.ResourceObjectId)
			continue
		}
		if err == io.EOF {
			return ids, nil
		}
		return nil, err
	}
}

func (c *AuthzedClient) LookupSubjects(ctx context.Context, resource ObjectRef, permission, subjectType string) ([]SubjectRef, error) {
	stream, err := c.client.LookupSubjects(ctx, &v1.LookupSubjectsRequest{
		Resource:          objectRef(resource),
		Permission:        permission,
		SubjectObjectType: subjectType,
		Consistency:       minimizeLatency(),
	})
	if err != nil {
		return nil, err
	}
	var subjects []SubjectRef
	for {
		resp, err := stream.Recv()
		if err == nil {
			if resp.Subject != nil {
				subjects = append(subjects, SubjectRef{Object: ObjectRef{Type: subjectType, ID: resp.Subject.SubjectObjectId}})
			}
			continue
		}
		if err == io.EOF {
			return subjects, nil
		}
		return nil, err
	}
}

func objectRef(ref ObjectRef) *v1.ObjectReference {
	return &v1.ObjectReference{ObjectType: ref.Type, ObjectId: ref.ID}
}

func subjectRef(ref SubjectRef) *v1.SubjectReference {
	return &v1.SubjectReference{
		Object:           objectRef(ref.Object),
		OptionalRelation: ref.Relation,
	}
}

func relationship(rel Relationship) *v1.Relationship {
	return &v1.Relationship{
		Resource: objectRef(rel.Resource),
		Relation: rel.Relation,
		Subject:  subjectRef(rel.Subject),
	}
}

func consistency(token ZedToken) *v1.Consistency {
	if token == "" {
		return &v1.Consistency{Requirement: &v1.Consistency_FullyConsistent{FullyConsistent: true}}
	}
	return &v1.Consistency{Requirement: &v1.Consistency_AtLeastAsFresh{AtLeastAsFresh: &v1.ZedToken{Token: string(token)}}}
}

func minimizeLatency() *v1.Consistency {
	return &v1.Consistency{Requirement: &v1.Consistency_MinimizeLatency{MinimizeLatency: true}}
}

func tokenFromProto(token *v1.ZedToken) ZedToken {
	if token == nil {
		return ""
	}
	return ZedToken(token.Token)
}
