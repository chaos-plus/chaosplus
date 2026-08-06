package audit

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	anchorSchema   = "chaosplus.audit-anchor.v1"
	anchorPrefix   = "audit/anchors"
	anchorLockMode = minio.Compliance
)

var (
	ErrAnchorDisabled      = errors.New("audit anchoring is not enabled")
	ErrAnchorEmpty         = errors.New("audit chain has no events to anchor")
	ErrAnchorMisconfigured = errors.New("audit anchoring requires endpoint, bucket, access key, secret key, and retention days")
	ErrAnchorAlreadyExists = errors.New("audit anchor already exists")
	ErrAnchorUnavailable   = errors.New("audit anchor store unavailable")
	ErrRootSigningDisabled = errors.New("audit root signing is not enabled")
	ErrInvalidSigningKey   = errors.New("audit root signing key must be a base64-encoded 32-byte Ed25519 seed")
)

// Anchor is a WORM commitment of one verified per-tenant audit head. The
// AnchorHash commits every other field; each anchor links to the previous one,
// so the anchor objects form their own immutable chain.
type Anchor struct {
	Schema             string    `json:"schema"`
	TenantID           string    `json:"tenant_id"`
	HeadSequence       int64     `json:"head_sequence"`
	HeadHash           string    `json:"head_hash"`
	AnchoredAt         time.Time `json:"anchored_at"`
	PreviousAnchorHash string    `json:"previous_anchor_hash,omitempty"`
	AnchorHash         string    `json:"anchor_hash"`
	SigningKeyID       string    `json:"signing_key_id,omitempty"`
	RootPublicKey      string    `json:"root_public_key,omitempty"`
	RootSignature      string    `json:"root_signature,omitempty"`
}

// AnchorStatus reports whether the external anchor chain exists and still
// matches the local audit chain.
type AnchorStatus struct {
	Enabled        bool      `json:"enabled"`
	Sequence       int64     `json:"sequence,omitempty"`
	Hash           string    `json:"hash,omitempty"`
	AnchoredAt     time.Time `json:"anchored_at,omitempty"`
	Signed         bool      `json:"signed"`
	SignatureValid bool      `json:"signature_valid,omitempty"`
	Valid          bool      `json:"valid"`
}

// AnchorStore writes and reads anchors on an S3-compatible endpoint using
// object lock retention, making each committed head immutable until the
// retention window expires.
type AnchorStore struct {
	client    *minio.Client
	bucket    string
	prefix    string
	region    string
	retention time.Duration
	ensure    sync.Once
	ensureErr error
}

func NewAnchorStore(cfg AnchorConfig) (*AnchorStore, error) {
	if !cfg.valid() {
		return nil, ErrAnchorMisconfigured
	}
	address, secure, err := endpointAddress(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	client, err := minio.New(address, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, fmt.Errorf("build anchor client: %w", err)
	}
	return &AnchorStore{
		client:    client,
		bucket:    cfg.Bucket,
		prefix:    anchorPrefix,
		region:    cfg.Region,
		retention: time.Duration(cfg.RetentionDays) * 24 * time.Hour,
	}, nil
}

// endpointAddress normalizes an S3 endpoint so both "127.0.0.1:9000" and
// "https://minio.example.com" are accepted, deriving TLS from the scheme.
func endpointAddress(endpoint string) (string, bool, error) {
	lower := strings.ToLower(endpoint)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return endpoint, false, nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return "", false, fmt.Errorf("invalid anchor endpoint %q", endpoint)
	}
	return parsed.Host, parsed.Scheme == "https", nil
}

func anchorStoreError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrAnchorUnavailable, err)
}

// ensureBucket creates the anchor bucket with object locking enabled when it
// does not exist yet, then proves the bucket really accepts retention by
// writing and removing a governance-locked probe object. MinIO (and S3) do not
// expose a usable "object locking enabled" query, so an actual retained write
// is the only honest capability check.
func (s *AnchorStore) ensureBucket(ctx context.Context) error {
	s.ensure.Do(func() {
		exists, err := s.client.BucketExists(ctx, s.bucket)
		if err != nil {
			s.ensureErr = fmt.Errorf("check anchor bucket: %w", err)
			return
		}
		if !exists {
			if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: s.region, ObjectLocking: true}); err != nil {
				s.ensureErr = fmt.Errorf("create anchor bucket %q: %w", s.bucket, err)
				return
			}
		}
		if err := s.probeObjectLock(ctx); err != nil {
			s.ensureErr = err
		}
	})
	return s.ensureErr
}

func (s *AnchorStore) probeObjectLock(ctx context.Context) error {
	key := s.prefix + "/_lock-probe"
	opts := minio.PutObjectOptions{
		Mode:            minio.Governance,
		RetainUntilDate: time.Now().UTC().Add(time.Minute),
	}
	if _, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(nil), 0, opts); err != nil {
		return fmt.Errorf("anchor bucket %q must support object locking: %w", s.bucket, err)
	}
	_ = s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{GovernanceBypass: true})
	return nil
}

func (s *AnchorStore) objectKey(tenantID string, sequence int64) string {
	return s.prefix + "/" + tenantID + "/" + strconv.FormatInt(sequence, 10) + ".json"
}

