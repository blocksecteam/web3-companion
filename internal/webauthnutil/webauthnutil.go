// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package webauthnutil

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
)

type ClientData struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin"`
}

type AttestationCredential struct {
	CredentialID string
	PublicKeyDER []byte
	SignCount    uint32
	AAGUID       string
	UserPresent  bool
	UserVerified bool
}

func DecodeBase64URL(s string) ([]byte, error) {
	if s == "" {
		return nil, fmt.Errorf("empty base64url value")
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

func ValidateClientData(encoded, expectedType, expectedChallenge, expectedOrigin string) ([]byte, *ClientData, error) {
	raw, err := DecodeBase64URL(encoded)
	if err != nil {
		return nil, nil, fmt.Errorf("decode clientDataJSON: %w", err)
	}
	var cd ClientData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return nil, nil, fmt.Errorf("parse clientDataJSON: %w", err)
	}
	if cd.Type != expectedType {
		return nil, nil, fmt.Errorf("unexpected WebAuthn type %q", cd.Type)
	}
	if cd.Challenge != expectedChallenge {
		return nil, nil, fmt.Errorf("challenge mismatch")
	}
	if expectedOrigin != "" && cd.Origin != expectedOrigin {
		return nil, nil, fmt.Errorf("origin mismatch")
	}
	return raw, &cd, nil
}

func ParsePublicKey(der []byte) (crypto.PublicKey, error) {
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse SPKI public key: %w", err)
	}
	switch pub.(type) {
	case *ecdsa.PublicKey, *rsa.PublicKey:
		return pub, nil
	default:
		return nil, fmt.Errorf("unsupported WebAuthn public key type %T", pub)
	}
}

func VerifyAssertion(publicKeyDER []byte, clientDataJSON, authenticatorData, signature, expectedChallenge, expectedOrigin, expectedRPID string) (uint32, error) {
	clientDataRaw, _, err := ValidateClientData(clientDataJSON, "webauthn.get", expectedChallenge, expectedOrigin)
	if err != nil {
		return 0, err
	}
	authData, err := DecodeBase64URL(authenticatorData)
	if err != nil {
		return 0, fmt.Errorf("decode authenticatorData: %w", err)
	}
	if len(authData) < 37 {
		return 0, fmt.Errorf("authenticatorData too short")
	}
	if expectedRPID == "" {
		expectedRPID = rpIDFromOrigin(expectedOrigin)
	}
	if expectedRPID != "" {
		expected := sha256.Sum256([]byte(expectedRPID))
		if string(authData[:32]) != string(expected[:]) {
			return 0, fmt.Errorf("rpIdHash mismatch")
		}
	}
	flags := authData[32]
	if flags&0x01 == 0 {
		return 0, fmt.Errorf("user presence flag not set")
	}
	if flags&0x04 == 0 {
		return 0, fmt.Errorf("user verification flag not set")
	}
	sig, err := DecodeBase64URL(signature)
	if err != nil {
		return 0, fmt.Errorf("decode signature: %w", err)
	}
	pub, err := ParsePublicKey(publicKeyDER)
	if err != nil {
		return 0, err
	}
	clientHash := sha256.Sum256(clientDataRaw)
	signed := append(append([]byte{}, authData...), clientHash[:]...)
	signedHash := sha256.Sum256(signed)
	switch p := pub.(type) {
	case *ecdsa.PublicKey:
		if !ecdsa.VerifyASN1(p, signedHash[:], sig) {
			return 0, fmt.Errorf("invalid ECDSA assertion signature")
		}
	case *rsa.PublicKey:
		if err := rsa.VerifyPKCS1v15(p, crypto.SHA256, signedHash[:], sig); err != nil {
			return 0, fmt.Errorf("invalid RSA assertion signature: %w", err)
		}
	default:
		return 0, fmt.Errorf("unsupported public key type %T", p)
	}
	return binary.BigEndian.Uint32(authData[33:37]), nil
}

