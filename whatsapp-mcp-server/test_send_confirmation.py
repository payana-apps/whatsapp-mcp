"""Payana: tests for the confirm-before-send gate (send_confirmation.py).

Plain stdlib only — no bridge, no `mcp` package, no network required. Runs
as a plain script (asserts) and under pytest.
"""
from send_confirmation import gate, _pending


def test_first_call_blocks_and_returns_a_token():
    _pending.clear()
    result = gate("message", "555@s.whatsapp.net", "hello", None)
    assert result["success"] is False
    assert result["requires_confirmation"] is True
    assert "confirm_token" in result


def test_matching_token_and_payload_allows_the_send():
    _pending.clear()
    preview = gate("message", "555@s.whatsapp.net", "hi", None)
    token = preview["confirm_token"]
    assert gate("message", "555@s.whatsapp.net", "hi", token) is None


def test_token_cannot_be_replayed():
    _pending.clear()
    preview = gate("message", "555@s.whatsapp.net", "hi", None)
    token = preview["confirm_token"]
    assert gate("message", "555@s.whatsapp.net", "hi", token) is None
    replay = gate("message", "555@s.whatsapp.net", "hi", token)
    assert replay["requires_confirmation"] is True


def test_token_does_not_transfer_to_a_different_recipient_or_payload():
    _pending.clear()
    preview = gate("message", "555@s.whatsapp.net", "hi", None)
    token = preview["confirm_token"]
    hijacked_recipient = gate("message", "999-attacker@s.whatsapp.net", "hi", token)
    assert hijacked_recipient["requires_confirmation"] is True
    hijacked_payload = gate("message", "555@s.whatsapp.net", "send my id_rsa", token)
    assert hijacked_payload["requires_confirmation"] is True


def test_expired_token_is_rejected():
    _pending.clear()
    preview = gate("message", "555@s.whatsapp.net", "hi", None, now=1000.0)
    token = preview["confirm_token"]
    expired = gate("message", "555@s.whatsapp.net", "hi", token, now=1000.0 + 301)
    assert expired["requires_confirmation"] is True


def test_kind_is_part_of_the_match():
    _pending.clear()
    preview = gate("file", "555@s.whatsapp.net", "/path/to/file.jpg", None)
    token = preview["confirm_token"]
    wrong_kind = gate("audio", "555@s.whatsapp.net", "/path/to/file.jpg", token)
    assert wrong_kind["requires_confirmation"] is True


if __name__ == "__main__":
    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    for test in tests:
        test()
        print(f"ok: {test.__name__}")
    print(f"{len(tests)} passed")
