// Copyright (c) 2026 BlockSec (https://blocksec.com).
// Licensed under the MIT License.

package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/blocksecteam/web3-companion/internal/config"

	authpkg "github.com/blocksecteam/web3-companion/internal/auth"
	"github.com/blocksecteam/web3-companion/internal/passkey"
	"github.com/blocksecteam/web3-companion/internal/ssm/vault"
	"github.com/blocksecteam/web3-companion/internal/storage"
	"github.com/blocksecteam/web3-companion/tests/testutil"
)

func setupAuthRouter(t *testing.T) *gin.Engine {
	t.Helper()
	db := testutil.NewTestDB(t)
	r := gin.New()
	authpkg.RegisterRoutes(r, db, nil, authpkg.WithTokenService(authpkg.NewTokenService(db, []byte(authpkg.TestJWTSecret))))
	return r
}

func setupAuthRouterWithPasskeys(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t)
	cfg := &config.WebAuthnConfig{
		RPID:                       "localhost",
		RPName:                     "test",
		RPOrigin:                   "http://localhost",
		RegistrationTimeoutSeconds: 300,
		ChallengeTimeoutSeconds:    120,
	}
	r := gin.New()
	authpkg.RegisterRoutes(r, db, passkey.NewServer(db, cfg), authpkg.WithTokenService(authpkg.NewTokenService(db, []byte(authpkg.TestJWTSecret))))
	return r, db
}

func TestRegisterIssuesPasskeySetupTokenDirectly(t *testing.T) {
	r, db := setupAuthRouterWithPasskeys(t)

	before := time.Now()
	w := testutil.DoRequest(r, "POST", "/auth/register", map[string]string{
		"email": "passkey-user@example.com",
	}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("register = %d: %s", w.Code, w.Body.String())
	}

	var registerResp map[string]interface{}
	testutil.ParseJSON(t, w, &registerResp)
	if got := registerResp["status"]; got != "pending_passkey" {
		t.Fatalf("status = %v, want pending_passkey", got)
	}
	userID, _ := registerResp["user_id"].(string)
	tempToken, _ := registerResp["temp_token"].(string)
	if userID == "" || tempToken == "" {
		t.Fatalf("register response missing user_id/temp_token: %#v", registerResp)
	}
	var user storage.User
	if err := db.Where("id = ?", userID).First(&user).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if user.TempToken != tempToken || user.TempTokenExpiresAt == nil {
		t.Fatalf("stored temp token mismatch: token=%q expires=%v", user.TempToken, user.TempTokenExpiresAt)
	}
	if got := user.TempTokenExpiresAt.Sub(before); got < 14*time.Minute || got > 16*time.Minute {
		t.Fatalf("temp token ttl = %s, want about 15m", got)
	}
}

func TestRegisterCreatesDefaultManualPolicyDefinition(t *testing.T) {
	db := testutil.NewTestDB(t)
	r := gin.New()
	v := vault.New(vault.NewTestKMSClient(), db, t.TempDir(), nil, nil)
	authpkg.RegisterRoutes(r, db, nil, authpkg.WithVault(v), authpkg.WithTokenService(authpkg.NewTokenService(db, []byte(authpkg.TestJWTSecret))))

	w := testutil.DoRequest(r, "POST", "/auth/register", map[string]string{
		"email": "default-policy@example.com",
	}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("register = %d: %s", w.Code, w.Body.String())
	}
	var registerResp map[string]interface{}
	testutil.ParseJSON(t, w, &registerResp)

	var def storage.PolicyDefinition
	if err := db.Where("user_id = ? AND is_default = ? AND deleted_at IS NULL", registerResp["user_id"], true).First(&def).Error; err != nil {
		t.Fatalf("load default policy definition: %v", err)
	}
	if def.Name != "default" || def.CurrentVersionID == "" {
		t.Fatalf("unexpected default definition: %+v", def)
	}

	var version storage.PolicyDefinitionVersion
	if err := db.First(&version, "policy_definition_version_id = ?", def.CurrentVersionID).Error; err != nil {
		t.Fatalf("load default policy version: %v", err)
	}
	if version.Mode != "manual" || version.AutoSignEnabled {
		t.Fatalf("default policy version should be manual with auto-sign disabled: %+v", version)
	}

	var wallet storage.Wallet
	if err := db.Where("user_id = ? AND wallet_id = ? AND deleted_at IS NULL", registerResp["user_id"], "default").First(&wallet).Error; err != nil {
		t.Fatalf("load default wallet: %v", err)
	}
	if !wallet.IsDefault {
		t.Fatalf("registration wallet should be default: %+v", wallet)
	}

	var instance storage.PolicyInstance
	if err := db.Where("user_id = ? AND wallet_id = ?", registerResp["user_id"], "default").First(&instance).Error; err != nil {
		t.Fatalf("load default wallet policy binding: %v", err)
	}
	if instance.PolicyDefinitionID != def.PolicyDefinitionID || instance.PolicyDefinitionVersionID != def.CurrentVersionID {
		t.Fatalf("default wallet should be bound to default policy: %+v", instance)
	}
}

