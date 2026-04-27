import logging
from pathlib import Path
from typing import Optional

from cryptography.fernet import Fernet

log = logging.getLogger("joycode-proxy.crypto")

DATA_DIR = Path.home() / ".joycode-proxy"
KEY_FILE = DATA_DIR / ".enc_key"

_PREFIX = "enc:"


def _load_or_create_key() -> bytes:
    DATA_DIR.mkdir(parents=True, exist_ok=True)
    if KEY_FILE.exists():
        key = KEY_FILE.read_bytes().strip()
        if key:
            return key
    key = Fernet.generate_key()
    KEY_FILE.write_bytes(key)
    KEY_FILE.chmod(0o600)
    log.info("Generated new encryption key at %s", KEY_FILE)
    return key


_fernet: Optional[Fernet] = None


def _get_fernet() -> Fernet:
    global _fernet
    if _fernet is None:
        _fernet = Fernet(_load_or_create_key())
    return _fernet


def encrypt(plaintext: str) -> str:
    """Encrypt a plaintext string. Returns prefixed ciphertext."""
    if not plaintext:
        return plaintext
    f = _get_fernet()
    encrypted = f.encrypt(plaintext.encode("utf-8"))
    return _PREFIX + encrypted.decode("ascii")


def decrypt(ciphertext: str) -> str:
    """Decrypt a prefixed ciphertext string. Returns plaintext."""
    if not ciphertext or not ciphertext.startswith(_PREFIX):
        return ciphertext
    f = _get_fernet()
    decrypted = f.decrypt(ciphertext[len(_PREFIX):].encode("ascii"))
    return decrypted.decode("utf-8")


def is_encrypted(value: str) -> bool:
    """Check if a value is encrypted (has the enc: prefix)."""
    return bool(value) and value.startswith(_PREFIX)
