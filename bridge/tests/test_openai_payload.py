from __future__ import annotations

import unittest

from openai_payload import completion_payload, prompt_from_payload, stream_chunks


class OpenAIPayloadTest(unittest.TestCase):
    def test_prompt_preserves_roles_and_multimodal_text(self) -> None:
        prompt = prompt_from_payload(
            {
                "messages": [
                    {"role": "system", "content": "Be concise."},
                    {
                        "role": "user",
                        "content": [
                            {"type": "text", "text": "Hello"},
                            {"type": "text", "text": "World"},
                        ],
                    },
                ]
            }
        )
        self.assertIn("SYSTEM:\nBe concise.", prompt)
        self.assertIn("USER:\nHello\nWorld", prompt)
        self.assertTrue(prompt.endswith("ASSISTANT:"))

    def test_completion_and_stream_are_openai_shaped(self) -> None:
        completion = completion_payload("model", "answer")
        self.assertEqual(completion["choices"][0]["message"]["content"], "answer")
        chunks = stream_chunks("model", "answer")
        self.assertIn('"content": "answer"', chunks[1])
        self.assertEqual(chunks[-1], "data: [DONE]\n\n")


if __name__ == "__main__":
    unittest.main()
