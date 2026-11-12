"""Read-only, bounded release audit with redacted output."""

import argparse
from collections import Counter
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import subprocess
import sys
import zipfile


ROOT = Path(__file__).resolve().parents[2]
MAX_COMMITS = 256
MAX_FILES = 5000
MAX_FILE_BYTES = 64 * 1024 * 1024
MAX_TOTAL_BYTES = 256 * 1024 * 1024
MAX_REPORTED_FINDINGS = 500
MAX_GIT_METADATA = 4 * 1024 * 1024

PROVIDER_TOKENS = {
    "provider-sk-token": rb"(?<![\w-])sk-(?:(?:proj|svcacct|admin|ant-api\d+|or-v1)-)?[A-Za-z0-9_-]{20,}(?![\w-])",
    "google-api-token": rb"(?<!\w)AIza[A-Za-z0-9_-]{35}(?![\w-])",
    "github-token": rb"(?<!\w)(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{40,})(?!\w)",
    "aws-access-key-id": rb"(?<!\w)(?:AKIA|ASIA)[A-Z0-9]{16}(?!\w)",
    "slack-token": rb"(?<![\w-])xox[baprs]-[A-Za-z0-9-]{18,}(?![\w-])",
}
PROVIDER_TOKENS = {name: re.compile(pattern) for name, pattern in PROVIDER_TOKENS.items()}
ASSIGNMENT = re.compile(
    rb"(?ix)\b(?:[a-z][a-z0-9_]*_)?(?:api[_-]?key|access[_-]?token|secret[_-]?key|"
    rb"(?:database|db|postgres|mysql)[_-]?(?:password|passwd)|password|passwd|client[_-]?secret)"
    rb"[\x22\x27]?[ \t]*[:=][ \t]*(?:(?:[\x22\x27])([A-Za-z0-9._+/=!?$%#@-]{8,})(?:[\x22\x27])|"
    rb"([A-Za-z0-9._+/=!?$%#@-]{12,})(?=[ \t\r\n;,]|$))"
)
DATABASE_URI = re.compile(rb"(?i)\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|mssql)://[^\s:'\x22]+:([^\s@'\x22]+)@")
BEARER = re.compile(rb"(?i)\bBearer[ \t]+([A-Za-z0-9._+/=-]{16,})(?![A-Za-z0-9._+/=-])")
PEM = re.compile(
    rb"-----BEGIN ([A-Z0-9 ]*(?:PRIVATE KEY|CERTIFICATE))-----[\r\n]+"
    rb"[A-Za-z0-9+/=\r\n]{32,}-----END \1-----"
)
PERSONAL_PATH = re.compile(rb"(?i)(?:[a-z]:[/\\]+Users[/\\]+|/(?:Users|home)/)([A-Za-z0-9_.@-]+)")
WINDOWS_WIDE = re.compile(rb"(?:[\x20-\x7e]\x00){10,}")
SENSITIVE_FILENAME = re.compile(
    r"(?i)(?:^\.env(?:\..*)?$|^(?:id_rsa|id_ed25519|credentials\.json)$|"
    r"\.(?:sqlite3?|db|pfx|p12|pem|key|crt|cer)(?:-wal|-shm|-journal)?$)"
)
ZIP_EXCLUSIONS = re.compile(
    r"(?i)(?:^|/)(?:\.git|node_modules|profile|profiles|cache|__pycache__)(?:/|$)|\.(?:log|jsonl|py|go|ts|vue)$"
)
ENV_KEYS = (
    "DEEPSEEK_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY",
    "GOOGLE_API_KEY", "GOOGLE_GENERATIVE_AI_API_KEY", "AZURE_OPENAI_API_KEY",
    "OPENROUTER_API_KEY", "MISTRAL_API_KEY", "GROQ_API_KEY", "COHERE_API_KEY",
    "XAI_API_KEY", "DATABASE_PASSWORD", "DB_PASSWORD", "POSTGRES_PASSWORD",
    "MYSQL_PASSWORD", "AWS_SECRET_ACCESS_KEY", "GITHUB_TOKEN",
)


