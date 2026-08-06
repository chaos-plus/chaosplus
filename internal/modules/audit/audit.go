package audit

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

var (
	ErrInvalidEvent  = errors.New("invalid audit event")
	ErrInvalidFilter = errors.New("invalid audit filter")
	ErrIntegrity     = errors.New("audit integrity check failed")
)

const exportBatchSize = 500

type EventInput struct {
	TenantID    string
	PrincipalID string
	EventType   string
	TargetType  string
	TargetID    string
	Outcome     string
	IPAddress   string
	UserAgent   string
	Detail      map[string]any
}

type Event struct {
	ID           string          `json:"id"`
	TenantID     string          `json:"tenant_id"`
	PrincipalID  string          `json:"principal_id,omitempty"`
	EventType    string          `json:"event_type"`
	TargetType   string          `json:"target_type,omitempty"`
	TargetID     string          `json:"target_id,omitempty"`
	Outcome      string          `json:"outcome"`
	IPAddress    string          `json:"ip_address,omitempty"`
	UserAgent    string          `json:"user_agent,omitempty"`
	Detail       json.RawMessage `json:"detail"`
	Sequence     int64           `json:"sequence"`
	PreviousHash string          `json:"previous_hash,omitempty"`
	EventHash    string          `json:"event_hash"`
	CreatedAt    time.Time       `json:"created_at"`
}

type Filter struct {
	TenantID    string
	PrincipalID string
	EventType   string
	Outcome     string
	TargetType  string
	TargetID    string
	From        time.Time
	To          time.Time
	Offset      int
	Limit       int
}

type Integrity struct {
	TenantID       string       `json:"tenant_id"`
	Valid          bool         `json:"valid"`
	VerifiedEvents int64        `json:"verified_events"`
	HeadSequence   int64        `json:"head_sequence"`
	HeadHash       string       `json:"head_hash,omitempty"`
	Anchor         AnchorStatus `json:"anchor"`
}

type ExportSnapshot struct {
	Filter      Filter
	Integrity   Integrity
	GeneratedAt time.Time
}

