//go:build windows

package credential

import (
	"bytes"
	"database/sql"
	"errors"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

func TestDPAPIRoundTripReplacementDeletionAndCorruptCiphertext(t *testing.T) {
	db, e := sql.Open("sqlite", filepath.Join(t.TempDir(), "vault.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(`CREATE TABLE ai_credentials(credential_id TEXT PRIMARY KEY,provider_id TEXT UNIQUE,encrypted_blob BLOB,created_at TEXT,updated_at TEXT)`); e != nil {
		t.Fatal(e)
	}
	v := Vault{DB: db}
	secret := []byte("local-fixture-secret-not-a-real-key")
	if e = v.StoreCredential("id", "provider", secret); e != nil {
		t.Fatal(e)
	}
	got, e := v.GetCredential("provider")
	if e != nil || !bytes.Equal(got, secret) {
		t.Fatalf("DPAPI roundtrip failed: %v", e)
	}
	Wipe(got)
	var blob []byte
	if e = db.QueryRow("SELECT encrypted_blob FROM ai_credentials").Scan(&blob); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(blob, secret) || bytes.Equal(blob, secret) {
		t.Fatal("plaintext found in ciphertext")
	}
	replacement := []byte("replacement-fixture")
	if e = v.UpdateCredential("id", "provider", replacement); e != nil {
		t.Fatal(e)
	}
	got, e = v.GetCredential("provider")
	if e != nil || !bytes.Equal(got, replacement) {
		t.Fatalf("replacement: %v", e)
	}
	Wipe(got)
	if _, e = db.Exec("UPDATE ai_credentials SET encrypted_blob=?", []byte("corrupt ciphertext")); e != nil {
		t.Fatal(e)
	}
	if _, e = v.GetCredential("provider"); !errors.Is(e, ErrDecrypt) {
		t.Fatalf("corrupt ciphertext must fail closed: %v", e)
	}
	has, e := v.HasCredential("provider")
	if e != nil || !has {
		t.Fatal("ciphertext presence should not pretend decryption succeeded")
	}
	if e = v.DeleteCredential("provider"); e != nil {
		t.Fatal(e)
	}
	has, e = v.HasCredential("provider")
	if e != nil || has {
		t.Fatal("credential deletion failed")
	}
	if _, e = v.GetCredential("provider"); !errors.Is(e, ErrUnavailable) {
		t.Fatal("deleted key readable")
	}
}
