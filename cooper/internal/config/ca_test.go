package config

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCAGenerateAndVerifyFiles(t *testing.T) {
	dir := t.TempDir()

	certPath, keyPath, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA failed: %v", err)
	}

	// Verify files exist.
	certInfo, err := os.Stat(certPath)
	if err != nil {
		t.Fatalf("cert file not found: %v", err)
	}
	keyInfo, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file not found: %v", err)
	}

	// Verify permissions.
	if got := certInfo.Mode().Perm(); got != 0644 {
		t.Errorf("cert permissions = %o, want 0644", got)
	}
	if got := keyInfo.Mode().Perm(); got != 0600 {
		t.Errorf("key permissions = %o, want 0600", got)
	}

	// Verify paths are in the expected location.
	expectedCert := filepath.Join(dir, "ca", "cooper-ca.pem")
	expectedKey := filepath.Join(dir, "ca", "cooper-ca-key.pem")
	if certPath != expectedCert {
		t.Errorf("certPath = %q, want %q", certPath, expectedCert)
	}
	if keyPath != expectedKey {
		t.Errorf("keyPath = %q, want %q", keyPath, expectedKey)
	}
}

func TestCAVerifyCertIsValidX509(t *testing.T) {
	dir := t.TempDir()

	certPath, _, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA failed: %v", err)
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("failed to read cert: %v", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("failed to decode PEM block")
	}
	if block.Type != "CERTIFICATE" {
		t.Errorf("PEM type = %q, want CERTIFICATE", block.Type)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse x509 certificate: %v", err)
	}

	// Verify it's a CA.
	if !cert.IsCA {
		t.Error("certificate IsCA = false, want true")
	}
	if !cert.BasicConstraintsValid {
		t.Error("certificate BasicConstraintsValid = false, want true")
	}

	// Verify subject.
	if cert.Subject.CommonName != "Cooper Local CA" {
		t.Errorf("CN = %q, want %q", cert.Subject.CommonName, "Cooper Local CA")
	}
	if len(cert.Subject.Organization) != 1 || cert.Subject.Organization[0] != "Cooper" {
		t.Errorf("O = %v, want [Cooper]", cert.Subject.Organization)
	}

	// Verify key usage.
	if cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("KeyUsage missing CertSign")
	}
	if cert.KeyUsage&x509.KeyUsageCRLSign == 0 {
		t.Error("KeyUsage missing CRLSign")
	}

	// Verify validity period is approximately 10 years.
	duration := cert.NotAfter.Sub(cert.NotBefore)
	minDays := 365*10 - 2
	maxDays := 365*10 + 3 // account for leap years
	days := int(duration.Hours() / 24)
	if days < minDays || days > maxDays {
		t.Errorf("validity = %d days, want approximately %d days (10 years)", days, 365*10)
	}

	// Verify NotBefore is recent (within the last minute).
	if time.Since(cert.NotBefore) > time.Minute {
		t.Errorf("NotBefore = %v, expected within last minute", cert.NotBefore)
	}
}

func TestCAEnsureDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()

	// First generation.
	certPath, _, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("first EnsureCA failed: %v", err)
	}

	firstInfo, err := os.Stat(certPath)
	if err != nil {
		t.Fatalf("stat cert: %v", err)
	}
	firstModTime := firstInfo.ModTime()

	// Small sleep to ensure filesystem timestamp would differ.
	time.Sleep(50 * time.Millisecond)

	// Second call should not overwrite.
	certPath2, _, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("second EnsureCA failed: %v", err)
	}
	if certPath2 != certPath {
		t.Errorf("second EnsureCA returned different path: %q vs %q", certPath2, certPath)
	}

	secondInfo, err := os.Stat(certPath)
	if err != nil {
		t.Fatalf("stat cert after second call: %v", err)
	}

	if !secondInfo.ModTime().Equal(firstModTime) {
		t.Errorf("EnsureCA overwrote existing cert: modtime changed from %v to %v",
			firstModTime, secondInfo.ModTime())
	}
}

func TestCARegenerateOverwrites(t *testing.T) {
	dir := t.TempDir()

	// First generation.
	certPath, _, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA failed: %v", err)
	}

	firstCertPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}

	// Small sleep to ensure filesystem timestamp would differ.
	time.Sleep(50 * time.Millisecond)

	// Regenerate should overwrite.
	certPath2, _, err := RegenerateCA(dir)
	if err != nil {
		t.Fatalf("RegenerateCA failed: %v", err)
	}
	if certPath2 != certPath {
		t.Errorf("RegenerateCA returned different path: %q vs %q", certPath2, certPath)
	}

	secondCertPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert after regeneration: %v", err)
	}

	// The cert content should differ because a new key and serial are generated.
	if string(firstCertPEM) == string(secondCertPEM) {
		t.Error("RegenerateCA did not produce a different certificate")
	}
}

func TestCAExistsEmptyDir(t *testing.T) {
	dir := t.TempDir()

	if CAExists(dir) {
		t.Error("CAExists returned true for empty directory")
	}
}

func TestCAExistsAfterGeneration(t *testing.T) {
	dir := t.TempDir()

	if _, _, err := EnsureCA(dir); err != nil {
		t.Fatalf("EnsureCA failed: %v", err)
	}

	if !CAExists(dir) {
		t.Error("CAExists returned false after EnsureCA")
	}
}

func TestCAExistsPartialFiles(t *testing.T) {
	dir := t.TempDir()

	// Create ca/ dir with only the cert file (no key).
	caDir := filepath.Join(dir, "ca")
	if err := os.MkdirAll(caDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caDir, "cooper-ca.pem"), []byte("fake"), 0644); err != nil {
		t.Fatal(err)
	}

	if CAExists(dir) {
		t.Error("CAExists returned true when only cert file exists (no key)")
	}

	// Now create only the key file (no cert).
	dir2 := t.TempDir()
	caDir2 := filepath.Join(dir2, "ca")
	if err := os.MkdirAll(caDir2, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(caDir2, "cooper-ca-key.pem"), []byte("fake"), 0600); err != nil {
		t.Fatal(err)
	}

	if CAExists(dir2) {
		t.Error("CAExists returned true when only key file exists (no cert)")
	}
}

func TestCAKeyPEMIsValid(t *testing.T) {
	dir := t.TempDir()

	_, keyPath, err := EnsureCA(dir)
	if err != nil {
		t.Fatalf("EnsureCA failed: %v", err)
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed to read key: %v", err)
	}

	block, _ := pem.Decode(keyPEM)
	if block == nil {
		t.Fatal("failed to decode key PEM block")
	}
	if block.Type != "RSA PRIVATE KEY" {
		t.Errorf("key PEM type = %q, want RSA PRIVATE KEY", block.Type)
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse RSA private key: %v", err)
	}

	if key.N.BitLen() != 2048 {
		t.Errorf("key size = %d bits, want 2048", key.N.BitLen())
	}
}