def git(*args):
    command = ["git", "-C", str(ROOT), *args]
    with subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL) as process:
        output = process.stdout.read(MAX_GIT_METADATA + 1)
        if len(output) > MAX_GIT_METADATA:
            process.kill()
            raise ValueError("Git metadata scan limit exceeded")
        if process.wait():
            raise subprocess.CalledProcessError(process.returncode, command)
        return output


def synthetic(value):
    lower = value.lower()
    markers = (b"fixture", b"placeholder", b"dummy", b"example", b"your-key", b"replace-me",
               b"changeme", b"mock-", b"test-key", b"fake-")
    return len(set(lower)) < 5 or any(marker in lower for marker in markers)


def sensitive_file(name):
    last = PurePosixPath(name.replace("\\", "/")).name
    return SENSITIVE_FILENAME.search(last) and last.lower() not in {".env.example", ".env.template", ".env.sample"}


def reparse_point(path):
    info = path.lstat()
    return stat.S_ISLNK(info.st_mode) or bool(getattr(info, "st_file_attributes", 0) & 0x400)


def unsafe_worktree_path(relative):
    if relative.anchor or ".." in relative.parts:
        return True
    path = ROOT
    for component in relative.parts:
        path /= component
        if reparse_point(path):
            return True
    return False


def read_exact(stream, size):
    content = stream.read(size)
    if len(content) != size:
        raise OSError("truncated Git object stream")
    return content


