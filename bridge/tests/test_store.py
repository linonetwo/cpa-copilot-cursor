from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from store import CredentialStore


class CredentialStoreTest(unittest.TestCase):
    def test_record_round_trip_uses_opaque_handle(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            store = CredentialStore(Path(directory))
            handle = store.new_handle()
            record = store.save_record("copilot", handle, label="Primary")
            loaded = store.load_record("copilot", handle)
            self.assertEqual(loaded, record)
            self.assertNotIn("token", (Path(directory) / "copilot" / handle / "auth.json").read_text())

    def test_rejects_path_traversal(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            store = CredentialStore(Path(directory))
            with self.assertRaises(ValueError):
                store.account_dir("cursor", "../escape")


if __name__ == "__main__":
    unittest.main()
