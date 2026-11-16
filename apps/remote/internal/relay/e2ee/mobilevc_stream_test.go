package e2ee

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMobileVCStreamCodecEncryptsAndAuthenticatesJSON(t *testing.T) {
	clientCodec, agentCodec := testMobileVCCodecs(t)
	payload := map[string]any{"type": "user_message", "text": "secret command"}

	frame, err := clientCodec.EncodeJSON("msg_1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Encryption != Suite || frame.StreamID != MobileVCStreamID || frame.Counter != 0 {
		t.Fatalf("unexpected encrypted frame metadata: %#v", frame)
	}
	if strings.Contains(frame.Payload, "secret") {
		t.Fatal("ciphertext leaked plaintext")
	}

	var decoded map[string]any
	if err := agentCodec.DecodeJSON(frame, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["text"] != "secret command" {
		t.Fatalf("decoded payload mismatch: %#v", decoded)
	}
}

func TestMobileVCStreamCodecRejectsReplayAndMetadataTamper(t *testing.T) {
	clientCodec, agentCodec := testMobileVCCodecs(t)
	frame, err := clientCodec.Encode("msg_1", []byte(`{"type":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentCodec.Decode(frame); err != nil {
		t.Fatal(err)
	}
	if _, err := agentCodec.Decode(frame); err == nil || !strings.Contains(err.Error(), "replay") {
		t.Fatalf("expected replay rejection, got %v", err)
	}

	_, freshAgent := testMobileVCCodecs(t)
	tampered := frame
	tampered.StreamID = 2
	if _, err := freshAgent.Decode(tampered); err == nil {
		t.Fatal("accepted frame with tampered stream id")
	}

	tampered = frame
	tampered.Direction = DirectionAgentToClient
	if _, err := freshAgent.Decode(tampered); err == nil {
		t.Fatal("accepted frame with tampered direction")
	}
}

func TestMobileVCStreamCodecJSONShapeMatchesRelayForward(t *testing.T) {
	clientCodec, _ := testMobileVCCodecs(t)
	frame, err := clientCodec.Encode("msg_1", []byte(`{"type":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"type", "version", "sessionId", "clientId", "direction", "messageId",
		"contentType", "encryption", "payloadEncoding", "payload",
		"streamId", "counter", "handshakeId",
	} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("missing relay forward json field %q in %s", key, raw)
		}
	}
}

func testMobileVCCodecs(t *testing.T) (*MobileVCStreamCodec, *MobileVCStreamCodec) {
	t.Helper()
	keys := &TrafficKeys{
		ClientToAgent: mustDecodeHex(t, "fe783fd2cb680d136b04d39b0e7c605826e418afd88c3819838bb8e57738144d"),
		AgentToClient: mustDecodeHex(t, "51cc1a21ce5b5098780c55c7e0487e4e413347bf11114e942e682c890aac7209"),
	}
	clientCodec, err := NewClientMobileVCStreamCodec("rs_stream", "rc_stream", "hs_stream_01", keys)
	if err != nil {
		t.Fatal(err)
	}
	agentCodec, err := NewAgentMobileVCStreamCodec("rs_stream", "rc_stream", "hs_stream_01", keys)
	if err != nil {
		t.Fatal(err)
	}
	return clientCodec, agentCodec
}
