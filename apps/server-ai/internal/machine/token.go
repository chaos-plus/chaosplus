// Package machine implements PRD §5.3.1: machine runner onboarding tokens and
// the WS daemon hub.
package machine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// TokenTTL is the one-time onboarding token lifetime (PRD §5.3.1: 300s ±5s).
const TokenTTL = 5 * time.Minute

var (
	ErrTokenInvalid = errors.New("invalid token")
	ErrTokenExpired = errors.New("token expired")
)

// AccessToken is one issued runner token. LongTerm tokens (post-confirm) never
// expire and are used for reconnects.
type AccessToken struct {
	Token     string
	MachineID string
	LongTerm  bool
	ExpiresAt time.Time
}

// TokenStore holds issued tokens in memory. Only the sha256 hash is retained,
// so a leaked db dump never reveals raw tokens (§17.2).
type TokenStore struct {
	mu        sync.Mutex
	byHash    map[string]*AccessToken
	byMachine map[string]map[string]struct{}
}

func NewTokenStore() *TokenStore {
	return &TokenStore{byHash: make(map[string]*AccessToken), byMachine: make(map[string]map[string]struct{})}
}

// Issue creates a one-time token valid for TokenTTL.
func (t *TokenStore) Issue(machineID string) AccessToken {
	at := AccessToken{Token: randomToken(), MachineID: machineID, ExpiresAt: time.Now().Add(TokenTTL)}
	key := hashToken(at.Token)
	t.mu.Lock()
	t.byHash[key] = &at
	if t.byMachine[machineID] == nil {
		t.byMachine[machineID] = make(map[string]struct{})
	}
	t.byMachine[machineID][key] = struct{}{}
	t.mu.Unlock()
	return at
}

// Validate returns the token record if valid: an unexpired one-time token or a
// long-term token. Expired one-time tokens are evicted.
func (t *TokenStore) Validate(token string) (*AccessToken, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := hashToken(token)
	at, ok := t.byHash[key]
	if !ok {
		return nil, ErrTokenInvalid
	}
	if !at.LongTerm && time.Now().After(at.ExpiresAt) {
		delete(t.byHash, key)
		delete(t.byMachine[at.MachineID], key)
		return nil, ErrTokenExpired
	}
	copy := *at
	return &copy, nil
}

// IssueLongTerm creates a long-lived token for an already-confirmed machine
// (manual rotation only — never expires).
func (t *TokenStore) IssueLongTerm(machineID string) AccessToken {
	at := AccessToken{Token: randomToken(), MachineID: machineID, LongTerm: true}
	key := hashToken(at.Token)
	t.mu.Lock()
	t.byHash[key] = &at
	if t.byMachine[machineID] == nil {
		t.byMachine[machineID] = make(map[string]struct{})
	}
	t.byMachine[machineID][key] = struct{}{}
	t.mu.Unlock()
	return at
}

// MakeLongTerm promotes a valid one-time token to long-term (confirm flow).
func (t *TokenStore) MakeLongTerm(machineID, token string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := hashToken(token)
	at, ok := t.byHash[key]
	if !ok {
		return ErrTokenInvalid
	}
	if !at.LongTerm && time.Now().After(at.ExpiresAt) {
		delete(t.byHash, key)
		delete(t.byMachine[at.MachineID], key)
		return ErrTokenExpired
	}
	if at.MachineID != machineID {
		return ErrTokenInvalid
	}
	at.LongTerm = true
	at.ExpiresAt = time.Time{}
	return nil
}

// Rehydrate 把 DB 持久化的长期 token hash 回灌进内存 store:控制面重启后
// 已确认机器的长期 token 仍可验证(否则内存 store 为空,daemon 重连全挂)。
func (t *TokenStore) Rehydrate(machineID, tokenHash string) {
	if tokenHash == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.byHash[tokenHash]; exists {
		return
	}
	t.byHash[tokenHash] = &AccessToken{MachineID: machineID, LongTerm: true}
	if t.byMachine[machineID] == nil {
		t.byMachine[machineID] = make(map[string]struct{})
	}
	t.byMachine[machineID][tokenHash] = struct{}{}
}

// GetToken 只读返回某机器当前在内存里的原始 token(用于展示接入命令),
// 不轮换、不失效、不踢守护进程。控制面重启后回灌的 token 只有 hash,
// 原始值不在内存 → 返回 ErrTokenInvalid,调用方应走轮换重新生成。
func (t *TokenStore) GetToken(machineID string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key := range t.byMachine[machineID] {
		if at := t.byHash[key]; at != nil && at.Token != "" {
			return at.Token, nil
		}
	}
	return "", ErrTokenInvalid
}

// Invalidate revokes all tokens for a machine (cancel / timeout / force-offline).
func (t *TokenStore) Invalidate(machineID string) {
	t.mu.Lock()
	for key := range t.byMachine[machineID] {
		delete(t.byHash, key)
	}
	delete(t.byMachine, machineID)
	t.mu.Unlock()
}

func hashToken(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// randomToken returns a 256-bit opaque bearer token as unpadded base64url. This
// matches PRD F.7 while remaining shell/URL safe for the onboarding command.
func randomToken() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
