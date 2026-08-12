package machine

import (
	"testing"
	"time"

	coreid "github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func TestTokenLifecycle(t *testing.T) {
	ts := NewTokenStore()
	machineID := coreid.ID(101)
	at := ts.Issue(machineID, 201, 301, 401)

	// 一次性 token 有效。
	v, err := ts.Validate(at.Token)
	if err != nil || v.MachineID != machineID || v.LongTerm {
		t.Fatalf("validate = %+v, %v", v, err)
	}
	// 格式 xxxx.xxxx.xxxx.xxxx.xxxx.xxxx(24 字符 + 5 点)。
	if len(at.Token) != 43 {
		t.Fatalf("token format wrong: %q", at.Token)
	}

	// 伪造 token 拒绝。
	if _, err := ts.Validate("garbage.token.value"); err == nil {
		t.Fatal("garbage token should be invalid")
	}

	// confirm → 长期化,仍可验证。
	if err := ts.MakeLongTerm(machineID, at.Token); err != nil {
		t.Fatalf("make long term: %v", err)
	}
	if v, _ := ts.Validate(at.Token); !v.LongTerm {
		t.Fatal("token should now be long-term")
	}

	// Invalidate 后失效。
	ts.Invalidate(machineID)
	if _, err := ts.Validate(at.Token); err == nil {
		t.Fatal("token should be invalid after Invalidate")
	}
}

func TestTokenExpiry(t *testing.T) {
	ts := NewTokenStore()
	at := ts.Issue(101, 201, 301, 401)
	// 手动把过期时间拨到过去。
	ts.mu.Lock()
	ts.byHash[hashToken(at.Token)].ExpiresAt = time.Now().Add(-time.Minute)
	ts.mu.Unlock()

	if _, err := ts.Validate(at.Token); err != ErrTokenExpired {
		t.Fatalf("expired token error = %v, want ErrTokenExpired", err)
	}
}

func TestTokenRehydrate(t *testing.T) {
	ts := NewTokenStore()
	ts.Rehydrate(101, 201, 301, 401, hashToken("saved-long-token"))
	v, err := ts.Validate("saved-long-token")
	if err != nil || v.MachineID != coreid.ID(101) || !v.LongTerm {
		t.Fatalf("rehydrated token validate = %+v, %v", v, err)
	}
	// 空 hash 直接忽略,不 panic、不占位。
	ts.Rehydrate(102, 201, 301, 401, "")
	if _, err := ts.Validate("saved-long-token"); err != nil {
		t.Fatalf("existing token must survive an empty rehydrate: %v", err)
	}
}

func TestMakeLongTermWrongMachine(t *testing.T) {
	ts := NewTokenStore()
	at := ts.Issue(101, 201, 301, 401)
	if err := ts.MakeLongTerm(102, at.Token); err == nil {
		t.Fatal("promoting with wrong machineID should error")
	}
}
