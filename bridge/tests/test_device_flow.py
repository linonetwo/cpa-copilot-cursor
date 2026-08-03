from __future__ import annotations

import unittest

from device_flow import authorization_url, login_metadata


class DeviceFlowTest(unittest.TestCase):
    def test_copilot_code_is_transportable_through_legacy_cpa(self) -> None:
        self.assertEqual(
            authorization_url(
                "copilot",
                "https://github.com/login/device",
                "ABCD-EFGH",
            ),
            "https://github.com/login/device?user_code=ABCD-EFGH",
        )

    def test_existing_query_is_preserved_without_duplicate_code(self) -> None:
        self.assertEqual(
            authorization_url(
                "copilot",
                "https://github.com/login/device?prompt=select_account&user_code=OLD-CODE",
                "NEW-CODE",
            ),
            "https://github.com/login/device?prompt=select_account&user_code=NEW-CODE",
        )

    def test_metadata_keeps_standard_device_flow_fields(self) -> None:
        metadata = login_metadata(
            "https://github.com/login/device",
            "ABCD-EFGH",
            "Enter the code in GitHub.",
        )
        self.assertEqual(metadata["verification_uri"], "https://github.com/login/device")
        self.assertEqual(metadata["user_code"], "ABCD-EFGH")


if __name__ == "__main__":
    unittest.main()
