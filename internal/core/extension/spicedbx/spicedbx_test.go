package spicedbx

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
)

func TestObjectRefString(t *testing.T) {
	assert.Equal(t, "tenant:1001", ObjectRef{Type: "tenant", ID: "1001"}.String())
	assert.Empty(t, ObjectRef{Type: "tenant"}.String())
	assert.Empty(t, ObjectRef{ID: "1001"}.String())
}

func TestSubjectRefString(t *testing.T) {
	assert.Equal(t, "user:u1", SubjectRef{Object: ObjectRef{Type: "user", ID: "u1"}}.String())
	assert.Equal(t, "role:r1#member", SubjectRef{
		Object:   ObjectRef{Type: "role", ID: "r1"},
		Relation: "member",
	}.String())
}

func TestRelationshipString(t *testing.T) {
	rel := Relationship{
		Resource: ObjectRef{Type: "tenant", ID: "t1"},
		Relation: "store_view_role",
		Subject:  SubjectRef{Object: ObjectRef{Type: "role", ID: "r1"}, Relation: "member"},
	}
	assert.Equal(t, "tenant:t1#store_view_role@role:r1#member", rel.String())
}

func TestOpenValidation(t *testing.T) {
	_, err := Open(Config{Token: "token"})
	require.Error(t, err)
	_, err = Open(Config{Endpoint: "localhost:50051"})
	require.Error(t, err)

	insecureClient, err := Open(Config{Endpoint: "localhost:1", Token: "token", Insecure: true})
	require.NoError(t, err)
	assert.NoError(t, insecureClient.Close())

	secureClient, err := Open(Config{Endpoint: "localhost:1", Token: "token"})
	require.NoError(t, err)
	assert.NoError(t, secureClient.Close())

	assert.NoError(t, (*AuthzedClient)(nil).Close())
	assert.NoError(t, (&AuthzedClient{}).Close())

	path := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(path, []byte("token\n"), 0o600))
	fileClient, err := Open(Config{Endpoint: "localhost:1", TokenFile: path, Insecure: true})
	require.NoError(t, err)
	assert.NoError(t, fileClient.Close())
	_, err = Open(Config{Endpoint: "localhost:1", Token: "token", TokenFile: path})
	assert.ErrorContains(t, err, "mutually exclusive")
}

func TestAuthzedClientMethods(t *testing.T) {
	fake := &fakeSpice{}
	client := &AuthzedClient{client: fake}

	token, err := client.WriteSchema(context.Background(), "definition user {}")
	require.NoError(t, err)
	assert.Equal(t, ZedToken("schema-token"), token)
	assert.Equal(t, "definition user {}", fake.schema)

	allowed, err := client.Check(context.Background(), ObjectRef{Type: "tenant", ID: "t1"}, "store_view", SubjectRef{Object: ObjectRef{Type: "user", ID: "u1"}}, "fresh")
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, "fresh", fake.check.Consistency.GetAtLeastAsFresh().Token)
	bulk, err := client.CheckBulk(context.Background(), ObjectRef{Type: "tenant", ID: "t1"}, []string{"store_view", "menu_view"}, SubjectRef{Object: ObjectRef{Type: "user", ID: "u1"}}, "fresh")
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"store_view": true, "menu_view": true}, bulk)
	empty, err := client.CheckBulk(context.Background(), ObjectRef{}, nil, SubjectRef{}, "")
	require.NoError(t, err)
	assert.Empty(t, empty)

	token, err = client.WriteRelationships(context.Background(), []Relationship{{
		Resource: ObjectRef{Type: "tenant", ID: "t1"},
		Relation: "store_view_role",
		Subject:  SubjectRef{Object: ObjectRef{Type: "role", ID: "r1"}, Relation: "member"},
	}})
	require.NoError(t, err)
	assert.Equal(t, ZedToken("rel-token"), token)
	require.Len(t, fake.relationships, 1)
	assert.Equal(t, v1.RelationshipUpdate_OPERATION_TOUCH, fake.relationships[0].Operation)

	token, err = client.WriteRelationshipUpdates(context.Background(), []RelationshipUpdate{{
		Operation: RelationshipDelete,
		Relationship: Relationship{
			Resource: ObjectRef{Type: "role", ID: "r1"},
			Relation: "member",
			Subject:  SubjectRef{Object: ObjectRef{Type: "user", ID: "u1"}},
		},
	}})
	require.NoError(t, err)
	assert.Equal(t, ZedToken("rel-token"), token)
	require.Len(t, fake.relationships, 1)
	assert.Equal(t, v1.RelationshipUpdate_OPERATION_DELETE, fake.relationships[0].Operation)

	_, err = client.WriteRelationshipUpdates(context.Background(), []RelationshipUpdate{{Operation: "UPSERT"}})
	assert.ErrorContains(t, err, "unsupported relationship operation")

	ids, err := client.LookupResources(context.Background(), "store", "view", SubjectRef{Object: ObjectRef{Type: "user", ID: "u1"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"s1", "s2"}, ids)

	subjects, err := client.LookupSubjects(context.Background(), ObjectRef{Type: "store", ID: "s1"}, "view", "user")
	require.NoError(t, err)
	assert.Equal(t, "user:u1", subjects[0].String())

	assert.Equal(t, ZedToken(""), tokenFromProto(nil))
}