func rpIDFromOrigin(origin string) string {
	if origin == "" {
		return ""
	}
	u, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func ParseAttestationObject(encoded, rpID string) (*AttestationCredential, error) {
	raw, err := DecodeBase64URL(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode attestationObject: %w", err)
	}
	authData, err := readAttestationAuthData(raw)
	if err != nil {
		return nil, err
	}
	if len(authData) < 37 {
		return nil, fmt.Errorf("authenticatorData too short")
	}
	if rpID != "" {
		expected := sha256.Sum256([]byte(rpID))
		if string(authData[:32]) != string(expected[:]) {
			return nil, fmt.Errorf("rpIdHash mismatch")
		}
	}
	flags := authData[32]
	if flags&0x01 == 0 {
		return nil, fmt.Errorf("user presence flag not set")
	}
	if flags&0x04 == 0 {
		return nil, fmt.Errorf("user verification flag not set")
	}
	if flags&0x40 == 0 {
		return nil, fmt.Errorf("attested credential data missing")
	}
	offset := 37
	if len(authData) < offset+18 {
		return nil, fmt.Errorf("attested credential data too short")
	}
	aaguid := authData[offset : offset+16]
	offset += 16
	credLen := int(binary.BigEndian.Uint16(authData[offset : offset+2]))
	offset += 2
	if len(authData) < offset+credLen {
		return nil, fmt.Errorf("credential ID truncated")
	}
	credentialID := authData[offset : offset+credLen]
	offset += credLen
	if len(authData) <= offset {
		return nil, fmt.Errorf("credential public key missing")
	}
	pubDER, err := coseKeyToSPKI(authData[offset:])
	if err != nil {
		return nil, err
	}
	return &AttestationCredential{
		CredentialID: base64.RawURLEncoding.EncodeToString(credentialID),
		PublicKeyDER: pubDER,
		SignCount:    binary.BigEndian.Uint32(authData[33:37]),
		AAGUID:       fmt.Sprintf("%x-%x-%x-%x-%x", aaguid[0:4], aaguid[4:6], aaguid[6:8], aaguid[8:10], aaguid[10:16]),
		UserPresent:  true,
		UserVerified: true,
	}, nil
}

func readAttestationAuthData(raw []byte) ([]byte, error) {
	r := &cborReader{data: raw}
	major, n, err := r.readHeader()
	if err != nil {
		return nil, fmt.Errorf("parse attestationObject: %w", err)
	}
	if major != 5 {
		return nil, fmt.Errorf("attestationObject must be a CBOR map")
	}
	var authData []byte
	for i := uint64(0); i < n; i++ {
		key, err := r.readText()
		if err != nil {
			return nil, fmt.Errorf("attestationObject key: %w", err)
		}
		if key == "authData" {
			authData, err = r.readBytes()
			if err != nil {
				return nil, fmt.Errorf("authData: %w", err)
			}
			continue
		}
		if err := r.skip(); err != nil {
			return nil, err
		}
	}
	if len(authData) == 0 {
		return nil, fmt.Errorf("attestationObject missing authData")
	}
	return authData, nil
}

func coseKeyToSPKI(raw []byte) ([]byte, error) {
	r := &cborReader{data: raw}
	major, n, err := r.readHeader()
	if err != nil {
		return nil, fmt.Errorf("parse COSE key: %w", err)
	}
	if major != 5 {
		return nil, fmt.Errorf("COSE key must be a map")
	}
	ints := map[int64]int64{}
	bytesVals := map[int64][]byte{}
	for i := uint64(0); i < n; i++ {
		key, err := r.readInt()
		if err != nil {
			return nil, fmt.Errorf("COSE key label: %w", err)
		}
		valueMajor, _, err := r.peekHeader()
		if err != nil {
			return nil, err
		}
		switch valueMajor {
		case 0, 1:
			v, err := r.readInt()
			if err != nil {
				return nil, err
			}
			ints[key] = v
		case 2:
			v, err := r.readBytes()
			if err != nil {
				return nil, err
			}
			bytesVals[key] = v
		default:
			if err := r.skip(); err != nil {
				return nil, err
			}
		}
	}
	switch ints[1] {
	case 2:
		if ints[-1] != 1 {
			return nil, fmt.Errorf("unsupported COSE EC curve %d", ints[-1])
		}
		x, y := bytesVals[-2], bytesVals[-3]
		if len(x) == 0 || len(y) == 0 {
			return nil, fmt.Errorf("COSE EC key missing coordinates")
		}
		pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		if !pub.Curve.IsOnCurve(pub.X, pub.Y) {
			return nil, fmt.Errorf("COSE EC key point is not on P-256")
		}
		return x509.MarshalPKIXPublicKey(pub)
	case 3:
		nBytes, eBytes := bytesVals[-1], bytesVals[-2]
		if len(nBytes) == 0 || len(eBytes) == 0 {
			return nil, fmt.Errorf("COSE RSA key missing modulus or exponent")
		}
		pub := &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: int(new(big.Int).SetBytes(eBytes).Int64())}
		if pub.E <= 1 {
			return nil, fmt.Errorf("invalid COSE RSA exponent")
		}
		return x509.MarshalPKIXPublicKey(pub)
	default:
		return nil, fmt.Errorf("unsupported COSE key type %d", ints[1])
	}
}

