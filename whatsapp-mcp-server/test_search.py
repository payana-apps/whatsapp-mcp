"""Payana: tests for the reimplemented fuzzy contact search and the
not-linked guard. Runs as a plain script (asserts) and under pytest.

Uses only the stdlib plus `requests` (imported by whatsapp at module load);
no linked WhatsApp account or running bridge is required.
"""
import os
import sqlite3
import tempfile


def _seed_store():
    """Create a fresh temp store and point the (already-imported) whatsapp module
    at it, so tests are order-independent regardless of any global mutation."""
    store = tempfile.mkdtemp()
    os.environ["WHATSAPP_STORE_DIR"] = store
    db = os.path.join(store, "messages.db")
    conn = sqlite3.connect(db)
    conn.execute("CREATE TABLE chats (jid TEXT PRIMARY KEY, name TEXT, last_message_time TIMESTAMP)")
    conn.executemany(
        "INSERT INTO chats (jid, name) VALUES (?, ?)",
        [
            ("5214625091298@s.whatsapp.net", "FERNANDO RANGEL"),
            ("573001112233@s.whatsapp.net", "María José Gómez"),
            ("120363406084831705@g.us", "Grupo Ventas"),
        ],
    )
    conn.commit()
    conn.close()
    # Rebind the module globals: they were computed from the env at import time,
    # so re-setting the env alone would not move them off the first test's store.
    import whatsapp
    whatsapp.MESSAGES_DB_PATH = db
    whatsapp.WHATSAPP_DB_PATH = os.path.join(store, "whatsapp.db")
    return store


def test_fuzzy_typo():
    _seed_store()
    import whatsapp
    r = whatsapp.search_contacts("fernando rangl")
    assert r and "FERNANDO" in (r[0].name or "")


def test_accent_insensitive():
    _seed_store()
    import whatsapp
    r = whatsapp.search_contacts("maria jose")
    assert any("María José" in (c.name or "") for c in r)


def test_phone_digits():
    _seed_store()
    import whatsapp
    r = whatsapp.search_contacts("4625091298")
    assert any(c.jid.startswith("5214625091298") for c in r)


def test_threshold_rejects_garbage():
    _seed_store()
    import whatsapp
    assert whatsapp.smart_search_contacts("zzzqwx", similarity_threshold=0.6) == []


def test_groups_excluded_by_default():
    _seed_store()
    import whatsapp
    assert not any(c.jid.endswith("@g.us") for c in whatsapp.search_contacts("grupo"))
    assert any(c.jid.endswith("@g.us") for c in whatsapp.search_contacts("grupo", include_groups=True))


def test_not_linked_raises():
    store = _seed_store()
    import whatsapp
    whatsapp.MESSAGES_DB_PATH = os.path.join(store, "does-not-exist.db")
    try:
        whatsapp.list_messages(query="hi")
    except whatsapp.BridgeNotLinkedError:
        return
    raise AssertionError("expected BridgeNotLinkedError, not an empty result")


if __name__ == "__main__":
    for name, fn in sorted(globals().items()):
        if name.startswith("test_") and callable(fn):
            fn()
            print(f"ok  {name}")
    print("ALL FUZZY + SAFETY TESTS PASSED")