// Put writes one anchor with compliance retention. Write-once is enforced by
// rejecting an existing object; object lock makes deletion and overwrite
// impossible inside the retention window.
func (s *AnchorStore) Put(ctx context.Context, anchor Anchor) error {
	if err := s.ensureBucket(ctx); err != nil {
		return err
	}
	key := s.objectKey(anchor.TenantID, anchor.HeadSequence)
	if _, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{}); err == nil {
		return ErrAnchorAlreadyExists
	} else if minio.ToErrorResponse(err).Code != minio.NoSuchKey {
		return fmt.Errorf("stat anchor object: %w", err)
	}
	payload, err := json.Marshal(anchor)
	if err != nil {
		return fmt.Errorf("encode anchor: %w", err)
	}
	opts := minio.PutObjectOptions{
		ContentType:     "application/json",
		Mode:            anchorLockMode,
		RetainUntilDate: time.Now().UTC().Add(s.retention),
	}
	if _, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(payload), int64(len(payload)), opts); err != nil {
		return fmt.Errorf("put anchor object: %w", err)
	}
	return nil
}

// List returns every anchor for a tenant, oldest first.
func (s *AnchorStore) List(ctx context.Context, tenantID string) ([]Anchor, error) {
	if err := s.ensureBucket(ctx); err != nil {
		return nil, err
	}
	var anchors []Anchor
	for object := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: s.prefix + "/" + tenantID + "/", Recursive: true}) {
		if object.Err != nil {
			return nil, fmt.Errorf("list anchor objects: %w", object.Err)
		}
		anchor, ok, err := readAnchorObject(ctx, s.client, s.bucket, object.Key)
		if err != nil {
			return nil, err
		}
		if ok {
			anchors = append(anchors, anchor)
		}
	}
	sort.Slice(anchors, func(i, j int) bool { return anchors[i].HeadSequence < anchors[j].HeadSequence })
	return anchors, nil
}

func readAnchorObject(ctx context.Context, client *minio.Client, bucket, key string) (Anchor, bool, error) {
	name := key[strings.LastIndexByte(key, '/')+1:]
	if !strings.HasSuffix(name, ".json") {
		return Anchor{}, false, nil
	}
	sequence, ok := parseAnchorSequence(name)
	if !ok {
		return Anchor{}, false, nil // not one of our anchors
	}
	object, err := client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return Anchor{}, false, fmt.Errorf("read anchor object %q: %w", key, err)
	}
	payload, err := io.ReadAll(object)
	closeErr := object.Close()
	if err != nil {
		return Anchor{}, false, fmt.Errorf("read anchor object %q: %w", key, err)
	}
	if closeErr != nil {
		return Anchor{}, false, fmt.Errorf("close anchor object %q: %w", key, closeErr)
	}
	var anchor Anchor
	if err := json.Unmarshal(payload, &anchor); err != nil {
		return Anchor{}, false, fmt.Errorf("decode anchor object %q: %w", key, err)
	}
	if anchor.Schema != anchorSchema || anchor.HeadSequence != sequence {
		return Anchor{}, false, nil
	}
	return anchor, true, nil
}

func parseAnchorSequence(name string) (int64, bool) {
	sequence, err := strconv.ParseInt(strings.TrimSuffix(name, ".json"), 10, 64)
	return sequence, err == nil
}

func computeAnchorHash(anchor Anchor) string {
	payload, _ := json.Marshal(struct {
		Schema, TenantID, HeadHash, PreviousAnchorHash string
		HeadSequence                                   int64
		AnchoredAt                                     time.Time
	}{anchor.Schema, anchor.TenantID, anchor.HeadHash, anchor.PreviousAnchorHash, anchor.HeadSequence, anchor.AnchoredAt.UTC()})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// RootSigner signs audit anchor roots with Ed25519 so every committed head
// carries an offline-verifiable root commitment. The public key is embedded in
// the anchor itself, so anchors remain verifiable without key distribution.
type RootSigner struct {
	privateKey ed25519.PrivateKey
	keyID      string
}

// NewRootSigner builds a signer from a base64-encoded 32-byte Ed25519 seed.
// The key ID is the first 8 bytes of the SHA-256 of the public key.
func NewRootSigner(seedBase64 string) (*RootSigner, error) {
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(seedBase64))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, ErrInvalidSigningKey
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	digest := sha256.Sum256(publicKey)
	return &RootSigner{privateKey: privateKey, keyID: hex.EncodeToString(digest[:8])}, nil
}

// Sign commits the anchor's hash. The signature is stored alongside the public
// key inside the anchor object, making each anchor self-verifying.
func (s *RootSigner) Sign(anchor *Anchor) error {
	digest, err := hex.DecodeString(anchor.AnchorHash)
	if err != nil || len(digest) != sha256.Size {
		return errors.New("audit anchor hash is not a valid SHA-256 digest")
	}
	anchor.SigningKeyID = s.keyID
	anchor.RootPublicKey = base64.StdEncoding.EncodeToString(s.privateKey.Public().(ed25519.PublicKey))
	anchor.RootSignature = base64.StdEncoding.EncodeToString(ed25519.Sign(s.privateKey, digest))
	return nil
}

// VerifyRootSignature validates the embedded Ed25519 signature over the
// anchor hash. Unsigned anchors (no signature present) are not valid signatures.
func VerifyRootSignature(anchor Anchor) bool {
	if anchor.RootSignature == "" || anchor.RootPublicKey == "" {
		return false
	}
	signature, err := base64.StdEncoding.DecodeString(anchor.RootSignature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return false
	}
	publicKey, err := base64.StdEncoding.DecodeString(anchor.RootPublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return false
	}
	digest, err := hex.DecodeString(anchor.AnchorHash)
	if err != nil || len(digest) != sha256.Size {
		return false
	}
	return ed25519.Verify(publicKey, digest, signature)
}