func TestAuthzedClientErrors(t *testing.T) {
	want := errors.New("boom")
	client := &AuthzedClient{client: &fakeSpice{err: want}}

	_, err := client.WriteSchema(context.Background(), "bad")
	assert.ErrorIs(t, err, want)
	_, err = client.Check(context.Background(), ObjectRef{}, "view", SubjectRef{}, "")
	assert.ErrorIs(t, err, want)
	_, err = client.CheckBulk(context.Background(), ObjectRef{}, []string{"view"}, SubjectRef{}, "")
	assert.ErrorIs(t, err, want)
	_, err = client.WriteRelationships(context.Background(), nil)
	assert.ErrorIs(t, err, want)
	_, err = client.WriteRelationshipUpdates(context.Background(), nil)
	assert.ErrorIs(t, err, want)
	_, err = client.LookupResources(context.Background(), "store", "view", SubjectRef{})
	assert.ErrorIs(t, err, want)
	_, err = client.LookupSubjects(context.Background(), ObjectRef{}, "view", "user")
	assert.ErrorIs(t, err, want)
}

func TestCheckBulkRejectsInvalidAndPartialResponses(t *testing.T) {
	client := &AuthzedClient{client: &fakeSpice{}}
	permissions := make([]string, 101)
	for i := range permissions {
		permissions[i] = "view"
	}
	_, err := client.CheckBulk(context.Background(), ObjectRef{}, permissions, SubjectRef{}, "")
	assert.ErrorContains(t, err, "at most 100")
	_, err = client.CheckBulk(context.Background(), ObjectRef{}, []string{""}, SubjectRef{}, "")
	assert.ErrorContains(t, err, "empty")

	client.client = &fakeSpice{bulkResponse: &v1.CheckBulkPermissionsResponse{}}
	_, err = client.CheckBulk(context.Background(), ObjectRef{}, []string{"view"}, SubjectRef{}, "")
	assert.ErrorContains(t, err, "returned 0 of 1")
	client.client = &fakeSpice{bulkResponse: &v1.CheckBulkPermissionsResponse{Pairs: []*v1.CheckBulkPermissionsPair{{Response: &v1.CheckBulkPermissionsPair_Error{Error: &statuspb.Status{Code: 13, Message: "down"}}}}}}
	_, err = client.CheckBulk(context.Background(), ObjectRef{}, []string{"view"}, SubjectRef{}, "")
	assert.ErrorContains(t, err, "down")
	client.client = &fakeSpice{bulkResponse: &v1.CheckBulkPermissionsResponse{Pairs: []*v1.CheckBulkPermissionsPair{{}}}}
	_, err = client.CheckBulk(context.Background(), ObjectRef{}, []string{"view"}, SubjectRef{}, "")
	assert.ErrorContains(t, err, "incomplete")
}

type fakeSpice struct {
	schema        string
	check         *v1.CheckPermissionRequest
	relationships []*v1.RelationshipUpdate
	err           error
	bulkResponse  *v1.CheckBulkPermissionsResponse
}

func (f *fakeSpice) WriteSchema(_ context.Context, req *v1.WriteSchemaRequest, _ ...grpc.CallOption) (*v1.WriteSchemaResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.schema = req.Schema
	return &v1.WriteSchemaResponse{WrittenAt: &v1.ZedToken{Token: "schema-token"}}, nil
}

func (f *fakeSpice) CheckPermission(_ context.Context, req *v1.CheckPermissionRequest, _ ...grpc.CallOption) (*v1.CheckPermissionResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.check = req
	return &v1.CheckPermissionResponse{Permissionship: v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION}, nil
}

func (f *fakeSpice) CheckBulkPermissions(_ context.Context, req *v1.CheckBulkPermissionsRequest, _ ...grpc.CallOption) (*v1.CheckBulkPermissionsResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.bulkResponse != nil {
		return f.bulkResponse, nil
	}
	pairs := make([]*v1.CheckBulkPermissionsPair, 0, len(req.Items))
	for _, item := range req.Items {
		pairs = append(pairs, &v1.CheckBulkPermissionsPair{Request: item, Response: &v1.CheckBulkPermissionsPair_Item{Item: &v1.CheckBulkPermissionsResponseItem{Permissionship: v1.CheckPermissionResponse_PERMISSIONSHIP_HAS_PERMISSION}}})
	}
	return &v1.CheckBulkPermissionsResponse{Pairs: pairs}, nil
}

func (f *fakeSpice) WriteRelationships(_ context.Context, req *v1.WriteRelationshipsRequest, _ ...grpc.CallOption) (*v1.WriteRelationshipsResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.relationships = req.Updates
	return &v1.WriteRelationshipsResponse{WrittenAt: &v1.ZedToken{Token: "rel-token"}}, nil
}

func (f *fakeSpice) LookupResources(context.Context, *v1.LookupResourcesRequest, ...grpc.CallOption) (v1.PermissionsService_LookupResourcesClient, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &fakeResourceStream{items: []string{"s1", "s2"}}, nil
}

func (f *fakeSpice) LookupSubjects(context.Context, *v1.LookupSubjectsRequest, ...grpc.CallOption) (v1.PermissionsService_LookupSubjectsClient, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &fakeSubjectStream{items: []string{"u1"}}, nil
}

func (f *fakeSpice) Close() error { return nil }

type fakeResourceStream struct {
	grpc.ClientStream
	items []string
}

func (s *fakeResourceStream) Recv() (*v1.LookupResourcesResponse, error) {
	if len(s.items) == 0 {
		return nil, io.EOF
	}
	id := s.items[0]
	s.items = s.items[1:]
	return &v1.LookupResourcesResponse{ResourceObjectId: id}, nil
}

type fakeSubjectStream struct {
	grpc.ClientStream
	items []string
}

func (s *fakeSubjectStream) Recv() (*v1.LookupSubjectsResponse, error) {
	if len(s.items) == 0 {
		return nil, io.EOF
	}
	id := s.items[0]
	s.items = s.items[1:]
	return &v1.LookupSubjectsResponse{Subject: &v1.ResolvedSubject{SubjectObjectId: id}}, nil
}
