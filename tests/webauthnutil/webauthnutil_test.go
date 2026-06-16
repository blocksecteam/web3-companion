// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package webauthnutil_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/blocksecteam/web3-companion/internal/webauthnutil"
)

func TestParseAttestationObjectAndVerifyAssertion(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	credentialID := []byte("credential-123")
	rpID := "localhost"
	attestationObject := testAttestationObject(t, key, credentialID, rpID, 7)

	att, err := webauthnutil.ParseAttestationObject(attestationObject, rpID)
	if err != nil {
		t.Fatalf("ParseAttestationObject: %v", err)
	}
	if att.CredentialID != base64.RawURLEncoding.EncodeToString(credentialID) {
		t.Fatalf("credential ID = %q", att.CredentialID)
	}
	if att.SignCount != 7 {
		t.Fatalf("sign count = %d, want 7", att.SignCount)
	}
	if _, err := x509.ParsePKIXPublicKey(att.PublicKeyDER); err != nil {
		t.Fatalf("stored public key is not SPKI: %v", err)
	}

	challenge := "challenge-assertion"
	clientData := testClientData(t, "webauthn.get", challenge, "http://localhost:8080")
	authData := testAuthenticatorDataEncoded(rpID, 8, false)
	signature := testAssertionSignature(t, key, clientData, authData)

	signCount, err := webauthnutil.VerifyAssertion(att.PublicKeyDER, clientData, authData, signature, challenge, "http://localhost:8080", rpID)
	if err != nil {
		t.Fatalf("VerifyAssertion: %v", err)
	}
	if signCount != 8 {
		t.Fatalf("signCount = %d, want 8", signCount)
	}
}

func TestParseAttestationObjectRejectsWrongRPID(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	attestationObject := testAttestationObject(t, key, []byte("credential-123"), "localhost", 1)
	if _, err := webauthnutil.ParseAttestationObject(attestationObject, "example.com"); err == nil {
		t.Fatal("expected rpIdHash mismatch")
	}
}

func TestVerifyAssertionRejectsMissingUserVerification(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal pubkey: %v", err)
	}
	challenge := "challenge-assertion"
	clientData := testClientData(t, "webauthn.get", challenge, "http://localhost:8080")
	authData := testAuthenticatorDataEncoded("localhost", 2, true)
	signature := testAssertionSignature(t, key, clientData, authData)

	if _, err := webauthnutil.VerifyAssertion(pubDER, clientData, authData, signature, challenge, "http://localhost:8080", "localhost"); err == nil {
		t.Fatal("expected missing user verification to fail")
	}
}

func testClientData(t *testing.T, typ, challenge, origin string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"type": typ, "challenge": challenge, "origin": origin})
	if err != nil {
		t.Fatalf("marshal clientData: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func testAttestationObject(t *testing.T, key *ecdsa.PrivateKey, credentialID []byte, rpID string, signCount uint32) string {
	t.Helper()
	authData := testAuthenticatorDataRaw(rpID, signCount, false)
	authData[32] |= 0x40
	authData = append(authData, make([]byte, 16)...)
	l := make([]byte, 2)
	binary.BigEndian.PutUint16(l, uint16(len(credentialID)))
	authData = append(authData, l...)
	authData = append(authData, credentialID...)
	authData = append(authData, testCOSEEC2Key(t, key)...)

	raw := cborMap([][]byte{
		cborText("fmt"), cborText("none"),
		cborText("authData"), cborBytes(authData),
		cborText("attStmt"), cborMap(nil),
	})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func testAuthenticatorDataEncoded(rpID string, signCount uint32, missingUV bool) string {
	return base64.RawURLEncoding.EncodeToString(testAuthenticatorDataRaw(rpID, signCount, missingUV))
}

func testAuthenticatorDataRaw(rpID string, signCount uint32, missingUV bool) []byte {
	sum := sha256.Sum256([]byte(rpID))
	authData := append([]byte{}, sum[:]...)
	flags := byte(0x01 | 0x04)
	if missingUV {
		flags = 0x01
	}
	authData = append(authData, flags)
	count := make([]byte, 4)
	binary.BigEndian.PutUint32(count, signCount)
	authData = append(authData, count...)
	return authData
}

func testAssertionSignature(t *testing.T, key *ecdsa.PrivateKey, clientDataEncoded, authDataEncoded string) string {
	t.Helper()
	clientDataRaw, err := base64.RawURLEncoding.DecodeString(clientDataEncoded)
	if err != nil {
		t.Fatalf("decode clientData: %v", err)
	}
	authDataRaw, err := base64.RawURLEncoding.DecodeString(authDataEncoded)
	if err != nil {
		t.Fatalf("decode authData: %v", err)
	}
	clientHash := sha256.Sum256(clientDataRaw)
	signed := append(append([]byte{}, authDataRaw...), clientHash[:]...)
	digest := sha256.Sum256(signed)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(sig)
}

func testCOSEEC2Key(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	x := key.X.FillBytes(make([]byte, 32))
	y := key.Y.FillBytes(make([]byte, 32))
	return cborMap([][]byte{
		cborInt(1), cborInt(2),
		cborInt(3), cborInt(-7),
		cborInt(-1), cborInt(1),
		cborInt(-2), cborBytes(x),
		cborInt(-3), cborBytes(y),
	})
}

func cborMap(items [][]byte) []byte {
	out := cborLen(5, uint64(len(items)/2))
	for _, item := range items {
		out = append(out, item...)
	}
	return out
}

func cborText(s string) []byte {
	out := cborLen(3, uint64(len(s)))
	return append(out, []byte(s)...)
}

func cborBytes(b []byte) []byte {
	out := cborLen(2, uint64(len(b)))
	return append(out, b...)
}

func cborInt(v int64) []byte {
	if v >= 0 {
		return cborLen(0, uint64(v))
	}
	return cborLen(1, uint64(-1-v))
}

func cborLen(major byte, n uint64) []byte {
	prefix := major << 5
	switch {
	case n < 24:
		return []byte{prefix | byte(n)}
	case n <= 0xff:
		return []byte{prefix | 24, byte(n)}
	case n <= 0xffff:
		return []byte{prefix | 25, byte(n >> 8), byte(n)}
	case n <= 0xffffffff:
		return []byte{prefix | 26, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
	default:
		return []byte{prefix | 27, byte(n >> 56), byte(n >> 48), byte(n >> 40), byte(n >> 32), byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
	}
}
