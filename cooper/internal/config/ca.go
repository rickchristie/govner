package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const (
	// caDirName is the subdirectory within cooperDir that holds CA files.
	caDirName = "ca"
	// caCertFile is the PEM-encoded CA certificate filename.
	caCertFile = "cooper-ca.pem"
	// caKeyFile is the PEM-encoded CA private key filename.
	caKeyFile = "cooper-ca-key.pem"
	// caKeyBits is the RSA key size for the CA key.
	caKeyBits = 2048
	// caValidityYears is how long the CA certificate is valid.
	caValidityYears = 10
)

// EnsureCA generates a CA certificate and key if they don't already exist at
// {cooperDir}/ca/cooper-ca.pem and {cooperDir}/ca/cooper-ca-key.pem.
// Returns the paths to the cert and key files.
func EnsureCA(cooperDir string) (certPath string, keyPath string, err error) {
	certPath = filepath.Join(cooperDir, caDirName, caCertFile)
	keyPath = filepath.Join(cooperDir, caDirName, caKeyFile)

	if CAExists(cooperDir) {
		return certPath, keyPath, nil
	}

	return generateCA(cooperDir)
}

// RegenerateCA always regenerates the CA certificate and key, overwriting any
// existing files. Returns the paths to the new cert and key files.
func RegenerateCA(cooperDir string) (certPath string, keyPath string, err error) {
	return generateCA(cooperDir)
}

// CAExists returns true if both the CA certificate and key files exist at
// {cooperDir}/ca/cooper-ca.pem and {cooperDir}/ca/cooper-ca-key.pem.
func CAExists(cooperDir string) bool {
	certPath := filepath.Join(cooperDir, caDirName, caCertFile)
	keyPath := filepath.Join(cooperDir, caDirName, caKeyFile)

	if _, err := os.Stat(certPath); err != nil {
		return false
	}
	if _, err := os.Stat(keyPath); err != nil {
		return false
	}
	return true
}

// generateCA creates the ca/ directory if needed, generates an RSA 2048-bit key
// and a self-signed CA certificate, and writes them as PEM files.
func generateCA(cooperDir string) (certPath string, keyPath string, err error) {
	caDir := filepath.Join(cooperDir, caDirName)
	if err := os.MkdirAll(caDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create CA directory: %w", err)
	}

	certPath = filepath.Join(caDir, caCertFile)
	keyPath = filepath.Join(caDir, caKeyFile)

	// Generate RSA private key.
	key, err := rsa.GenerateKey(rand.Reader, caKeyBits)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate RSA key: %w", err)
	}

	// Generate a random serial number.
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate serial number: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Cooper Local CA",
			Organization: []string{"Cooper"},
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(caValidityYears, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	// Self-signed: parent is the same as the template.
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("failed to create CA certificate: %w", err)
	}

	// Write cert PEM (0644).
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return "", "", fmt.Errorf("failed to write CA certificate: %w", err)
	}

	// Write key PEM (0600).
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		// Clean up cert file on key write failure.
		os.Remove(certPath)
		return "", "", fmt.Errorf("failed to write CA key: %w", err)
	}

	return certPath, keyPath, nil
}
