from __future__ import annotations

import io
import math
import os
import traceback
import wave
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import numpy as np
from flask import Flask, Response, jsonify, request

app = Flask(__name__)

DEFAULT_HOST = os.getenv("SIDECAR_HOST", "127.0.0.1")
DEFAULT_PORT = int(os.getenv("SIDECAR_PORT", "9966"))
DEFAULT_MODE = os.getenv("SIDECAR_MODE", "auto").strip().lower() or "auto"
DEFAULT_SAMPLE_RATE = int(os.getenv("CHATTTS_SAMPLE_RATE", "24000"))
DEFAULT_MODEL_DIR = os.getenv("CHATTTS_MODEL_DIR", str(Path.home() / ".cache" / "mobilevc" / "chattts")).strip()
SUPPORTED_MODES = {"auto", "mock", "chattts"}
SUPPORTED_FORMATS = {"wav"}


@dataclass
class ProviderStatus:
    backend: str
    ready: bool
    detail: str = ""


class ChatTTSBackend:
    def __init__(self) -> None:
        self._attempted = False
        self._chattts = None
        self._pipeline = None
        self._error = ""

    def ensure_ready(self) -> bool:
        if self._pipeline is not None:
            return True
        if self._attempted:
            return False

        self._attempted = True
        try:
            import ChatTTS  # type: ignore

            self._chattts = ChatTTS
            chat = ChatTTS.Chat()
            model_dir = current_model_dir()
            if model_dir:
                model_dir.mkdir(parents=True, exist_ok=True)
            if hasattr(chat, "load"):
                if model_dir:
                    chat.load(source="custom", custom_path=str(model_dir), compile=False)
                else:
                    chat.load(compile=False)
            elif hasattr(chat, "load_models"):
                chat.load_models(compile=False)
            else:
                raise RuntimeError("unsupported ChatTTS loader")
            if hasattr(chat, "has_loaded") and not chat.has_loaded(use_decoder=True):
                raise RuntimeError("ChatTTS models are not fully loaded")
            self._pipeline = chat
            self._error = ""
            return True
        except Exception as exc:  # pragma: no cover - depends on local env
            self._pipeline = None
            self._error = f"ChatTTS init failed: {exc}"
            return False

    @property
    def error(self) -> str:
        return self._error

    def synthesize(self, text: str, sample_rate: int) -> bytes:
        if not self.ensure_ready():
            raise RuntimeError(self._error or "ChatTTS is unavailable")
        wav = self._run_infer(text)
        audio = extract_audio_array(wav)
        return encode_wav(audio, sample_rate)

    def _run_infer(self, text: str) -> Any:
        assert self._pipeline is not None
        infer = getattr(self._pipeline, "infer", None)
        if infer is None:
            raise RuntimeError("ChatTTS infer method not found")

        result = infer(text)
        if isinstance(result, tuple):
            result = result[0]
        return result


chattts_backend = ChatTTSBackend()


def current_mode() -> str:
    mode = os.getenv("SIDECAR_MODE", DEFAULT_MODE).strip().lower() or "auto"
    if mode not in SUPPORTED_MODES:
        return "auto"
    return mode


def current_sample_rate() -> int:
    value = os.getenv("CHATTTS_SAMPLE_RATE", str(DEFAULT_SAMPLE_RATE)).strip()
    try:
        parsed = int(value)
    except ValueError:
        return DEFAULT_SAMPLE_RATE
    return parsed if parsed > 0 else DEFAULT_SAMPLE_RATE


def current_model_dir() -> Path | None:
    raw = os.getenv("CHATTTS_MODEL_DIR", DEFAULT_MODEL_DIR).strip()
    if not raw:
        return None
    return Path(raw).expanduser()


def health_status() -> ProviderStatus:
    mode = current_mode()
    if mode == "mock":
        return ProviderStatus(backend="mock", ready=True)
    if chattts_backend.ensure_ready():
        return ProviderStatus(backend="chattts", ready=True)
    if mode == "auto":
        return ProviderStatus(backend="mock", ready=True, detail=chattts_backend.error)
    return ProviderStatus(backend="chattts", ready=False, detail=chattts_backend.error or "ChatTTS unavailable")


