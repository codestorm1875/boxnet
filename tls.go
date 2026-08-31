package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type tlsMaterial struct {
	certificate tls.Certificate
	leaf        *x509.Certificate
	ca          *x509.Certificate
	roots       *x509.CertPool
}

func createTLSMaterial(dataDir, address string, security *nodeSecurity) (*tlsMaterial, error) {
	caPrivate := deriveP256Key(security.meshKey, "boxnet/tls/ca/v2")
	caPublicDER, err := x509.MarshalPKIXPublicKey(&caPrivate.PublicKey)
	if err != nil {
		return nil, err
	}
	caFingerprint := sha256.Sum256(caPublicDER)
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{Organization: []string{"Boxnet"}, CommonName: "Boxnet Mesh " + hex.EncodeToString(caFingerprint[:6])},
		NotBefore:             time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2045, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		SubjectKeyId:          caFingerprint[:20],
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caPrivate.PublicKey, caPrivate)
	if err != nil {
		return nil, err
	}
	caCertificate, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dataDir, "mesh-ca.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o644); err != nil {
		return nil, err
	}

	dnsNames := []string{"localhost"}
	ipAddresses := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	if parsed, parseErr := url.Parse(address); parseErr == nil && parsed.Hostname() != "" {
		if ip := net.ParseIP(parsed.Hostname()); ip != nil {
			ipAddresses = appendUniqueIP(ipAddresses, ip)
		} else if parsed.Hostname() != "localhost" {
			dnsNames = append(dnsNames, parsed.Hostname())
		}
	}
	identityHash := sha256.Sum256(security.publicKey)
	leafPrivate := deriveP256Key(security.privateKey.Seed(), "boxnet/tls/node/v2")
	serial := new(big.Int).SetBytes(identityHash[:16])
	leafTemplate := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{Organization: []string{"Boxnet"}, CommonName: nodeID(security.publicKey)},
		NotBefore:    time.Now().Add(-24 * time.Hour),
		NotAfter:     time.Now().AddDate(2, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     dnsNames,
		IPAddresses:  ipAddresses,
		SubjectKeyId: identityHash[:20],
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, caCertificate, &leafPrivate.PublicKey, caPrivate)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil {
		return nil, err
	}
	certificate := tls.Certificate{Certificate: [][]byte{leafDER, caDER}, PrivateKey: leafPrivate, Leaf: leaf}
	roots := x509.NewCertPool()
	roots.AddCert(caCertificate)
	return &tlsMaterial{certificate: certificate, leaf: leaf, ca: caCertificate, roots: roots}, nil
}

func deriveP256Key(secret []byte, label string) *ecdsa.PrivateKey {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(label))
	curve := elliptic.P256()
	orderMinusOne := new(big.Int).Sub(curve.Params().N, big.NewInt(1))
	d := new(big.Int).SetBytes(mac.Sum(nil))
	d.Mod(d, orderMinusOne)
	d.Add(d, big.NewInt(1))
	x, y := curve.ScalarBaseMult(d.Bytes())
	return &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y}, D: d}
}

func appendUniqueIP(values []net.IP, candidate net.IP) []net.IP {
	for _, value := range values {
		if value.Equal(candidate) {
			return values
		}
	}
	return append(values, candidate)
}

func (n *Node) serverTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{n.tlsCertificate},
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    n.tlsRoots,
	}
}

func (n *Node) verifyTLSPeer(request *http.Request) bool {
	if request.TLS == nil || len(request.TLS.PeerCertificates) == 0 {
		return false
	}
	leaf := request.TLS.PeerCertificates[0]
	if leaf.Subject.CommonName != request.Header.Get("X-Boxnet-ID") {
		return false
	}
	intermediates := x509.NewCertPool()
	for _, certificate := range request.TLS.PeerCertificates[1:] {
		intermediates.AddCert(certificate)
	}
	_, err := leaf.Verify(x509.VerifyOptions{Roots: n.tlsRoots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	return err == nil
}

func requireHTTPS(address string) (string, error) {
	validated, err := validatePeerURL(address)
	if err != nil {
		return "", err
	}
	parsed, _ := url.Parse(validated)
	if parsed.Scheme != "https" {
		return "", errors.New("peer address must use https")
	}
	return validated, nil
}
