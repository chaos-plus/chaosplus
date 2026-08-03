// Package webauthnx isolates the WebAuthn protocol library from IAM services.
package webauthnx

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

var ErrInvalidCredential = errors.New("invalid WebAuthn credential")

type Config struct {
	RPID        string
	DisplayName string
	Origins     []string
}

type Adapter struct {
	protocol *webauthn.WebAuthn
}

type User struct {
	id          []byte
	name        string
	displayName string
	credentials []webauthn.Credential
}

type Credential struct {
	ID           []byte
	Data         []byte
	SignCount    uint32
	CloneWarning bool
}

func New(cfg Config) (*Adapter, error) {
	instance, err := webauthn.New(&webauthn.Config{
		RPID:                  cfg.RPID,
		RPDisplayName:         cfg.DisplayName,
		RPOrigins:             cfg.Origins,
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		},
	})
	if err != nil {
		return nil, err
	}
	return &Adapter{protocol: instance}, nil
}

func NewUser(id []byte, name, displayName string, storedCredentials [][]byte) (User, error) {
	user := User{id: append([]byte(nil), id...), name: name, displayName: displayName}
	user.credentials = make([]webauthn.Credential, 0, len(storedCredentials))
	for _, data := range storedCredentials {
		var credential webauthn.Credential
		if err := json.Unmarshal(data, &credential); err != nil {
			return User{}, ErrInvalidCredential
		}
		user.credentials = append(user.credentials, credential)
	}
	return user, nil
}

func (u User) WebAuthnID() []byte                         { return u.id }
func (u User) WebAuthnName() string                       { return u.name }
func (u User) WebAuthnDisplayName() string                { return u.displayName }
func (u User) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func (a *Adapter) BeginRegistration(user User, expires time.Time) (options, session []byte, err error) {
	creation, state, err := a.protocol.BeginRegistration(user)
	if err != nil {
		return nil, nil, err
	}
	state.Expires = expires
	options, err = json.Marshal(creation)
	if err != nil {
		return nil, nil, err
	}
	session, err = json.Marshal(state)
	return options, session, err
}

func (a *Adapter) FinishRegistration(user User, session, response []byte) (Credential, error) {
	state, err := decodeSession(session)
	if err != nil {
		return Credential{}, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
	if err != nil {
		return Credential{}, ErrInvalidCredential
	}
	credential, err := a.protocol.CreateCredential(user, state, parsed)
	if err != nil {
		return Credential{}, ErrInvalidCredential
	}
	return encodeCredential(credential)
}

func (a *Adapter) BeginLogin(expires time.Time) (options, session []byte, err error) {
	assertion, state, err := a.protocol.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationRequired),
	)
	if err != nil {
		return nil, nil, err
	}
	state.Expires = expires
	options, err = json.Marshal(assertion)
	if err != nil {
		return nil, nil, err
	}
	session, err = json.Marshal(state)
	return options, session, err
}

func (a *Adapter) FinishLogin(session, response []byte, lookup func(credentialID, userHandle []byte) (User, error)) (User, Credential, error) {
	state, err := decodeSession(session)
	if err != nil {
		return User{}, Credential{}, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		return User{}, Credential{}, ErrInvalidCredential
	}
	var resolved User
	_, credential, err := a.protocol.ValidatePasskeyLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		resolved, err = lookup(rawID, userHandle)
		if err != nil {
			return nil, err
		}
		return resolved, nil
	}, state, parsed)
	if err != nil {
		return User{}, Credential{}, ErrInvalidCredential
	}
	encoded, err := encodeCredential(credential)
	return resolved, encoded, err
}

func decodeSession(data []byte) (webauthn.SessionData, error) {
	var session webauthn.SessionData
	if err := json.Unmarshal(data, &session); err != nil {
		return session, ErrInvalidCredential
	}
	return session, nil
}

func encodeCredential(credential *webauthn.Credential) (Credential, error) {
	data, err := json.Marshal(credential)
	if err != nil {
		return Credential{}, err
	}
	return Credential{
		ID:           append([]byte(nil), credential.ID...),
		Data:         data,
		SignCount:    credential.Authenticator.SignCount,
		CloneWarning: credential.Authenticator.CloneWarning,
	}, nil
}
