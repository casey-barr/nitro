// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package signer

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func issueSelfSignedLeaf(t *testing.T) (priv ed25519.PrivateKey, leafDER []byte) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	leafDER, err = x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return priv, leafDER
}

func writeCombinedPEM(t *testing.T, dir string, priv ed25519.PrivateKey, leafDER []byte) string {
	t.Helper()
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal PKCS8: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	path := filepath.Join(dir, "combined.pem")
	if err := os.WriteFile(path, append(keyPEM, certPEM...), 0o600); err != nil {
		t.Fatalf("write PEM: %v", err)
	}
	return path
}

func TestSigner_ReloadPicksUpNewCert(t *testing.T) {
	priv1, leafDER1 := issueSelfSignedLeaf(t)
	dir := t.TempDir()
	pemPath := writeCombinedPEM(t, dir, priv1, leafDER1)

	s, err := NewSigner(&Config{PEMFile: pemPath})
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	first := s.creds.Load().leafCert.Raw

	priv2, leafDER2 := issueSelfSignedLeaf(t)
	keyDER2, err := x509.MarshalPKCS8PrivateKey(priv2)
	if err != nil {
		t.Fatalf("marshal PKCS8: %v", err)
	}
	keyPEM2 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER2})
	certPEM2 := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER2})
	if err := os.WriteFile(pemPath, append(keyPEM2, certPEM2...), 0o600); err != nil {
		t.Fatalf("rewrite PEM: %v", err)
	}
	if err := s.loadConfig(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if bytes.Equal(first, s.creds.Load().leafCert.Raw) {
		t.Fatal("expected leafDER to change after reload")
	}
}

func TestSigner_ReloadKeepsOldOnParseError(t *testing.T) {
	priv, leafDER := issueSelfSignedLeaf(t)
	dir := t.TempDir()
	pemPath := writeCombinedPEM(t, dir, priv, leafDER)

	s, err := NewSigner(&Config{PEMFile: pemPath})
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	original := s.creds.Load()

	if err := os.WriteFile(pemPath, []byte("not a pem"), 0o600); err != nil {
		t.Fatalf("rewrite PEM: %v", err)
	}
	if err := s.loadConfig(); err == nil {
		t.Fatal("expected reload error on garbage PEM")
	}
	if s.creds.Load() != original {
		t.Fatal("expected credentials to be retained after parse failure")
	}
}