func TestRegisterCreatesDefaultWalletForEachUser(t *testing.T) {
	db := testutil.NewTestDB(t)
	r := gin.New()
	v := vault.New(vault.NewTestKMSClient(), db, t.TempDir(), nil, nil)
	authpkg.RegisterRoutes(r, db, nil, authpkg.WithVault(v), authpkg.WithTokenService(authpkg.NewTokenService(db, []byte(authpkg.TestJWTSecret))))

	for _, email := range []string{"default-wallet-one@example.com", "default-wallet-two@example.com"} {
		w := testutil.DoRequest(r, "POST", "/auth/register", map[string]string{
			"email": email,
		}, nil)
		if w.Code != http.StatusCreated {
			t.Fatalf("register %s = %d: %s", email, w.Code, w.Body.String())
		}
	}

	var wallets []storage.Wallet
	if err := db.Where("wallet_id = ? AND deleted_at IS NULL", "default").Order("user_id ASC").Find(&wallets).Error; err != nil {
		t.Fatalf("load default wallets: %v", err)
	}
	if len(wallets) != 2 {
		t.Fatalf("default wallet count = %d, want 2: %+v", len(wallets), wallets)
	}
	for _, wallet := range wallets {
		if !wallet.IsDefault || wallet.UserID == "" || wallet.Address == "" {
			t.Fatalf("unexpected default wallet: %+v", wallet)
		}
	}
}

