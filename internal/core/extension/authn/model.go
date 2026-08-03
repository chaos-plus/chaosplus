package authn

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrInvalidSession            = errors.New("invalid browser session")
	ErrSessionNotFound           = errors.New("browser session not found")
	ErrInvalidPassword           = errors.New("invalid password")
	ErrPasswordReused            = errors.New("password was used recently")
	ErrInvalidMFA                = errors.New("invalid multi-factor authentication code")
	ErrMFAChallenge              = errors.New("invalid or expired multi-factor authentication challenge")
	ErrMFAEnrollment             = errors.New("invalid or expired multi-factor authentication enrollment")
	ErrMFAAlreadyOn              = errors.New("multi-factor authentication is already enabled")
	ErrMFANotEnabled             = errors.New("multi-factor authentication is not enabled")
	ErrPasskeyDisabled           = errors.New("passkey authentication is disabled")
	ErrPasskeyChallenge          = errors.New("invalid or expired passkey challenge")
	ErrPasskeyCredential         = errors.New("invalid passkey credential")
	ErrPasskeyLimit              = errors.New("passkey credential limit reached")
	ErrPasskeyNotFound           = errors.New("passkey credential not found")
	ErrReturnURL                 = errors.New("return URL is not allowed")
	ErrRecoveryDisabled          = errors.New("password recovery is disabled")
	ErrInvalidRecovery           = errors.New("invalid or expired password recovery credential")
	ErrRecoveryCooldown          = errors.New("account is in the post-recovery cooldown period")
	ErrEmailVerificationDisabled = errors.New("email verification is disabled")
	ErrInvalidEmailVerification  = errors.New("invalid or expired email verification credential")
	ErrEmailRequired             = errors.New("primary email is required")
)

// LoginResult is a discriminated browser login result. A password-valid MFA
// account receives a challenge but no session cookie until VerifyLoginMFA.
type LoginResult struct {
	Status      string     `json:"status"`
	ReturnURL   string     `json:"return_url"`
	ChallengeID string     `json:"challenge_id,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Methods     []string   `json:"methods,omitempty"`
	SessionID   string     `json:"-"`
}

type PasskeyOptions struct {
	ChallengeID string          `json:"challenge_id"`
	ExpiresAt   time.Time       `json:"expires_at"`
	Options     json.RawMessage `json:"options"`
}

type Passkey struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	SignCount  uint32     `json:"sign_count"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

type MFAStatus struct {
	TOTPEnabled            bool `json:"totp_enabled"`
	RecoveryCodesRemaining int  `json:"recovery_codes_remaining"`
}

type MFAEnrollment struct {
	Secret          string    `json:"secret"`
	ProvisioningURI string    `json:"provisioning_uri"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type MFAConfirmation struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// BrowserSession is a revocable browser session visible to its owner.
type BrowserSession struct {
	ID                string    `json:"id"`
	Current           bool      `json:"current"`
	CreatedAt         time.Time `json:"created_at"`
	LastSeenAt        time.Time `json:"last_seen_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	AbsoluteExpiresAt time.Time `json:"absolute_expires_at"`
	IPAddress         string    `json:"ip_address,omitempty"`
	UserAgent         string    `json:"user_agent,omitempty"`
}