def synthesize_audio(text: str, sample_rate: int) -> tuple[bytes, str]:
    mode = current_mode()
    if mode == "mock":
        return generate_mock_wav(text, sample_rate), "mock"
    if mode == "chattts":
        return chattts_backend.synthesize(text, sample_rate), "chattts"
    if chattts_backend.ensure_ready():
        return chattts_backend.synthesize(text, sample_rate), "chattts"
    return generate_mock_wav(text, sample_rate), "mock"


def generate_mock_wav(text: str, sample_rate: int) -> bytes:
    length = max(len(text.strip()), 1)
    duration_seconds = min(2.4, 0.45 + length * 0.035)
    total_samples = max(int(sample_rate * duration_seconds), sample_rate // 5)
    t = np.linspace(0.0, duration_seconds, total_samples, endpoint=False)

    base_freq = 440.0 + (length % 8) * 35.0
    tone = 0.32 * np.sin(2.0 * math.pi * base_freq * t)
    overtone = 0.12 * np.sin(2.0 * math.pi * base_freq * 2.0 * t)
    envelope = np.minimum(1.0, t / 0.03) * np.minimum(1.0, np.maximum(0.0, (duration_seconds - t) / 0.08))
    audio = (tone + overtone) * envelope
    return encode_wav(audio, sample_rate)


def extract_audio_array(value: Any) -> np.ndarray:
    if hasattr(value, "detach"):
        value = value.detach()
    if hasattr(value, "cpu"):
        value = value.cpu()
    if hasattr(value, "numpy"):
        value = value.numpy()

    if isinstance(value, list):
        if not value:
            raise ValueError("ChatTTS returned empty audio list")
        value = value[0]
        if hasattr(value, "detach"):
            value = value.detach()
        if hasattr(value, "cpu"):
            value = value.cpu()
        if hasattr(value, "numpy"):
            value = value.numpy()

    array = np.asarray(value, dtype=np.float32)
    if array.size == 0:
        raise ValueError("ChatTTS returned empty audio")
    array = np.squeeze(array)
    if array.ndim == 0:
        raise ValueError("ChatTTS returned invalid audio shape")
    if array.ndim > 1:
        array = array.reshape(-1)
    return np.clip(array, -1.0, 1.0)


def encode_wav(audio: np.ndarray, sample_rate: int) -> bytes:
    clipped = np.clip(np.asarray(audio, dtype=np.float32), -1.0, 1.0)
    pcm = (clipped * 32767.0).astype(np.int16)
    buffer = io.BytesIO()
    with wave.open(buffer, "wb") as wav_file:
        wav_file.setnchannels(1)
        wav_file.setsampwidth(2)
        wav_file.setframerate(sample_rate)
        wav_file.writeframes(pcm.tobytes())
    return buffer.getvalue()


def error_response(message: str, status_code: int) -> Response:
    return jsonify({"error": message}), status_code


@app.get("/healthz")
def healthz() -> Response:
    status = health_status()
    body = {
        "status": "ok" if status.ready else "error",
        "mode": current_mode(),
        "backend": status.backend,
        "sampleRate": current_sample_rate(),
    }
    model_dir = current_model_dir()
    if model_dir:
        body["modelDir"] = str(model_dir)
    if status.detail:
        body["detail"] = status.detail
    return jsonify(body), 200 if status.ready else 503


@app.post("/synthesize")
def synthesize() -> Response:
    payload = request.get_json(silent=True)
    if not isinstance(payload, dict):
        return error_response("invalid json body", 400)

    text = str(payload.get("text", "")).strip()
    if not text:
        return error_response("text is required", 400)

    fmt = str(payload.get("format", "wav")).strip().lower() or "wav"
    if fmt not in SUPPORTED_FORMATS:
        return error_response("format must be wav", 400)

    try:
        audio_bytes, backend = synthesize_audio(text, current_sample_rate())
        response = Response(audio_bytes, mimetype="audio/wav")
        response.headers["X-TTS-Backend"] = backend
        response.headers["Content-Length"] = str(len(audio_bytes))
        return response
    except RuntimeError as exc:
        if current_mode() == "chattts":
            return error_response(str(exc), 503)
        app.logger.error("runtime synthesize error: %s", exc)
        return error_response("synthesize failed", 500)
    except Exception as exc:  # pragma: no cover - defensive fallback
        app.logger.error("unexpected synthesize error: %s\n%s", exc, traceback.format_exc())
        return error_response("internal server error", 500)


if __name__ == "__main__":
    app.run(host=DEFAULT_HOST, port=DEFAULT_PORT)
