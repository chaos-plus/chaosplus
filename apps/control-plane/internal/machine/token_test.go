package machine

import (
	"testing"
	"time"
)

func TestTokenLifecycle(t *testing.T) {
	ts := NewTokenStore()
	at := ts.Issue("m1")

	// 一次性 token 有效。
	v, err := ts.Validate(at.Token)
	if err != nil || v.MachineID != "m1" || v.LongTerm {
		t.Fatalf("validate = %+v, %v", v, err)
	}
	// 格式 xxxx.xxxx.xxxxxxxxx。
	if len(at.Token) != 4+1+4+1+9 {
		t.Fatalf("token format wrong: %q", at.Token)
	}

	// 伪造 token 拒绝。
	if _, err := ts.Validate("garbage.token.value"); err == nil {
		t.Fatal("garbage token should be invalid")
	}

	// confirm → 长期化,仍可验证。
	if err := ts.MakeLongTerm("m1", at.Token); err != nil {
		t.Fatalf("make long term: %v", err)
	}
	if v, _ := ts.Validate(at.Token); !v.LongTerm {
		t.Fatal("token should now be long-term")
	}

	// Invalidate 后失效。
	ts.Invalidate("m1")
	if _, err := ts.Validate(at.Token); err == nil {
		t.Fatal("token should be invalid after Invalidate")
	}
}

func TestTokenExpiry(t *testing.T) {
	ts := NewTokenStore()
	at := ts.Issue("m1")
	// 手动把过期时间拨到过去。
	ts.mu.Lock()
	ts.byHash[hashToken(at.Token)].ExpiresAt = time.Now().Add(-time.Minute)
	ts.mu.Unlock()

	if _, err := ts.Validate(at.Token); err != ErrTokenExpired {
		t.Fatalf("expired token error = %v, want ErrTokenExpired", err)
	}
}

func TestMakeLongTermWrongMachine(t *testing.T) {
	ts := NewTokenStore()
	at := ts.Issue("m1")
	if err := ts.MakeLongTerm("m2", at.Token); err == nil {
		t.Fatal("promoting with wrong machineID should error")
	}
}
