package authn

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	authnext "github.com/chaos-plus/chaosplus/internal/core/extension/authn"
	"github.com/uptrace/bun"
)

type stepUpChallengeRow struct {
	bun.BaseModel `bun:"table:iam_stepup_challenges"`
	IDHash        string `bun:"id_hash,pk"`
	PrincipalID   string
	SessionHash   string
	CreatedAt     int64
	ExpiresAt     int64
	ConsumedAt    int64
	Attempts      int
}

// BeginStepUp starts a one-time MFA challenge bound to the current browser
// session. Completing it rotates the session with ACR 2 and a fresh auth_time.
func (s *WebService) BeginStepUp(ctx context.Context, authorization, cookieHeader string) (authnext.StepUpOptions, error) {
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return authnext.StepUpOptions{}, err
	}
	var credential credentialRow
	if err := s.db.NewSelect().Model(&credential).Where("principal_id = ? AND mfa_required = ?", claims.Subject, true).Scan(ctx); err != nil {
		return authnext.StepUpOptions{}, ErrMFANotEnabled
	}
	now := s.now().UTC()
	challengeID, err := randomToken(32)
	if err != nil {
		return authnext.StepUpOptions{}, err
	}
	row := stepUpChallengeRow{
		IDHash: tokenHash(challengeID), PrincipalID: claims.Subject, SessionHash: currentSessionHash(cookieHeader, s.web.CookieName),
		CreatedAt: now.UnixMilli(), ExpiresAt: now.Add(s.cfg.MFA.ChallengeTTL).UnixMilli(),
	}
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewDelete().Model((*stepUpChallengeRow)(nil)).Where("principal_id = ? AND (consumed_at <> 0 OR expires_at <= ?)", claims.Subject, now.UnixMilli()).Exec(ctx); err != nil {
			return err
		}
		_, err := tx.NewInsert().Model(&row).Exec(ctx)
		return err
	})
	if err != nil {
		return authnext.StepUpOptions{}, fmt.Errorf("create step-up challenge: %w", err)
	}
	s.audit(ctx, claims.Subject, "step_up_started", "success")
	return authnext.StepUpOptions{ChallengeID: challengeID, ExpiresAt: time.UnixMilli(row.ExpiresAt).UTC(), Methods: []string{"totp", "recovery_code"}}, nil
}

// VerifyStepUp consumes the step-up challenge and elevates the presenting
// session: the old session is revoked and a rotated one is issued with
// ACR 2, AMR including "mfa", and auth_time set to the verification time.
func (s *WebService) VerifyStepUp(ctx context.Context, authorization, cookieHeader, challengeID, code string) (authnext.StepUpResult, error) {
	claims, err := s.Authenticate(ctx, authorization, cookieHeader)
	if err != nil {
		return authnext.StepUpResult{}, err
	}
	if strings.TrimSpace(challengeID) == "" || strings.TrimSpace(code) == "" {
		return authnext.StepUpResult{}, ErrMFAChallenge
	}
	now := s.now().UTC()
	sessionHash := currentSessionHash(cookieHeader, s.web.CookieName)
	var result authnext.StepUpResult
	invalidCode := false
	err = s.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var challenge stepUpChallengeRow
		err := tx.NewSelect().Model(&challenge).Where("id_hash = ?", tokenHash(challengeID)).Scan(ctx)
		if err != nil || challenge.ConsumedAt != 0 || challenge.ExpiresAt <= now.UnixMilli() || challenge.Attempts >= s.cfg.MFA.MaxAttempts || challenge.PrincipalID != claims.Subject || challenge.SessionHash != sessionHash {
			return ErrMFAChallenge
		}
		var credential credentialRow
		if err := tx.NewSelect().Model(&credential).Where("principal_id = ? AND mfa_required = ?", claims.Subject, true).Scan(ctx); err != nil {
			return ErrMFAChallenge
		}
		valid, err := s.consumeFactor(ctx, tx, &credential, code, now)
		if err != nil {
			return err
		}
		if !valid {
			update := tx.NewUpdate().Model((*stepUpChallengeRow)(nil)).
				Set("attempts = attempts + 1")
			if challenge.Attempts+1 >= s.cfg.MFA.MaxAttempts {
				update = update.Set("consumed_at = ?", now.UnixMilli())
			}
			result, err := update.Where("id_hash = ? AND consumed_at = 0 AND attempts = ?", challenge.IDHash, challenge.Attempts).Exec(ctx)
			if err != nil {
				return err
			}
			if affected, _ := result.RowsAffected(); affected != 1 {
				return ErrMFAChallenge
			}
			invalidCode = true
			return nil
		}
		if _, err := tx.NewUpdate().Model((*stepUpChallengeRow)(nil)).Set("consumed_at = ?", now.UnixMilli()).Where("id_hash = ? AND consumed_at = 0", challenge.IDHash).Exec(ctx); err != nil {
			return err
		}
		sessionID, absoluteExpiry, err := s.elevateSession(ctx, tx, claims.Subject, sessionHash, now)
		if err != nil {
			return err
		}
		result = authnext.StepUpResult{SessionID: sessionID, ExpiresAt: time.UnixMilli(absoluteExpiry).UTC()}
		return s.appendSecurityAudit(ctx, tx, claims.Subject, "step_up_completed", "success")
	})
	if err != nil {
		return authnext.StepUpResult{}, err
	}
	if invalidCode {
		s.audit(ctx, claims.Subject, "step_up", "denied")
		return authnext.StepUpResult{}, ErrInvalidMFA
	}
	return result, nil
}

func (s *WebService) elevateSession(ctx context.Context, tx bun.Tx, principalID, sessionHash string, now time.Time) (string, int64, error) {
	var current sessionRow
	if err := tx.NewSelect().Model(&current).Where("id_hash = ? AND principal_id = ? AND revoked_at = 0", sessionHash, principalID).Scan(ctx); err != nil {
		return "", 0, ErrMFAChallenge
	}
	token, err := randomToken(32)
	if err != nil {
		return "", 0, err
	}
	amr := strings.Fields(current.AMR)
	if !slices.Contains(amr, "mfa") {
		amr = append(amr, "mfa")
	}
	row := sessionRow{
		IDHash: tokenHash(token), PrincipalID: principalID, CreatedAt: now.UnixMilli(), AuthTime: now.UnixMilli(),
		ACR: max(current.ACR, 2), AMR: strings.Join(amr, " "), LastSeenAt: now.UnixMilli(),
		ExpiresAt: now.Add(s.web.IdleTTL).UnixMilli(), AbsoluteExpiresAt: current.AbsoluteExpiresAt,
		IPAddress: current.IPAddress, UserAgent: current.UserAgent,
	}
	if _, err := tx.NewInsert().Model(&row).Exec(ctx); err != nil {
		return "", 0, fmt.Errorf("create step-up session: %w", err)
	}
	if _, err := tx.NewUpdate().Model((*sessionRow)(nil)).Set("revoked_at = ?", now.UnixMilli()).Where("id_hash = ? AND revoked_at = 0", sessionHash).Exec(ctx); err != nil {
		return "", 0, err
	}
	return token, current.AbsoluteExpiresAt, nil
}
