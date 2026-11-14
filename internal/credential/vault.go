// Package credential stores only OS-protected ciphertext. Plaintext is never exported to the UI.
package credential

import (
	"database/sql"
	"errors"
	"time"
)

var ErrUnavailable = errors.New("CREDENTIAL_UNAVAILABLE")
var ErrDecrypt = errors.New("CREDENTIAL_DECRYPT_FAILED")

type CredentialVault interface {
	StoreCredential(string, string, []byte) error
	GetCredential(string) ([]byte, error)
	UpdateCredential(string, string, []byte) error
	DeleteCredential(string) error
	HasCredential(string) (bool, error)
}
type Database interface {
	Exec(string, ...any) (sql.Result, error)
	QueryRow(string, ...any) *sql.Row
}
type Vault struct{ DB Database }

// Protect encrypts a bounded secret with the current operating-system account.
func Protect(secret []byte) ([]byte, error) {
	if len(secret) == 0 || len(secret) > 16384 {
		return nil, ErrUnavailable
	}
	blob, err := protect(secret)
	if err != nil {
		return nil, ErrUnavailable
	}
	return blob, nil
}

// Unprotect decrypts a stored secret for the current operating-system account.
// The caller owns the returned buffer and should wipe it after use.
func Unprotect(blob []byte) ([]byte, error) {
	value, err := unprotect(blob)
	if err != nil {
		return nil, ErrDecrypt
	}
	return value, nil
}

func (v Vault) StoreCredential(id, provider string, secret []byte) error {
	blob, e := Protect(secret)
	if e != nil {
		return e
	}
	defer Wipe(blob)
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	_, e = v.DB.Exec(`INSERT INTO ai_credentials(credential_id,provider_id,encrypted_blob,created_at,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(provider_id) DO UPDATE SET encrypted_blob=excluded.encrypted_blob,updated_at=excluded.updated_at`, id, provider, blob, stamp, stamp)
	return e
}
func (v Vault) UpdateCredential(id, provider string, secret []byte) error {
	return v.StoreCredential(id, provider, secret)
}
func (v Vault) GetCredential(provider string) ([]byte, error) {
	var blob []byte
	if e := v.DB.QueryRow("SELECT encrypted_blob FROM ai_credentials WHERE provider_id=?", provider).Scan(&blob); e != nil {
		return nil, ErrUnavailable
	}
	defer Wipe(blob)
	return Unprotect(blob)
}
func (v Vault) DeleteCredential(provider string) error {
	_, e := v.DB.Exec("DELETE FROM ai_credentials WHERE provider_id=?", provider)
	return e
}
func (v Vault) HasCredential(provider string) (bool, error) {
	var n int
	e := v.DB.QueryRow("SELECT COUNT(*) FROM ai_credentials WHERE provider_id=?", provider).Scan(&n)
	return n > 0, e
}
func Wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