type cborReader struct {
	data []byte
	pos  int
}

func (r *cborReader) peekHeader() (byte, uint64, error) {
	pos := r.pos
	major, n, err := r.readHeader()
	r.pos = pos
	return major, n, err
}

func (r *cborReader) readHeader() (byte, uint64, error) {
	if r.pos >= len(r.data) {
		return 0, 0, fmt.Errorf("unexpected end of CBOR data")
	}
	b := r.data[r.pos]
	r.pos++
	major := b >> 5
	add := b & 0x1f
	switch {
	case add < 24:
		return major, uint64(add), nil
	case add == 24:
		if r.pos+1 > len(r.data) {
			return 0, 0, fmt.Errorf("truncated CBOR uint8")
		}
		v := uint64(r.data[r.pos])
		r.pos++
		return major, v, nil
	case add == 25:
		if r.pos+2 > len(r.data) {
			return 0, 0, fmt.Errorf("truncated CBOR uint16")
		}
		v := uint64(binary.BigEndian.Uint16(r.data[r.pos : r.pos+2]))
		r.pos += 2
		return major, v, nil
	case add == 26:
		if r.pos+4 > len(r.data) {
			return 0, 0, fmt.Errorf("truncated CBOR uint32")
		}
		v := uint64(binary.BigEndian.Uint32(r.data[r.pos : r.pos+4]))
		r.pos += 4
		return major, v, nil
	case add == 27:
		if r.pos+8 > len(r.data) {
			return 0, 0, fmt.Errorf("truncated CBOR uint64")
		}
		v := binary.BigEndian.Uint64(r.data[r.pos : r.pos+8])
		r.pos += 8
		return major, v, nil
	default:
		return 0, 0, fmt.Errorf("indefinite CBOR lengths are not supported")
	}
}

func (r *cborReader) readText() (string, error) {
	major, n, err := r.readHeader()
	if err != nil {
		return "", err
	}
	if major != 3 {
		return "", fmt.Errorf("expected CBOR text string")
	}
	if r.pos+int(n) > len(r.data) {
		return "", fmt.Errorf("truncated CBOR text string")
	}
	s := string(r.data[r.pos : r.pos+int(n)])
	r.pos += int(n)
	return s, nil
}

func (r *cborReader) readBytes() ([]byte, error) {
	major, n, err := r.readHeader()
	if err != nil {
		return nil, err
	}
	if major != 2 {
		return nil, fmt.Errorf("expected CBOR byte string")
	}
	if r.pos+int(n) > len(r.data) {
		return nil, fmt.Errorf("truncated CBOR byte string")
	}
	b := r.data[r.pos : r.pos+int(n)]
	r.pos += int(n)
	return b, nil
}

func (r *cborReader) readInt() (int64, error) {
	major, n, err := r.readHeader()
	if err != nil {
		return 0, err
	}
	switch major {
	case 0:
		if n > uint64(^uint64(0)>>1) {
			return 0, fmt.Errorf("CBOR unsigned integer overflows int64")
		}
		return int64(n), nil
	case 1:
		if n > uint64(^uint64(0)>>1) {
			return 0, fmt.Errorf("CBOR negative integer overflows int64")
		}
		return -1 - int64(n), nil
	default:
		return 0, fmt.Errorf("expected CBOR integer")
	}
}

func (r *cborReader) skip() error {
	major, n, err := r.readHeader()
	if err != nil {
		return err
	}
	switch major {
	case 0, 1, 7:
		return nil
	case 2, 3:
		if r.pos+int(n) > len(r.data) {
			return fmt.Errorf("truncated CBOR item")
		}
		r.pos += int(n)
		return nil
	case 4:
		for i := uint64(0); i < n; i++ {
			if err := r.skip(); err != nil {
				return err
			}
		}
		return nil
	case 5:
		for i := uint64(0); i < n*2; i++ {
			if err := r.skip(); err != nil {
				return err
			}
		}
		return nil
	case 6:
		return r.skip()
	default:
		return fmt.Errorf("unsupported CBOR major type %d", major)
	}
}
