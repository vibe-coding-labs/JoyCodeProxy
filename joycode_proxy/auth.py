import json
import os
import platform
import sqlite3
from dataclasses import dataclass


@dataclass
class Credentials:
    pt_key: str
    user_id: str


def load_from_system() -> Credentials:
    if platform.system() != "Darwin":
        raise RuntimeError(
            "auto credential extraction only supported on macOS; "
            "on other systems, please provide --ptkey and --userid flags"
        )
    home = os.path.expanduser("~")
    db_path = os.path.join(
        home,
        "Library", "Application Support",
        "JoyCode", "User", "globalStorage", "state.vscdb",
    )
    if not os.path.exists(db_path):
        raise FileNotFoundError(
            f"JoyCode state database not found at {db_path}\n"
            "  Please install and log in to JoyCode IDE first"
        )
    conn = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
    try:
        cursor = conn.execute(
            "SELECT value FROM ItemTable WHERE key='JoyCoder.IDE'"
        )
        row = cursor.fetchone()
        if not row:
            raise ValueError(
                "login info not found in database\n"
                "  Please log in to JoyCode IDE first"
            )
        data = json.loads(row[0])
        user = data.get("joyCoderUser", {})
        pt_key = user.get("ptKey", "")
        user_id = user.get("userId", "")
        if not pt_key:
            raise ValueError(
                "ptKey is empty in stored credentials\n"
                "  Please re-login to JoyCode IDE"
            )
        if not user_id:
            raise ValueError(
                "userId is empty in stored credentials\n"
                "  Please re-login to JoyCode IDE"
            )
        return Credentials(pt_key=pt_key, user_id=user_id)
    finally:
        conn.close()