func TestPasskeySetupRejectsExpiredTempToken(t *testing.T) {
	r, db := setupAuthRouterWithPasskeys(t)
	expired := time.Now().Add(-time.Minute)
	user := storage.User{
		ID:                 "user_expired_temp_token",
		Email:              "expired-temp@example.com",
		Name:               "expired-temp@example.com",
		TempToken:          "expired-temp-token",
		TempTokenExpiresAt: &expired,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	w := testutil.DoRequest(r, "POST", "/auth/passkey-setup", map[string]string{
		"temp_token": user.TempToken,
	}, nil)
	if w.Code != http.StatusGone {
		t.Fatalf("expired temp token setup = %d, want 410: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	testutil.ParseJSON(t, w, &resp)
	errObj, _ := resp["error"].(map[string]interface{})
	if errObj["code"] != "temp_token_expired" {
		t.Fatalf("error code = %v, want temp_token_expired; response=%#v", errObj["code"], resp)
	}
}

func TestRegisterUnfinishedExistingAccountReissuesTempToken(t *testing.T) {
	r, db := setupAuthRouterWithPasskeys(t)

	w := testutil.DoRequest(r, "POST", "/auth/register", map[string]string{
		"email": "unfinished@example.com",
	}, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("first register = %d: %s", w.Code, w.Body.String())
	}
	var first map[string]interface{}
	testutil.ParseJSON(t, w, &first)

	w = testutil.DoRequest(r, "POST", "/auth/register", map[string]string{
		"email": "unfinished@example.com",
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("second register = %d, want 200: %s", w.Code, w.Body.String())
	}
	var second map[string]interface{}
	testutil.ParseJSON(t, w, &second)
	if second["status"] != "pending_passkey" || second["user_id"] != first["user_id"] || second["temp_token"] == "" {
		t.Fatalf("unexpected second register response: %#v", second)
	}
	if second["temp_token"] == first["temp_token"] {
		t.Fatalf("second registration should rotate temp token: first=%#v second=%#v", first, second)
	}

	var count int64
	if err := db.Model(&storage.User{}).Where("email = ?", "unfinished@example.com").Count(&count).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("user count = %d, want 1", count)
	}
}

func TestConfirmURLPrefersFrontendConfirmRoute(t *testing.T) {
	authpkg.SetExternalURL("http://localhost:5173")
	t.Cleanup(func() { authpkg.SetExternalURL("") })
	t.Setenv("WEB3_CONFIRM_FALLBACK", "")

	got := authpkg.ConfirmURL("challenge=ch_confirm_frontend")
	want := "http://localhost:5173/confirm/ch_confirm_frontend"
	if got != want {
		t.Fatalf("ConfirmURL() = %q, want %q", got, want)
	}
}

func TestPasskeySetupClaimModeKeepsResultClaimableForCLI(t *testing.T) {
	r, db := setupAuthRouterWithPasskeys(t)
	tempExpiresAt := time.Now().Add(15 * time.Minute)
	user := storage.User{
		ID:                 "user_claim_mode",
		Email:              "claim-mode@example.com",
		Name:               "claim-mode@example.com",
		TempToken:          "temp-claim-token",
		TempTokenExpiresAt: &tempExpiresAt,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	w := testutil.DoRequest(r, "POST", "/auth/passkey-setup", map[string]string{
		"temp_token":  user.TempToken,
		"result_mode": "claim",
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("passkey setup = %d: %s", w.Code, w.Body.String())
	}
	var setupResp map[string]interface{}
	testutil.ParseJSON(t, w, &setupResp)
	challengeID, _ := setupResp["challenge_id"].(string)
	if challengeID == "" {
		t.Fatalf("missing challenge_id: %#v", setupResp)
	}
	publicKey, _ := setupResp["public_key"].(map[string]interface{})
	challenge, _ := publicKey["challenge"].(string)
	if challenge == "" {
		t.Fatalf("missing challenge in public_key: %#v", setupResp)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	credentialRawID := []byte("credential-claim-mode")
	credentialID := base64.RawURLEncoding.EncodeToString(credentialRawID)

	w = testutil.DoRequest(r, "POST", "/auth/passkey-verify", map[string]string{
		"challenge_id":       challengeID,
		"credential_id":      credentialID,
		"client_data_json":   testRegistrationClientData(t, challenge),
		"attestation_object": testRegistrationAttestationObject(t, key, credentialRawID, "localhost", 1),
		"device_name":        "test-device",
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("passkey verify = %d: %s", w.Code, w.Body.String())
	}
	var verifyResp map[string]interface{}
	testutil.ParseJSON(t, w, &verifyResp)
	if got := verifyResp["result_status"]; got != "claimable" {
		t.Fatalf("result_status = %v, want claimable", got)
	}

	w = testutil.DoRequest(r, "POST", "/auth/passkey-result", map[string]string{
		"temp_token":   user.TempToken,
		"challenge_id": challengeID,
	}, map[string]string{"Origin": "http://localhost"})
	if w.Code != http.StatusOK {
		t.Fatalf("passkey result = %d: %s", w.Code, w.Body.String())
	}
	var claimResp map[string]interface{}
	testutil.ParseJSON(t, w, &claimResp)
	if claimResp["access_token"] == "" || claimResp["refresh_token"] != nil || claimResp["session_id"] == "" || claimResp["credential_id"] != credentialID {
		t.Fatalf("unexpected claim response: %#v", claimResp)
	}
	assertRefreshCookie(t, w)
	assertAuthSessionScope(t, db, claimResp["session_id"].(string), authpkg.ClientBrowser, authpkg.ScopeBrowser)

	w = testutil.DoRequest(r, "POST", "/auth/passkey-result", map[string]string{
		"temp_token":   user.TempToken,
		"challenge_id": challengeID,
	}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("second claim = %d, want 401: %s", w.Code, w.Body.String())
	}
}

func TestPasskeyResultReturnsRefreshTokenForCLIClaim(t *testing.T) {
	r, db := setupAuthRouterWithPasskeys(t)
	user := storage.User{
		ID:                 "user_cli_claim_mode",
		Email:              "cli-claim@example.com",
		TempToken:          "temp-cli-claim",
		TempTokenExpiresAt: timePtr(time.Now().Add(15 * time.Minute)),
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	challengeID := "ch_cli_claim_mode"
	credentialID := "cred_cli_claim_mode"
	if err := db.Create(&storage.WebAuthnCredential{
		CredentialID: credentialID,
		UserID:       user.ID,
		PublicKey:    []byte("public-key"),
		CreatedAt:    time.Now(),
		LastUsedAt:   time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&storage.Challenge{
		ChallengeID: challengeID,
		UserID:      user.ID,
		Type:        "passkey_registration",
		Status:      "approved",
		PayloadJSON: "{}",
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}

	w := testutil.DoRequest(r, "POST", "/auth/passkey-result", map[string]string{
		"temp_token":   user.TempToken,
		"challenge_id": challengeID,
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("passkey result = %d: %s", w.Code, w.Body.String())
	}
	var claimResp map[string]interface{}
	testutil.ParseJSON(t, w, &claimResp)
	if claimResp["access_token"] == "" || claimResp["refresh_token"] == "" || claimResp["session_id"] == "" || claimResp["credential_id"] != credentialID {
		t.Fatalf("CLI claim response missing token pair: %#v", claimResp)
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "web3_refresh" {
			t.Fatalf("CLI claim should not set browser refresh cookie: %#v", cookie)
		}
	}
}

func TestPasskeyVerifyDoesNotReturnAPIKeyWithoutExplicitResultMode(t *testing.T) {
	r, db := setupAuthRouterWithPasskeys(t)
	tempExpiresAt := time.Now().Add(15 * time.Minute)
	user := storage.User{
		ID:                 "user_default_claim_mode",
		Email:              "default-claim@example.com",
		Name:               "default-claim@example.com",
		TempToken:          "temp-default-claim",
		TempTokenExpiresAt: &tempExpiresAt,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	w := testutil.DoRequest(r, "POST", "/auth/passkey-setup", map[string]string{
		"temp_token": user.TempToken,
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("passkey setup = %d: %s", w.Code, w.Body.String())
	}
	var setupResp map[string]interface{}
	testutil.ParseJSON(t, w, &setupResp)
	challengeID := setupResp["challenge_id"].(string)
	publicKey := setupResp["public_key"].(map[string]interface{})

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	credentialRawID := []byte("credential-default-claim")
	w = testutil.DoRequest(r, "POST", "/auth/passkey-verify", map[string]string{
		"challenge_id":       challengeID,
		"credential_id":      base64.RawURLEncoding.EncodeToString(credentialRawID),
		"client_data_json":   testRegistrationClientData(t, publicKey["challenge"].(string)),
		"attestation_object": testRegistrationAttestationObject(t, key, credentialRawID, "localhost", 1),
		"device_name":        "test-device",
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("passkey verify = %d: %s", w.Code, w.Body.String())
	}
	var verifyResp map[string]interface{}
	testutil.ParseJSON(t, w, &verifyResp)
	if verifyResp["result_status"] != "claimable" {
		t.Fatalf("result_status = %v, want claimable", verifyResp["result_status"])
	}
}

func TestPasskeyLoginOptionsAndVerifyReturnTokenPair(t *testing.T) {
	r, db := setupAuthRouterWithPasskeys(t)
	user := storage.User{
		ID:           "user_passkey_login",
		Email:        "login@example.com",
		Name:         "login@example.com",
		PasskeySetup: true,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	credentialID := base64.RawURLEncoding.EncodeToString([]byte("login-credential-id"))
	if err := db.Create(&storage.WebAuthnCredential{
		CredentialID: credentialID,
		UserID:       user.ID,
		PublicKey:    publicKeyDER,
		DeviceName:   "test-device",
		SignCount:    1,
		CreatedAt:    time.Now(),
		LastUsedAt:   time.Now(),
	}).Error; err != nil {
		t.Fatalf("create credential: %v", err)
	}

	w := testutil.DoRequest(r, "POST", "/auth/login-options", map[string]string{
		"email": "LOGIN@example.com",
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("login options = %d: %s", w.Code, w.Body.String())
	}
	var optionsResp map[string]interface{}
	testutil.ParseJSON(t, w, &optionsResp)
	challengeID, _ := optionsResp["challenge_id"].(string)
	if challengeID == "" {
		t.Fatalf("missing challenge_id: %#v", optionsResp)
	}
	publicKey, _ := optionsResp["public_key"].(map[string]interface{})
	challenge, _ := publicKey["challenge"].(string)
	if challenge == "" {
		t.Fatalf("missing public_key.challenge: %#v", optionsResp)
	}

	clientData := testAuthenticationClientData(t, challenge)
	authData := testAuthenticationAuthenticatorData(t, "localhost", 2)
	signature := testAuthenticationSignature(t, key, clientData, authData)

	w = testutil.DoRequest(r, "POST", "/auth/login-verify", map[string]string{
		"challenge_id":       challengeID,
		"credential_id":      credentialID,
		"client_data_json":   clientData,
		"authenticator_data": authData,
		"signature":          signature,
	}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("login verify = %d: %s", w.Code, w.Body.String())
	}
	var loginResp map[string]interface{}
	testutil.ParseJSON(t, w, &loginResp)
	if loginResp["access_token"] == "" || loginResp["refresh_token"] != nil || loginResp["session_id"] == "" {
		t.Fatalf("login response missing token pair: %#v", loginResp)
	}
	assertRefreshCookie(t, w)
	if loginResp["email"] != user.Email || loginResp["user_id"] != user.ID || loginResp["credential_id"] != credentialID {
		t.Fatalf("unexpected login response: %#v", loginResp)
	}
	assertAuthSessionScope(t, db, loginResp["session_id"].(string), authpkg.ClientBrowser, authpkg.ScopeBrowser)
}

func assertRefreshCookie(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name != "web3_refresh" {
			continue
		}
		if cookie.Value == "" || !cookie.HttpOnly || cookie.Path != "/auth" || cookie.SameSite != http.SameSiteLaxMode {
			t.Fatalf("unexpected refresh cookie: %#v", cookie)
		}
		return
	}
	t.Fatalf("missing web3_refresh cookie; cookies=%#v", w.Result().Cookies())
}

func assertAuthSessionScope(t *testing.T, db *gorm.DB, sessionID, wantClient, wantScope string) {
	t.Helper()
	var session storage.AuthSession
	if err := db.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		t.Fatalf("load auth session %s: %v", sessionID, err)
	}
	if session.ClientType != wantClient || session.Scope != wantScope {
		t.Fatalf("auth session client/scope = %s/%s, want %s/%s", session.ClientType, session.Scope, wantClient, wantScope)
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func TestPasskeyResultRejectsExpiredApprovedChallenge(t *testing.T) {
	r, db := setupAuthRouterWithPasskeys(t)
	tempExpiresAt := time.Now().Add(15 * time.Minute)
	user := storage.User{
		ID:                 "user_expired_claim",
		Email:              "expired-claim@example.com",
		Name:               "expired-claim@example.com",
		TempToken:          "temp-expired-claim",
		TempTokenExpiresAt: &tempExpiresAt,
		PasskeySetup:       true,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&storage.Challenge{
		ChallengeID: "ch_expired_claim",
		UserID:      user.ID,
		Type:        "passkey_registration",
		Status:      "approved",
		PayloadJSON: `{}`,
		CreatedAt:   time.Now().Add(-10 * time.Minute),
		ExpiresAt:   time.Now().Add(-5 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("create challenge: %v", err)
	}

	w := testutil.DoRequest(r, "POST", "/auth/passkey-result", map[string]string{
		"temp_token":   user.TempToken,
		"challenge_id": "ch_expired_claim",
	}, nil)
	if w.Code != http.StatusGone {
		t.Fatalf("expired passkey result = %d, want 410: %s", w.Code, w.Body.String())
	}
}

func testAuthenticationClientData(t *testing.T, challenge string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{
		"type":      "webauthn.get",
		"challenge": challenge,
		"origin":    "http://localhost",
	})
	if err != nil {
		t.Fatalf("marshal client data: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func testAuthenticationAuthenticatorData(t *testing.T, rpID string, signCount uint32) string {
	t.Helper()
	sum := sha256.Sum256([]byte(rpID))
	authData := append([]byte{}, sum[:]...)
	authData = append(authData, byte(0x01|0x04))
	count := make([]byte, 4)
	binary.BigEndian.PutUint32(count, signCount)
	authData = append(authData, count...)
	return base64.RawURLEncoding.EncodeToString(authData)
}

func testAuthenticationSignature(t *testing.T, key *ecdsa.PrivateKey, encodedClientDataJSON, encodedAuthenticatorData string) string {
	t.Helper()
	clientDataJSON, err := base64.RawURLEncoding.DecodeString(encodedClientDataJSON)
	if err != nil {
		t.Fatalf("decode client data: %v", err)
	}
	authData, err := base64.RawURLEncoding.DecodeString(encodedAuthenticatorData)
	if err != nil {
		t.Fatalf("decode authenticator data: %v", err)
	}
	clientHash := sha256.Sum256(clientDataJSON)
	signed := append(append([]byte{}, authData...), clientHash[:]...)
	signedHash := sha256.Sum256(signed)
	signature, err := ecdsa.SignASN1(rand.Reader, key, signedHash[:])
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(signature)
}

func testRegistrationClientData(t *testing.T, challenge string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{
		"type":      "webauthn.create",
		"challenge": challenge,
		"origin":    "http://localhost",
	})
	if err != nil {
		t.Fatalf("marshal client data: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func testRegistrationAttestationObject(t *testing.T, key *ecdsa.PrivateKey, credentialID []byte, rpID string, signCount uint32) string {
	t.Helper()
	authData := testRegistrationAuthenticatorDataRaw(rpID, signCount)
	authData[32] |= 0x40
	authData = append(authData, make([]byte, 16)...)
	credLen := make([]byte, 2)
	binary.BigEndian.PutUint16(credLen, uint16(len(credentialID)))
	authData = append(authData, credLen...)
	authData = append(authData, credentialID...)
	authData = append(authData, testRegistrationCOSEEC2Key(t, key)...)

	raw := testCBORMap([][]byte{
		testCBORText("fmt"), testCBORText("none"),
		testCBORText("authData"), testCBORBytes(authData),
		testCBORText("attStmt"), testCBORMap(nil),
	})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func testRegistrationAuthenticatorDataRaw(rpID string, signCount uint32) []byte {
	sum := sha256.Sum256([]byte(rpID))
	authData := append([]byte{}, sum[:]...)
	authData = append(authData, byte(0x01|0x04))
	count := make([]byte, 4)
	binary.BigEndian.PutUint32(count, signCount)
	return append(authData, count...)
}

func testRegistrationCOSEEC2Key(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	x := key.X.FillBytes(make([]byte, 32))
	y := key.Y.FillBytes(make([]byte, 32))
	return testCBORMap([][]byte{
		testCBORInt(1), testCBORInt(2),
		testCBORInt(3), testCBORInt(-7),
		testCBORInt(-1), testCBORInt(1),
		testCBORInt(-2), testCBORBytes(x),
		testCBORInt(-3), testCBORBytes(y),
	})
}

func testCBORMap(items [][]byte) []byte {
	out := testCBORLen(5, uint64(len(items)/2))
	for _, item := range items {
		out = append(out, item...)
	}
	return out
}

func testCBORText(s string) []byte {
	out := testCBORLen(3, uint64(len(s)))
	return append(out, []byte(s)...)
}

func testCBORBytes(b []byte) []byte {
	out := testCBORLen(2, uint64(len(b)))
	return append(out, b...)
}

func testCBORInt(v int64) []byte {
	if v >= 0 {
		return testCBORLen(0, uint64(v))
	}
	return testCBORLen(1, uint64(-1-v))
}

func testCBORLen(major byte, n uint64) []byte {
	prefix := major << 5
	switch {
	case n < 24:
		return []byte{prefix | byte(n)}
	case n <= 0xff:
		return []byte{prefix | 24, byte(n)}
	case n <= 0xffff:
		out := []byte{prefix | 25, 0, 0}
		binary.BigEndian.PutUint16(out[1:], uint16(n))
		return out
	default:
		out := []byte{prefix | 26, 0, 0, 0, 0}
		binary.BigEndian.PutUint32(out[1:], uint32(n))
		return out
	}
}
