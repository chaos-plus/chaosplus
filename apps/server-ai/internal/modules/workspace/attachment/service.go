package attachment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

type BlobStore interface {
	Put(context.Context, string, io.Reader, int64, string) error
	Get(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

type Service struct {
	repository Repository
	blobs      BlobStore
	nextID     func() (guid.ID, error)
}

func NewService(repository Repository, blobs BlobStore, nextID func() (guid.ID, error)) *Service {
	if repository == nil || blobs == nil || nextID == nil {
		panic("attachment service requires repository, blob store, and id generator")
	}
	return &Service{repository: repository, blobs: blobs, nextID: nextID}
}

func (s *Service) Upload(ctx context.Context, input UploadInput) (*Attachment, error) {
	if err := validateUpload(&input); err != nil {
		return nil, err
	}
	tenantID, entityID, principalID := authn.TenantIDFromContext(ctx), authn.EntityIDFromContext(ctx), authn.PrincipalIDFromContext(ctx)
	if tenantID.Zero() || entityID.Zero() || principalID.Zero() {
		return nil, ErrUnavailable
	}
	id, err := s.nextID()
	if err != nil {
		return nil, fmt.Errorf("generate attachment id: %w", err)
	}
	key := fmt.Sprintf("workspace/%s/%s/%s", tenantID.String(), entityID.String(), id.String())
	hash := sha256.New()
	counted := &countingReader{reader: io.TeeReader(io.LimitReader(input.Content, input.SizeBytes+1), hash)}
	if err := s.blobs.Put(ctx, key, counted, input.SizeBytes, input.ContentType); err != nil {
		return nil, fmt.Errorf("store attachment content: %w", err)
	}
	if counted.count != input.SizeBytes {
		if cleanupErr := s.blobs.Delete(ctx, key); cleanupErr != nil {
			return nil, errorsJoin(ErrInvalid, cleanupErr)
		}
		return nil, ErrInvalid
	}
	value := &Attachment{ID: id, TenantID: tenantID, EntityID: entityID, OwnerID: principalID, ResourceType: input.ResourceType, ResourceID: input.ResourceID, Filename: input.Filename, ContentType: input.ContentType, SizeBytes: input.SizeBytes, Checksum: hex.EncodeToString(hash.Sum(nil)), ObjectKey: key}
	if err := s.repository.Create(ctx, value); err != nil {
		if cleanupErr := s.blobs.Delete(ctx, key); cleanupErr != nil {
			return nil, errorsJoin(err, cleanupErr)
		}
		return nil, err
	}
	return value, nil
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	r.count += int64(count)
	return count, err
}

func errorsJoin(primary, cleanup error) error {
	return fmt.Errorf("%w; attachment cleanup failed: %v", primary, cleanup)
}

func (s *Service) List(ctx context.Context, resourceType ResourceType, resourceID guid.ID) ([]Attachment, error) {
	input := UploadInput{ResourceType: resourceType, ResourceID: resourceID, Filename: "list", ContentType: "application/octet-stream", SizeBytes: 1, Content: &emptyReader{}}
	if err := validateUpload(&input); err != nil {
		return nil, err
	}
	return s.repository.List(ctx, resourceType, resourceID)
}

func (s *Service) ValidateConversationReferences(ctx context.Context, channelID guid.ID, ids []guid.ID) error {
	if channelID.Zero() {
		return ErrInvalid
	}
	for _, id := range ids {
		value, err := s.repository.Get(ctx, id)
		if err != nil {
			return err
		}
		if value.ResourceType != ResourceConversation || value.ResourceID != channelID || value.Status != StatusAvailable {
			return ErrInvalid
		}
	}
	return nil
}

type emptyReader struct{}

func (*emptyReader) Read([]byte) (int, error) { return 0, io.EOF }

func (s *Service) Download(ctx context.Context, id guid.ID) (*Attachment, io.ReadCloser, error) {
	if id.Zero() {
		return nil, nil, ErrInvalid
	}
	value, err := s.repository.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if value.Status != StatusAvailable {
		return nil, nil, ErrUnavailable
	}
	reader, err := s.blobs.Get(ctx, value.ObjectKey)
	if err != nil {
		return nil, nil, fmt.Errorf("read attachment content: %w", err)
	}
	return value, reader, nil
}

func (s *Service) Delete(ctx context.Context, id guid.ID, version int64) error {
	if id.Zero() || version < 1 {
		return ErrInvalid
	}
	value, err := s.repository.MarkDeleting(ctx, id, version)
	if err != nil {
		return err
	}
	if err := s.blobs.Delete(ctx, value.ObjectKey); err != nil {
		markErr := s.repository.MarkDeleteFailed(ctx, id, value.Version)
		if markErr != nil {
			return errorsJoin(err, markErr)
		}
		return fmt.Errorf("delete attachment content: %w", err)
	}
	return s.repository.CompleteDelete(ctx, id, value.Version)
}