type exportCriteria struct {
	PrincipalID string `json:"principal_id,omitempty"`
	EventType   string `json:"event_type,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
	TargetType  string `json:"target_type,omitempty"`
	TargetID    string `json:"target_id,omitempty"`
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
}

type exportManifest struct {
	Type           string         `json:"type"`
	Schema         string         `json:"schema"`
	TenantID       string         `json:"tenant_id"`
	GeneratedAt    time.Time      `json:"generated_at"`
	HeadSequence   int64          `json:"head_sequence"`
	HeadHash       string         `json:"head_hash,omitempty"`
	VerifiedEvents int64          `json:"verified_events"`
	Filter         exportCriteria `json:"filter"`
}

type exportEvent struct {
	Type  string `json:"type"`
	Event Event  `json:"event"`
}

type exportComplete struct {
	Type           string `json:"type"`
	ExportedEvents int64  `json:"exported_events"`
	ContentSHA256  string `json:"content_sha256"`
}

type eventRow struct {
	bun.BaseModel `bun:"table:iam_audit_events"`
	ID            string `bun:"id,pk"`
	TenantID      string
	PrincipalID   string
	EventType     string
	TargetType    string
	TargetID      string
	Outcome       string
	IPAddress     string
	UserAgent     string
	Detail        string
	CreatedAt     int64
	Sequence      int64
	PreviousHash  string
	EventHash     string
}

type headRow struct {
	bun.BaseModel `bun:"table:iam_audit_heads"`
	TenantID      string `bun:"tenant_id,pk"`
	Sequence      int64
	EventHash     string
	UpdatedAt     int64
}

type Service struct {
	db      *bun.DB
	dialect string
	now     func() time.Time
	nextID  func() (string, error)
	anchor  *AnchorStore
	signer  *RootSigner
}

func NewService(db *bun.DB) *Service {
	return NewServiceWithAnchor(db, nil)
}

func NewServiceWithAnchor(db *bun.DB, anchor *AnchorStore) *Service {
	if db == nil {
		panic("audit service requires database")
	}
	dialect := db.Dialect().Name().String()
	return &Service{db: db, dialect: dialect, now: time.Now, nextID: secureID, anchor: anchor}
}

// NewServiceWithAnchorAndSigner builds an anchored service that signs every
// committed head with the configured Ed25519 root key.
func NewServiceWithAnchorAndSigner(db *bun.DB, anchor *AnchorStore, signer *RootSigner) *Service {
	service := NewServiceWithAnchor(db, anchor)
	service.signer = signer
	return service
}

func (s *Service) Append(ctx context.Context, input EventInput) (Event, error) {
	var event Event
	err := s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var err error
		event, err = s.AppendTo(ctx, tx, input)
		return err
	})
	return event, err
}

func (s *Service) AppendTo(ctx context.Context, db bun.IDB, input EventInput) (Event, error) {
	if db == nil || validate(input) != nil {
		return Event{}, ErrInvalidEvent
	}
	detail, err := json.Marshal(input.Detail)
	if err != nil {
		return Event{}, fmt.Errorf("encode audit detail: %w", err)
	}
	if string(detail) == "null" {
		detail = []byte("{}")
	}
	id, err := s.nextID()
	if err != nil {
		return Event{}, fmt.Errorf("generate audit id: %w", err)
	}
	now := s.now().UTC()
	head := headRow{TenantID: input.TenantID, UpdatedAt: now.UnixMilli()}
	if _, err := db.NewInsert().Model(&head).Ignore().Exec(ctx); err != nil {
		return Event{}, fmt.Errorf("ensure audit head: %w", err)
	}
	query := db.NewSelect().Model(&head).Where("tenant_id = ?", input.TenantID)
	if s.dialect != "sqlite" {
		query = query.For("UPDATE")
	}
	if err := query.Scan(ctx); err != nil {
		return Event{}, fmt.Errorf("lock audit head: %w", err)
	}
	row := eventRow{
		ID: id, TenantID: input.TenantID, PrincipalID: input.PrincipalID,
		EventType: input.EventType, TargetType: input.TargetType, TargetID: input.TargetID,
		Outcome: input.Outcome, IPAddress: input.IPAddress, UserAgent: input.UserAgent,
		Detail: string(detail), CreatedAt: now.UnixMilli(), Sequence: head.Sequence + 1,
		PreviousHash: head.EventHash,
	}
	row.EventHash = hash(row)
	if _, err := db.NewInsert().Model(&row).Exec(ctx); err != nil {
		return Event{}, fmt.Errorf("append audit event: %w", err)
	}
	result, err := db.NewUpdate().Model((*headRow)(nil)).
		Set("sequence = ?", row.Sequence).Set("event_hash = ?", row.EventHash).
		Set("updated_at = ?", row.CreatedAt).
		Where("tenant_id = ? AND sequence = ?", row.TenantID, head.Sequence).Exec(ctx)
	if err != nil {
		return Event{}, fmt.Errorf("advance audit head: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Event{}, errors.New("audit head changed concurrently")
	}
	return eventFromRow(row), nil
}

func (s *Service) List(ctx context.Context, filter Filter) ([]Event, int64, error) {
	if validateFilter(filter, true) != nil {
		return nil, 0, ErrInvalidFilter
	}
	query := s.db.NewSelect().Model((*eventRow)(nil)).Where("tenant_id = ?", filter.TenantID)
	query = applyFilter(query, filter)
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count audit events: %w", err)
	}
	var rows []eventRow
	if err := applyFilter(s.db.NewSelect().Model(&rows).Where("tenant_id = ?", filter.TenantID), filter).
		Order("created_at DESC", "id DESC").Offset(filter.Offset).Limit(filter.Limit).Scan(ctx); err != nil {
		return nil, 0, fmt.Errorf("list audit events: %w", err)
	}
	events := make([]Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, eventFromRow(row))
	}
	return events, int64(total), nil
}

func (s *Service) Get(ctx context.Context, tenantID, id string) (Event, error) {
	var row eventRow
	if err := s.db.NewSelect().Model(&row).Where("tenant_id = ? AND id = ?", tenantID, id).Scan(ctx); err != nil {
		return Event{}, err
	}
	return eventFromRow(row), nil
}

func (s *Service) Verify(ctx context.Context, tenantID string) (Integrity, error) {
	if strings.TrimSpace(tenantID) == "" || len(tenantID) > 128 {
		return Integrity{}, ErrInvalidFilter
	}
	head, found, err := s.readHead(ctx, tenantID)
	if err != nil {
		return Integrity{}, err
	}
	result := Integrity{TenantID: tenantID, Valid: true}
	if found {
		result, err = s.verifyHead(ctx, head)
		if err != nil {
			return Integrity{}, err
		}
	}
	if s.anchor != nil {
		status, err := s.verifyAnchors(ctx, tenantID)
		if err != nil {
			return Integrity{}, err
		}
		result.Anchor = status
	}
	return result, nil
}

// Anchor commits the current verified head into the external WORM store.
// Anchoring is idempotent: an already-committed head returns its anchor.
func (s *Service) Anchor(ctx context.Context, tenantID string) (Anchor, error) {
	if s.anchor == nil {
		return Anchor{}, ErrAnchorDisabled
	}
	if strings.TrimSpace(tenantID) == "" || len(tenantID) > 128 {
		return Anchor{}, ErrInvalidFilter
	}
	head, found, err := s.readHead(ctx, tenantID)
	if err != nil {
		return Anchor{}, err
	}
	if !found {
		return Anchor{}, ErrAnchorEmpty
	}
	integrity, err := s.verifyHead(ctx, head)
	if err != nil {
		return Anchor{}, err
	}
	if !integrity.Valid {
		return Anchor{}, ErrIntegrity
	}
	anchors, err := s.anchor.List(ctx, tenantID)
	if err != nil {
		return Anchor{}, anchorStoreError(err)
	}
	var latest *Anchor
	if len(anchors) > 0 {
		latest = &anchors[len(anchors)-1]
	}
	switch {
	case latest != nil && latest.HeadSequence > head.Sequence:
		return Anchor{}, ErrIntegrity // local chain rolled back behind its anchors
	case latest != nil && latest.HeadSequence == head.Sequence:
		return *latest, nil // already anchored
	}
	anchor := Anchor{
		Schema: anchorSchema, TenantID: tenantID, HeadSequence: head.Sequence,
		HeadHash: head.EventHash, AnchoredAt: s.now().UTC(),
	}
	if latest != nil {
		anchor.PreviousAnchorHash = latest.AnchorHash
	}
	anchor.AnchorHash = computeAnchorHash(anchor)
	if s.signer != nil {
		if err := s.signer.Sign(&anchor); err != nil {
			return Anchor{}, fmt.Errorf("sign audit root: %w", err)
		}
	}
	if err := s.anchor.Put(ctx, anchor); err != nil {
		putErr := err
		if errors.Is(putErr, ErrAnchorAlreadyExists) {
			// Lost a concurrent anchor race; the committed object is authoritative.
			anchors, listErr := s.anchor.List(ctx, tenantID)
			if listErr != nil {
				return Anchor{}, anchorStoreError(listErr)
			}
			if len(anchors) > 0 {
				return anchors[len(anchors)-1], nil
			}
		}
		return Anchor{}, anchorStoreError(putErr)
	}
	return anchor, nil
}

// SignRoot commits the current verified head and requires a root signature.
// An anchor already committed before signing was enabled cannot be
// retrofitted, so it fails closed instead of returning an unsigned root.
func (s *Service) SignRoot(ctx context.Context, tenantID string) (Anchor, error) {
	anchor, err := s.Anchor(ctx, tenantID)
	if err != nil {
		return Anchor{}, err
	}
	if anchor.RootSignature == "" {
		return Anchor{}, ErrRootSigningDisabled
	}
	return anchor, nil
}

func (s *Service) verifyAnchors(ctx context.Context, tenantID string) (AnchorStatus, error) {
	anchors, err := s.anchor.List(ctx, tenantID)
	if err != nil {
		return AnchorStatus{}, anchorStoreError(err)
	}
	status := AnchorStatus{Enabled: true, Valid: true}
	if len(anchors) == 0 {
		return status, nil
	}
	previous := ""
	for _, anchor := range anchors {
		if anchor.PreviousAnchorHash != previous || anchor.AnchorHash != computeAnchorHash(anchor) {
			status.Valid = false
			return status, nil
		}
		if anchor.RootSignature != "" {
			status.Signed = true
			if !VerifyRootSignature(anchor) {
				status.Valid = false
				return status, nil
			}
			status.SignatureValid = true
		}
		previous = anchor.AnchorHash
	}
	latest := anchors[len(anchors)-1]
	status.Sequence = latest.HeadSequence
	status.Hash = latest.AnchorHash
	status.AnchoredAt = latest.AnchoredAt
	chain, err := s.verifyPrefix(ctx, tenantID, latest.HeadSequence)
	if err != nil {
		return AnchorStatus{}, err
	}
	if !chain.Valid || chain.HeadHash != latest.HeadHash {
		status.Valid = false
	}
	return status, nil
}

func (s *Service) PrepareExport(ctx context.Context, filter Filter) (ExportSnapshot, error) {
	filter.Offset, filter.Limit = 0, 0
	if validateFilter(filter, false) != nil {
		return ExportSnapshot{}, ErrInvalidFilter
	}
	head, found, err := s.readHead(ctx, filter.TenantID)
	if err != nil {
		return ExportSnapshot{}, err
	}
	integrity := Integrity{TenantID: filter.TenantID, Valid: true}
	if found {
		integrity, err = s.verifyHead(ctx, head)
		if err != nil {
			return ExportSnapshot{}, err
		}
	}
	if !integrity.Valid {
		return ExportSnapshot{}, ErrIntegrity
	}
	return ExportSnapshot{Filter: filter, Integrity: integrity, GeneratedAt: s.now().UTC()}, nil
}

func (s *Service) WriteExport(ctx context.Context, snapshot ExportSnapshot, writer io.Writer) error {
	if writer == nil || validateFilter(snapshot.Filter, false) != nil || !snapshot.Integrity.Valid || snapshot.Integrity.TenantID != snapshot.Filter.TenantID {
		return ErrInvalidFilter
	}
	manifest := exportManifest{
		Type: "manifest", Schema: "chaosplus.audit-export.v1", TenantID: snapshot.Filter.TenantID,
		GeneratedAt: snapshot.GeneratedAt.UTC(), HeadSequence: snapshot.Integrity.HeadSequence,
		HeadHash: snapshot.Integrity.HeadHash, VerifiedEvents: snapshot.Integrity.VerifiedEvents,
		Filter: exportCriteriaOf(snapshot.Filter),
	}
	if err := writeJSONLine(writer, manifest); err != nil {
		return fmt.Errorf("write audit export manifest: %w", err)
	}

	contentHash := sha256.New()
	var cursor, exported int64
	for cursor < snapshot.Integrity.HeadSequence {
		var rows []eventRow
		query := s.db.NewSelect().Model(&rows).
			Where("tenant_id = ? AND sequence > ? AND sequence <= ?", snapshot.Filter.TenantID, cursor, snapshot.Integrity.HeadSequence)
		query = applyFilter(query, snapshot.Filter)
		if err := query.Order("sequence ASC").Limit(exportBatchSize).Scan(ctx); err != nil {
			return fmt.Errorf("read audit export: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			if row.Sequence <= cursor || row.EventHash != hash(row) {
				return ErrIntegrity
			}
			line, err := json.Marshal(exportEvent{Type: "event", Event: eventFromRow(row)})
			if err != nil {
				return fmt.Errorf("encode audit export event: %w", err)
			}
			if err := writeLine(writer, line); err != nil {
				return fmt.Errorf("write audit export event: %w", err)
			}
			_, _ = contentHash.Write(line)
			_, _ = contentHash.Write([]byte{'\n'})
			cursor = row.Sequence
			exported++
		}
	}
	complete := exportComplete{Type: "complete", ExportedEvents: exported, ContentSHA256: hex.EncodeToString(contentHash.Sum(nil))}
	if err := writeJSONLine(writer, complete); err != nil {
		return fmt.Errorf("write audit export completion: %w", err)
	}
	return nil
}

func (s *Service) readHead(ctx context.Context, tenantID string) (headRow, bool, error) {
	var head headRow
	err := s.db.NewSelect().Model(&head).Where("tenant_id = ?", tenantID).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return headRow{}, false, nil
	}
	if err != nil {
		return headRow{}, false, fmt.Errorf("read audit head: %w", err)
	}
	return head, true, nil
}

func (s *Service) verifyHead(ctx context.Context, head headRow) (Integrity, error) {
	result, err := s.verifyPrefix(ctx, head.TenantID, head.Sequence)
	if err != nil {
		return Integrity{}, err
	}
	result.Valid = result.Valid && result.HeadHash == head.EventHash
	result.HeadHash = head.EventHash
	return result, nil
}

func (s *Service) verifyPrefix(ctx context.Context, tenantID string, maxSequence int64) (Integrity, error) {
	var rows []eventRow
	if err := s.db.NewSelect().Model(&rows).
		Where("tenant_id = ? AND sequence > 0 AND sequence <= ?", tenantID, maxSequence).
		Order("sequence ASC").Scan(ctx); err != nil {
		return Integrity{}, err
	}
	result := Integrity{TenantID: tenantID, Valid: true, VerifiedEvents: int64(len(rows)), HeadSequence: maxSequence}
	previous := ""
	for index, row := range rows {
		if row.Sequence != int64(index+1) || row.PreviousHash != previous || row.EventHash != hash(row) {
			result.Valid = false
			return result, nil
		}
		previous = row.EventHash
	}
	result.HeadHash = previous
	result.Valid = result.Valid && maxSequence == int64(len(rows))
	return result, nil
}

func applyFilter(query *bun.SelectQuery, filter Filter) *bun.SelectQuery {
	if filter.PrincipalID != "" {
		query = query.Where("principal_id = ?", filter.PrincipalID)
	}
	if filter.EventType != "" {
		query = query.Where("event_type = ?", filter.EventType)
	}
	if filter.Outcome != "" {
		query = query.Where("outcome = ?", filter.Outcome)
	}
	if filter.TargetType != "" {
		query = query.Where("target_type = ?", filter.TargetType)
	}
	if filter.TargetID != "" {
		query = query.Where("target_id = ?", filter.TargetID)
	}
	if !filter.From.IsZero() {
		query = query.Where("created_at >= ?", filter.From.UTC().UnixMilli())
	}
	if !filter.To.IsZero() {
		query = query.Where("created_at < ?", filter.To.UTC().UnixMilli())
	}
	return query
}

func validateFilter(filter Filter, paginated bool) error {
	if strings.TrimSpace(filter.TenantID) == "" || len(filter.TenantID) > 128 || len(filter.PrincipalID) > 64 || len(filter.EventType) > 64 || len(filter.TargetType) > 64 || len(filter.TargetID) > 128 {
		return ErrInvalidFilter
	}
	if filter.Outcome != "" && filter.Outcome != "success" && filter.Outcome != "denied" && filter.Outcome != "failure" {
		return ErrInvalidFilter
	}
	if !filter.From.IsZero() && !filter.To.IsZero() && !filter.From.Before(filter.To) {
		return ErrInvalidFilter
	}
	if paginated && (filter.Offset < 0 || filter.Limit < 1 || filter.Limit > 200) {
		return ErrInvalidFilter
	}
	return nil
}

func exportCriteriaOf(filter Filter) exportCriteria {
	criteria := exportCriteria{PrincipalID: filter.PrincipalID, EventType: filter.EventType, Outcome: filter.Outcome, TargetType: filter.TargetType, TargetID: filter.TargetID}
	if !filter.From.IsZero() {
		criteria.From = filter.From.UTC().Format(time.RFC3339Nano)
	}
	if !filter.To.IsZero() {
		criteria.To = filter.To.UTC().Format(time.RFC3339Nano)
	}
	return criteria
}

func writeJSONLine(writer io.Writer, value any) error {
	line, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return writeLine(writer, line)
}

func writeLine(writer io.Writer, line []byte) error {
	written, err := writer.Write(append(line, '\n'))
	if err != nil {
		return err
	}
	if written != len(line)+1 {
		return io.ErrShortWrite
	}
	return nil
}

func validate(input EventInput) error {
	if strings.TrimSpace(input.TenantID) == "" || len(input.TenantID) > 128 || strings.TrimSpace(input.EventType) == "" || len(input.EventType) > 64 || (input.Outcome != "success" && input.Outcome != "denied" && input.Outcome != "failure") {
		return ErrInvalidEvent
	}
	if len(input.PrincipalID) > 64 || len(input.TargetType) > 64 || len(input.TargetID) > 128 || len(input.IPAddress) > 64 || len(input.UserAgent) > 512 {
		return ErrInvalidEvent
	}
	return nil
}

func hash(row eventRow) string {
	payload, _ := json.Marshal(struct {
		ID, TenantID, PrincipalID, EventType, TargetType, TargetID, Outcome, IPAddress, UserAgent, Detail string
		CreatedAt, Sequence                                                                               int64
		PreviousHash                                                                                      string
	}{row.ID, row.TenantID, row.PrincipalID, row.EventType, row.TargetType, row.TargetID, row.Outcome, row.IPAddress, row.UserAgent, row.Detail, row.CreatedAt, row.Sequence, row.PreviousHash})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func eventFromRow(row eventRow) Event {
	return Event{ID: row.ID, TenantID: row.TenantID, PrincipalID: row.PrincipalID, EventType: row.EventType, TargetType: row.TargetType, TargetID: row.TargetID, Outcome: row.Outcome, IPAddress: row.IPAddress, UserAgent: row.UserAgent, Detail: json.RawMessage(row.Detail), Sequence: row.Sequence, PreviousHash: row.PreviousHash, EventHash: row.EventHash, CreatedAt: time.UnixMilli(row.CreatedAt).UTC()}
}

func secureID() (string, error) {
	data := make([]byte, 18)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
