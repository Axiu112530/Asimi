"""Read-only checks for closed, isolated SQLite upgrade fixtures."""

import argparse
from collections import Counter
import json
from pathlib import Path
import re
import sqlite3


IMMUTABLE_FIXTURE_TABLES = (
    "app_settings", "ai_settings", "ai_profiles", "ai_models", "ai_credentials",
    "conversations", "messages", "ai_requests", "ai_usage", "workspaces",
    "agent_tasks", "agent_task_steps", "approval_records", "tool_executions",
    "file_intents", "product_actions", "product_action_journal",
    "product_action_intents", "computer_actions", "computer_approvals",
    "computer_action_events", "computer_task_evidence",
)


def open_fixture(path):
    path = Path(path).resolve(strict=True)
    wal = path.with_name(path.name + "-wal")
    if wal.exists() and wal.stat().st_size:
        raise ValueError("fixture must be checkpointed")
    return sqlite3.connect(path.as_uri() + "?mode=ro&immutable=1", uri=True)


def fixture_port(db):
    row = db.execute("SELECT json FROM ai_profiles WHERE id='fixture-chat' AND deleted=0").fetchone()
    endpoint = json.loads(row[0])["baseUrl"] if row else ""
    match = re.fullmatch(r"http://127\.0\.0\.1:([1-9][0-9]{0,4})/v1", endpoint)
    if not match or int(match[1]) > 65535:
        raise ValueError("expected an existing IPv4 loopback fixture endpoint")
    return int(match[1])


def rows(db, table):
    # Table names are fixed by IMMUTABLE_FIXTURE_TABLES.
    values = db.execute(f"SELECT * FROM {table} LIMIT 10001").fetchall()
    if len(values) > 10000:
        raise ValueError("unexpected fixture size")
    return Counter(values)


def compare(source, destination):
    if destination.execute("PRAGMA quick_check").fetchone() != ("ok",):
        raise ValueError("destination integrity check failed")
    if destination.execute("PRAGMA foreign_key_check").fetchone() is not None:
        raise ValueError("destination foreign key check failed")
    preserved = {}
    for table in IMMUTABLE_FIXTURE_TABLES:
        before = rows(source, table)
        after = rows(destination, table)
        if before - after:
            raise ValueError(f"fixture rows changed in {table}")
        preserved[table] = sum(before.values())
    return {
        "passed": True,
        "schemaBefore": source.execute("PRAGMA user_version").fetchone()[0],
        "schemaAfter": destination.execute("PRAGMA user_version").fetchone()[0],
        "preservedRows": preserved,
        "integrity": "ok",
        "foreignKeyViolations": 0,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source")
    parser.add_argument("--compare")
    args = parser.parse_args()
    with open_fixture(args.source) as source:
        if args.compare:
            with open_fixture(args.compare) as destination:
                print(json.dumps(compare(source, destination)))
        else:
            print(fixture_port(source))


if __name__ == "__main__":
    main()
