package workspace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Service struct {
	repo         Repository
	executor     Executor
	notifier     Notifier
	artifactRoot string
}

func NewService(repo Repository, executor Executor, notifier Notifier, artifactRoot string) *Service {
	if repo == nil {
		panic("workspace service requires repository")
	}
	if notifier == nil {
		notifier = noopNotifier{}
	}
	return &Service{repo: repo, executor: executor, notifier: notifier, artifactRoot: artifactRoot}
}

func (s *Service) ListWorkItems(ctx context.Context, itemType, status, parent string) ([]WorkItem, error) {
	return s.repo.ListWorkItems(ctx, itemType, status, parent)
}

func (s *Service) GetWorkItem(ctx context.Context, id string) (*WorkItem, error) {
	item, err := s.repo.GetWorkItem(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return item, nil
}

func (s *Service) CreateWorkItem(ctx context.Context, item WorkItem) (*WorkItem, error) {
	var err error
	item, err = normalizeWorkItem(item)
	if err != nil {
		return nil, err
	}
	item.ID = "wi-" + randomHex(8)
	if err := s.repo.CreateWorkItem(ctx, &item); err != nil {
		return nil, err
	}
	s.notifier.WorkItemChanged(ctx, &item, "created")
	return &item, nil
}

func (s *Service) CreateWorkItemFromChannel(ctx context.Context, channelID string, item WorkItem) (*WorkItem, error) {
	item.ChannelID = channelID
	return s.CreateWorkItem(ctx, item)
}

func (s *Service) UpdateWorkItem(ctx context.Context, id string, patch WorkItemPatch) (*WorkItem, error) {
	existing, err := s.GetWorkItem(ctx, id)
	if err != nil {
		return nil, err
	}
	updated, err := applyWorkItemPatch(*existing, patch)
	if err != nil {
		return nil, err
	}
	if updated.ParentID != "" {
		if _, err := s.GetWorkItem(ctx, updated.ParentID); err != nil {
			return nil, ErrInvalid
		}
	}
	if err := s.repo.UpdateWorkItem(ctx, &updated); err != nil {
		return nil, err
	}
	if updated.Status != existing.Status {
		s.notifier.WorkItemChanged(ctx, &updated, "status")
	}
	return &updated, nil
}

func (s *Service) DeleteWorkItem(ctx context.Context, id string) error {
	if _, err := s.GetWorkItem(ctx, id); err != nil {
		return err
	}
	return s.repo.DeleteWorkItem(ctx, id)
}

func (s *Service) ExecuteWorkItem(ctx context.Context, id string) (string, error) {
	item, err := s.GetWorkItem(ctx, id)
	if err != nil {
		return "", err
	}
	if item.Status == "in_progress" && item.WorkflowRunID != "" {
		return "", ErrConflict
	}
	if s.executor == nil {
		return "", errors.New("workspace executor is unavailable")
	}
	runID, err := s.executor.ExecuteWorkItem(ctx, item)
	if err != nil {
		return "", err
	}
	item.Status = "in_progress"
	item.WorkflowRunID = runID
	if err := s.repo.UpdateWorkItem(ctx, item); err != nil {
		return "", err
	}
	s.notifier.WorkItemChanged(ctx, item, "status")
	return runID, nil
}

func (s *Service) ListAttachments(ctx context.Context, ownerType, ownerID string) ([]Attachment, error) {
	return s.repo.ListAttachments(ctx, ownerType, ownerID)
}

func (s *Service) GetAttachment(ctx context.Context, id string) (*Attachment, error) {
	a, err := s.repo.GetAttachment(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return a, nil
}

func (s *Service) UploadAttachment(ctx context.Context, ownerType, ownerID, filename string, src io.Reader) (*Attachment, error) {
	if !oneOf(ownerType, "work_item", "channel") || strings.TrimSpace(ownerID) == "" || strings.TrimSpace(filename) == "" {
		return nil, ErrInvalid
	}
	root := s.artifactRoot
	if root == "" {
		return nil, errors.New("workspace artifact root is not configured")
	}
	id := "att-" + randomHex(8)
	dir := filepath.Join(root, "attachments")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create attachment directory: %w", err)
	}
	path := filepath.Join(dir, id)
	dst, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, fmt.Errorf("create attachment: %w", err)
	}
	n, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return nil, errors.Join(copyErr, closeErr)
	}
	mime, _ := SafeMime(filename)
	a := &Attachment{ID: id, OwnerType: ownerType, OwnerID: ownerID, Filename: filepath.Base(filename), Mime: mime, SizeBytes: n, StorePath: path}
	if err := s.repo.CreateAttachment(ctx, a); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return a, nil
}

func (s *Service) ListOkrs(ctx context.Context) ([]Okr, error) { return s.repo.ListOkrs(ctx) }

func (s *Service) CreateOkr(ctx context.Context, okr Okr) (*Okr, error) {
	okr.Title = strings.TrimSpace(okr.Title)
	if okr.Title == "" || len(okr.Title) > 300 {
		return nil, ErrInvalid
	}
	if okr.KeyResults == "" {
		okr.KeyResults = "[]"
	}
	if err := ValidateKeyResults(okr.KeyResults); err != nil {
		return nil, err
	}
	okr.ID = "okr-" + randomHex(6)
	if err := s.repo.CreateOkr(ctx, &okr); err != nil {
		return nil, err
	}
	return &okr, nil
}

func (s *Service) UpdateOkr(ctx context.Context, id string, update Okr) (*Okr, error) {
	existing, err := s.repo.GetOkr(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	update.ID, update.EntityID, update.CreatedAt = existing.ID, existing.EntityID, existing.CreatedAt
	update.Title = strings.TrimSpace(update.Title)
	if update.Title == "" || len(update.Title) > 300 || ValidateKeyResults(update.KeyResults) != nil {
		return nil, ErrInvalid
	}
	if err := s.repo.UpdateOkr(ctx, &update); err != nil {
		return nil, err
	}
	return &update, nil
}

func (s *Service) DeleteOkr(ctx context.Context, id string) error {
	if _, err := s.repo.GetOkr(ctx, id); err != nil {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return s.repo.DeleteOkr(ctx, id)
}

func ValidateKeyResults(raw string) error {
	var results []struct {
		Title    string   `json:"title"`
		Target   *float64 `json:"target"`
		Progress *float64 `json:"progress"`
		Unit     string   `json:"unit"`
	}
	if err := json.Unmarshal([]byte(raw), &results); err != nil {
		return ErrInvalid
	}
	for _, result := range results {
		if strings.TrimSpace(result.Title) == "" || result.Target == nil || result.Progress == nil || *result.Target < 0 || *result.Progress < 0 {
			return ErrInvalid
		}
	}
	return nil
}

func SafeMime(filename string) (mime string, inline bool) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".gif":
		return "image/gif", true
	case ".webp":
		return "image/webp", true
	case ".mp4":
		return "video/mp4", true
	case ".webm":
		return "video/webm", true
	case ".pdf":
		return "application/pdf", true
	default:
		return "application/octet-stream", false
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