class Audit:
    def __init__(self):
        self.findings = []
        self.rules = Counter()
        self.levels = Counter()
        self.totals = {}
        self.known_secrets = [
            value.encode() for name in ENV_KEYS if len(value := os.environ.get(name, "")) >= 8
        ]
        absolute = str(ROOT)
        self.checkout_paths = {
            spelling.encode().lower() for spelling in (absolute, absolute.replace("\\", "/"), absolute.replace("\\", "\\\\"))
        }
        self.home_name = os.environ.get("USERNAME", "").encode().lower()

    def record(self, scope, name, rule, level, line=None, object_id=None):
        self.rules[(scope, rule)] += 1
        self.levels[(scope, level)] += 1
        if len(self.findings) < MAX_REPORTED_FINDINGS:
            raw_name = name.encode("utf-8", errors="replace")
            redacted = PERSONAL_PATH.sub(b"[personal-path]", raw_name)
            for spelling in self.checkout_paths:
                redacted = re.sub(re.escape(spelling), b"[checkout-path]", redacted, flags=re.IGNORECASE)
            for pattern in PROVIDER_TOKENS.values():
                redacted = pattern.sub(b"[token]", redacted)
            for secret in self.known_secrets:
                redacted = redacted.replace(secret, b"[secret]")
            finding = {"scope": scope, "file": redacted.decode("utf-8", errors="replace"), "rule": rule, "level": level}
            if line is not None:
                finding["line"] = line
            if object_id is not None:
                finding["gitObject"] = object_id[:12]
            self.findings.append(finding)

    def reserve(self, scope, name, size):
        total = self.totals.setdefault(scope, {"filesScanned": 0, "bytesScanned": 0})
        if (size > MAX_FILE_BYTES or total["bytesScanned"] + size > MAX_TOTAL_BYTES
                or total["filesScanned"] >= MAX_FILES):
            self.record(scope, name, "scan-bound-exceeded", "blocked")
            return False
        total["filesScanned"] += 1
        total["bytesScanned"] += size
        return True

    def inspect(self, scope, name, content, object_id=None):
        if content.startswith(b"SQLite format 3\x00"):
            self.record(scope, name, "sqlite-database", "blocked", object_id=object_id)
        for secret in self.known_secrets:
            offset = content.find(secret)
            if offset >= 0 or secret.decode().encode("utf-16le") in content:
                self.record(scope, name, "active-environment-credential", "blocked", object_id=object_id)

        hits = {}

        def hit(rule, level, offset):
            hits.setdefault((rule, level), offset)

        for rule, pattern in PROVIDER_TOKENS.items():
            for match in pattern.finditer(content):
                hit(rule, "blocked", match.start())
        binary = b"\x00" in content[:8192]
        if not binary:
            for pattern in (ASSIGNMENT, DATABASE_URI, BEARER):
                for match in pattern.finditer(content):
                    value = next(group for group in match.groups() if group is not None)
                    if not synthetic(value):
                        hit("credential-literal", "blocked" if scope == "archive" else "review", match.start())
        for match in PEM.finditer(content):
            level = "blocked" if b"PRIVATE KEY" in match[1] or scope == "archive" else "review"
            hit("private-key-pem" if b"PRIVATE KEY" in match[1] else "certificate-pem", level, match.start())

        def scan_paths(data, base_offset=0):
            for match in PERSONAL_PATH.finditer(data):
                user = match[1].lower()
                examples = {b"username", b"user", b"alice", b"bob", b"example", b"public", b"default"}
                if scope == "archive" or user == self.home_name or user not in examples:
                    hit("personal-absolute-path", "blocked" if scope == "archive" else "review", base_offset + match.start())
            lowered = data.lower()
            for spelling in self.checkout_paths:
                offset = lowered.find(spelling)
                if offset >= 0:
                    hit("checkout-absolute-path", "blocked" if scope == "archive" else "review", base_offset + offset)

        scan_paths(content)
        if binary:
            for wide in WINDOWS_WIDE.finditer(content):
                scan_paths(wide[0][::2], wide.start())
        for (rule, level), offset in hits.items():
            line = None if binary else content.count(b"\n", 0, offset) + 1
            self.record(scope, name, rule, level, line, object_id)

    def worktree(self):
        names = sorted(set(git("ls-files", "-z", "--cached", "--others", "--exclude-standard").split(b"\x00")) - {b""})
        for raw in names:
            name = raw.decode("utf-8", errors="surrogateescape")
            path = ROOT / name
            try:
                if unsafe_worktree_path(Path(name)):
                    self.record("worktree", name, "symlink-or-escaped-path", "blocked")
                    continue
                if sensitive_file(name):
                    self.record("worktree", name, "sensitive-file-type", "blocked")
                if path.is_file() and self.reserve("worktree", name, path.stat().st_size):
                    self.inspect("worktree", name, path.read_bytes())
            except FileNotFoundError:
                # Deleted tracked files are covered by the history pass.
                continue
            except OSError:
                self.record("worktree", name, "unreadable-file", "blocked")

    def history(self):
        commits = git("rev-list", "--all", "HEAD", f"--max-count={MAX_COMMITS + 1}").decode().splitlines()
        self.totals["history"] = {"commitsScanned": len(commits), "filesScanned": 0, "bytesScanned": 0}
        if len(commits) > MAX_COMMITS:
            self.record("history", "[git history]", "commit-bound-exceeded", "blocked")
            return
        objects = {}
        sensitive_names = set()
        for commit in commits:
            objects[commit] = "[commit message]"
            for record in git("ls-tree", "-r", "-z", commit).split(b"\x00"):
                if not record:
                    continue
                header, name = record.split(b"\t", 1)
                _, kind, object_id = header.split()
                if kind == b"blob":
                    object_id = object_id.decode()
                    name = name.decode("utf-8", errors="surrogateescape")
                    if sensitive_file(name) and (name, object_id) not in sensitive_names:
                        self.record("history", name, "sensitive-file-type", "blocked", object_id=object_id)
                        sensitive_names.add((name, object_id))
                    objects.setdefault(object_id, name)
                    if len(objects) > MAX_FILES:
                        self.record("history", "[git history]", "scan-bound-exceeded", "blocked")
                        return
                elif kind == b"commit":
                    self.record("history", name.decode("utf-8", errors="surrogateescape"), "submodule-not-scanned", "blocked")
        with subprocess.Popen(
            ["git", "-C", str(ROOT), "cat-file", "--batch"], stdin=subprocess.PIPE,
            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
        ) as stream:
            for object_id, name in objects.items():
                stream.stdin.write(object_id.encode() + b"\n")
                stream.stdin.flush()
                header = stream.stdout.readline().split()
                if len(header) != 3:
                    self.record("history", name, "unreadable-git-object", "blocked", object_id=object_id)
                    break
                size = int(header[2])
                if not self.reserve("history", name, size):
                    stream.kill()
                    break
                content = read_exact(stream.stdout, size)
                read_exact(stream.stdout, 1)
                if header[1] == b"commit":
                    content = content.partition(b"\n\n")[2]
                self.inspect("history", name, content, object_id)
            stream.stdin.close()

    def archive(self, archive):
        if archive is None:
            return
        with archive.open("rb") as stream:
            if archive.stat().st_size > MAX_TOTAL_BYTES:
                self.record("archive", "[archive]", "scan-bound-exceeded", "blocked")
                return
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
        self.totals["archive"] = {"sha256": digest, "filesScanned": 0, "bytesScanned": 0}
        seen = set()
        with zipfile.ZipFile(archive) as package:
            for entry in package.infolist():
                name = entry.filename
                normalized = name.replace("\\", "/")
                path = PurePosixPath(normalized)
                mode = entry.external_attr >> 16
                if (normalized.startswith("/") or ":" in normalized or ".." in path.parts
                        or normalized.casefold() in seen or stat.S_ISLNK(mode)):
                    self.record("archive", name, "unsafe-archive-member", "blocked")
                    continue
                seen.add(normalized.casefold())
                if ZIP_EXCLUSIONS.search(normalized):
                    self.record("archive", name, "non-release-artifact", "blocked")
                if sensitive_file(normalized):
                    self.record("archive", name, "sensitive-file-type", "blocked")
                if not entry.is_dir() and self.reserve("archive", name, entry.file_size):
                    with package.open(entry) as stream:
                        content = stream.read(MAX_FILE_BYTES + 1)
                    if len(content) != entry.file_size:
                        self.record("archive", name, "member-size-mismatch", "blocked")
                    else:
                        self.inspect("archive", name, content)

    def report(self):
        scopes = {}
        for scope in ("worktree", "history", "archive"):
            total = self.totals.get(scope)
            if total is None and not self.levels[(scope, "blocked")]:
                scopes[scope] = {"status": "PENDING"}
                continue
            blocked = self.levels[(scope, "blocked")]
            review = self.levels[(scope, "review")]
            scopes[scope] = {
                "status": "BLOCKED" if blocked else "CONDITIONAL" if review else "VERIFIED",
                **(total or {}), "blockedFindings": blocked, "reviewFindings": review,
            }
        return {
            "scopes": scopes,
            "rules": {f"{scope}/{rule}": count for (scope, rule), count in sorted(self.rules.items())},
            "environmentValuesChecked": len(self.known_secrets),
            "findings": self.findings,
            "additionalFindingsOmitted": sum(self.rules.values()) - len(self.findings),
            "scopeNote": "Worktree includes tracked and nonignored untracked files. Git history includes reachable commits and distinct file blobs. Ignored profiles and unrelated disks are outside the scan. Reports contain no matched values.",
        }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path, help="Optional final ZIP to inspect")
    parser.add_argument("--output", type=Path, default=ROOT / "docs/release/evidence/secret-scan.json")
    args = parser.parse_args()
    audit = Audit()
    scans = (("worktree", audit.worktree), ("history", audit.history), ("archive", lambda: audit.archive(args.archive)))
    for scope, scan in scans:
        try:
            scan()
        except (OSError, subprocess.CalledProcessError, ValueError, RuntimeError, zipfile.BadZipFile) as error:
            audit.record(scope, "[scan]", type(error).__name__, "blocked")
    result = audit.report()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(result, ensure_ascii=True, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(result["scopes"], ensure_ascii=True, indent=2))
    return int(any(scope["status"] == "BLOCKED" for scope in result["scopes"].values()))


if __name__ == "__main__":
    sys.exit(main())
