package e2ee

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

const (
	vectorClientPrivate = "201f1e1d1c1b1a191817161514131211100f0e0d0c0b0a090807060504030201"
	vectorAgentPrivate  = "2f2e2d2c2b2a292827262524232221201f1e1d1c1b1a19181716151413121110"
	vectorPayload       = "mobilevc relay e2ee vector payload"
)

var vectorContext = FrameContext{
	SessionID:   "rs_test_vector",
	ClientID:    "rc_test_client",
	HandshakeID: "hs_test_vector_01",
	Direction:   DirectionClientToAgent,
	StreamID:    7,
	Counter:     42,
}

func TestCrossLanguageVector(t *testing.T) {
	clientPrivate := mustDecodeHex(t, vectorClientPrivate)
	agentPrivate := mustDecodeHex(t, vectorAgentPrivate)

	clientPublic, err := PublicKeyFromPrivate(clientPrivate)
	if err != nil {
		t.Fatal(err)
	}
	agentPublic, err := PublicKeyFromPrivate(agentPrivate)
	if err != nil {
		t.Fatal(err)
	}

	assertHex(t, "client public", clientPublic, "0421e184d5162d8a4d59f7d99fa819f84f0b6b162339ec1859c78f77362e37c28ff9289adbfe3f2a462e1043cd661a56bc7ded65a454b1c9e3f88bc47e2d1e8bf1")
	assertHex(t, "agent public", agentPublic, "04b0c9b23dbe2da93634265119a5f60ff0e0ff38695b6214b4bc934a4fe8a43124bbdfe38eb01ecd82ffa5b6dc3d139f4c5f2bc579e8cb8ff24a317bf5f5fca859")

	clientShared, err := SharedSecret(clientPrivate, agentPublic)
	if err != nil {
		t.Fatal(err)
	}
	agentShared, err := SharedSecret(agentPrivate, clientPublic)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(clientShared, agentShared) {
		t.Fatal("client and agent shared secrets differ")
	}
	assertHex(t, "shared secret", clientShared, "3731680e7b78859914321fa055572ebbfe67a819cbf50ae4e412258d20667d25")

	salt := TrafficSalt(vectorContext)
	assertHex(t, "traffic salt", salt, "b9b61603d86e47979f5f5d27b7bed551e29c1979a485d55236c299285f853f33")

	key, err := DeriveTrafficKey(clientShared, vectorContext)
	if err != nil {
		t.Fatal(err)
	}
	assertHex(t, "traffic key", key, "fe783fd2cb680d136b04d39b0e7c605826e418afd88c3819838bb8e57738144d")

	nonce, err := Nonce(vectorContext)
	if err != nil {
		t.Fatal(err)
	}
	assertHex(t, "nonce", nonce, "c7aa9ffa41338991891dada0")

	aad, err := AAD(vectorContext)
	if err != nil {
		t.Fatal(err)
	}
	assertHex(t, "aad", aad, "4d5643450001002c703235362d65636473612b703235362d656364682b686b64662d7368613235362b6165732d3235362d67636d000e72735f746573745f766563746f72000e72635f746573745f636c69656e74001168735f746573745f766563746f725f3031000f636c69656e745f746f5f6167656e740000000000000007000000000000002a")

	sealed, err := Encrypt(key, []byte(vectorPayload), vectorContext)
	if err != nil {
		t.Fatal(err)
	}
	assertHex(t, "sealed payload", sealed, "7631c834d3631978cd53028ba4ab870585bcc6301b31c79b49e03d062343b411b3f907b104581d2abb56b5c5ce44b2c344aa")

	plaintext, err := Decrypt(key, sealed, vectorContext)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != vectorPayload {
		t.Fatalf("plaintext mismatch: %q", plaintext)
	}

	fingerprint := Fingerprint(agentPublic)
	assertHex(t, "agent fingerprint", fingerprint, "4e7d5371dd01c77385af5e2cad89a989671481cf162b803dbcdf5a7585b321a0")
	if got := ShortFingerprint(fingerprint); got != "JZ6V-G4O5-AHDX-HBNP-LYWK" {
		t.Fatalf("short fingerprint mismatch: %s", got)
	}
}

func TestDecryptRejectsTampering(t *testing.T) {
	key := mustDecodeHex(t, "fe783fd2cb680d136b04d39b0e7c605826e418afd88c3819838bb8e57738144d")
	sealed := mustDecodeHex(t, "7631c834d3631978cd53028ba4ab870585bcc6301b31c79b49e03d062343b411b3f907b104581d2abb56b5c5ce44b2c344aa")
	sealed[0] ^= 0x01

	if _, err := Decrypt(key, sealed, vectorContext); err == nil {
		t.Fatal("decrypt accepted tampered ciphertext")
	}
}

func TestContextValidation(t *testing.T) {
	invalid := vectorContext
	invalid.Direction = "client-to-agent"

	if _, err := Nonce(invalid); !errors.Is(err, ErrInvalidDirection) {
		t.Fatalf("expected invalid direction error, got %v", err)
	}

	if _, err := Encrypt([]byte("short"), nil, vectorContext); !errors.Is(err, ErrInvalidLength) {
		t.Fatalf("expected invalid key length error, got %v", err)
	}
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func assertHex(t *testing.T, name string, got []byte, want string) {
	t.Helper()
	if hex.EncodeToString(got) != want {
		t.Fatalf("%s mismatch:\n got %s\nwant %s", name, hex.EncodeToString(got), want)
	}
}
