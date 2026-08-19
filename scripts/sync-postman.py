#!/usr/bin/env python3
"""
sync-postman.py — Generate a synced Postman collection from the codebase.

Reads the manually-maintained collection as source of truth (never modifies it),
applies fixes (routes, auth, payloads), and writes the result to a separate
output file with a "last synced" timestamp.

Checks:
  1. Routes: missing from Postman, stale in Postman
  2. Auth: public routes with bearer, protected routes with noauth
  3. Payloads: request body fields missing vs Go model structs

Modes:
  --check   Report mismatches, exit 1 if any found (default)
  --fix     Apply fixes and write synced collection to --output path
  --json    Machine-readable JSON output (for SSE integration)

Usage:
  python3 scripts/sync-postman.py --check
  python3 scripts/sync-postman.py --fix --output ~/Automax/synced.postman_collection.json
"""

import argparse
import json
import os
import re
import sys
from datetime import datetime, timezone

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
MAIN_GO = os.path.join(REPO_ROOT, "cmd", "server", "main.go")
COLLECTION = os.path.join(REPO_ROOT, "automax-backend.postman_collection.json")
MODELS_DIR = os.path.join(REPO_ROOT, "internal", "models")
DEFAULT_OUTPUT = os.path.join(
    os.path.dirname(REPO_ROOT), "automax-backend-synced.postman_collection.json"
)


# ─── Route extraction from main.go ───────────────────────────────────────────


def extract_code_routes(main_go_path):
    """Parse main.go and return {(METHOD, normalized_path): is_protected}.
    First registration wins (handles duplicate route groups like public/auth OTP)."""
    with open(main_go_path) as f:
        lines = f.read().split("\n")

    groups = {}  # var -> {prefix, has_auth}
    seen_routes = {}

    for line in lines:
        s = line.strip()

        # Group definition: var := parent.Group("/path", middlewares...)
        m = re.match(r'(\w+)\s*:=\s*(\w+)\.Group\("([^"]*)"', s)
        if m:
            var, parent, prefix = m.groups()
            pi = groups.get(parent, {"prefix": "", "has_auth": False})
            inline_auth = (
                "authMiddleware.Authenticate()" in s
                or "authMiddleware.RequirePermission(" in s
            )
            groups[var] = {
                "prefix": pi["prefix"] + prefix,
                "has_auth": pi["has_auth"] or inline_auth,
            }

        # .Use(authMiddleware...) on existing group
        m2 = re.match(r"(\w+)\.Use\(authMiddleware\.Authenticate\(\)", s)
        if m2 and m2.group(1) in groups:
            groups[m2.group(1)]["has_auth"] = True

        # Route definition
        m3 = re.match(r'(\w+)\.(Get|Post|Put|Patch|Delete)\("([^"]*)"', s)
        if m3:
            gv, method, path = m3.groups()
            gi = groups.get(gv, {"prefix": "", "has_auth": False})
            full_path = (gi["prefix"] + path).rstrip("/") or "/"
            inline_auth = (
                "authMiddleware.Authenticate()" in s
                or "authMiddleware.RequirePermission(" in s
            )
            is_protected = gi["has_auth"] or inline_auth
            norm = re.sub(r":[a-zA-Z_]+", ":id", full_path)
            key = (method.upper(), norm)
            if key not in seen_routes:
                seen_routes[key] = is_protected

    return seen_routes


# ─── Postman collection parsing ──────────────────────────────────────────────


def load_collection(path):
    with open(path) as f:
        return json.load(f)


def save_collection(path, data):
    os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
    with open(path, "w") as f:
        json.dump(data, f, indent=2, ensure_ascii=False)
        f.write("\n")


def extract_postman_routes(data):
    """Return {(METHOD, norm_path): effective_auth_type}."""
    col_auth = data.get("auth", {}).get("type", "none")
    results = {}

    def walk(items, parent_auth):
        for item in items:
            folder_auth = parent_auth
            if "auth" in item and item["auth"] is not None:
                folder_auth = item["auth"].get("type", parent_auth)
            elif "auth" in item and item["auth"] is None:
                folder_auth = "noauth"

            if "item" in item:
                walk(item["item"], folder_auth)
            elif "request" in item:
                req = item["request"]
                url = req.get("url", {})
                raw = url.get("raw", "") if isinstance(url, dict) else url
                method = req.get("method", "")

                req_auth = folder_auth
                if "auth" in req and req["auth"] is not None:
                    req_auth = req["auth"].get("type", folder_auth)
                elif "auth" in req and req["auth"] is None:
                    req_auth = "noauth"

                path = raw.replace("{{base_url}}", "").split("?")[0].rstrip("/") or "/"
                norm = re.sub(r"\{\{[^}]+\}\}", ":id", path)
                norm = re.sub(r":[a-zA-Z_]+", ":id", norm)
                results[(method.upper(), norm)] = req_auth

    walk(data.get("item", []), col_auth)
    return results


# ─── Auth fix helpers ─────────────────────────────────────────────────────────

BEARER_AUTH = {
    "type": "bearer",
    "bearer": [{"key": "token", "value": "{{token}}", "type": "string"}],
}
NOAUTH = {"type": "noauth"}


def fix_auth_in_collection(data, code_routes):
    """Fix auth mismatches in-place on the COPY. Returns list of change descriptions."""
    public_code = {k for k, protected in code_routes.items() if not protected}
    protected_code = {k for k, protected in code_routes.items() if protected}
    col_auth = data.get("auth", {}).get("type", "none")
    changes = []

    def walk(items, parent_auth):
        for item in items:
            folder_auth = parent_auth
            if "auth" in item and item["auth"] is not None:
                folder_auth = item["auth"].get("type", parent_auth)
            elif "auth" in item and item["auth"] is None:
                folder_auth = "noauth"

            if "item" in item:
                walk(item["item"], folder_auth)
            elif "request" in item:
                req = item["request"]
                url = req.get("url", {})
                raw = url.get("raw", "") if isinstance(url, dict) else url
                method = req.get("method", "")

                eff_auth = folder_auth
                if "auth" in req and req["auth"] is not None:
                    eff_auth = req["auth"].get("type", folder_auth)
                elif "auth" in req and req["auth"] is None:
                    eff_auth = "noauth"

                path = raw.replace("{{base_url}}", "").split("?")[0].rstrip("/") or "/"
                norm = re.sub(r"\{\{[^}]+\}\}", ":id", path)
                norm = re.sub(r":[a-zA-Z_]+", ":id", norm)
                key = (method.upper(), norm)

                if "/ws" in norm:
                    continue

                is_bearer = eff_auth in ("bearer", "apikey", "oauth2")
                is_noauth = eff_auth == "noauth"

                if key in protected_code and is_noauth:
                    req["auth"] = BEARER_AUTH
                    changes.append(
                        f"AUTH-FIX: {method} {path} — added bearer (protected route)"
                    )
                if key in public_code and is_bearer:
                    req["auth"] = NOAUTH
                    changes.append(
                        f"AUTH-FIX: {method} {path} — set noauth (public route)"
                    )

    walk(data.get("item", []), col_auth)
    return changes


# ─── Payload struct extraction from Go models ────────────────────────────────


def extract_struct_fields(models_dir):
    """Parse Go model files and return {StructName: [json_field_name, ...]}."""
    structs = {}
    struct_re = re.compile(r"type\s+(\w+)\s+struct\s*\{")
    field_re = re.compile(r'json:"([^",]+)')

    for fname in os.listdir(models_dir):
        if not fname.endswith(".go"):
            continue
        with open(os.path.join(models_dir, fname)) as f:
            content = f.read()

        pos = 0
        while True:
            m = struct_re.search(content, pos)
            if not m:
                break
            struct_name = m.group(1)
            brace_start = content.index("{", m.end() - 1)
            depth = 1
            i = brace_start + 1
            while i < len(content) and depth > 0:
                if content[i] == "{":
                    depth += 1
                elif content[i] == "}":
                    depth -= 1
                i += 1
            body = content[brace_start + 1 : i - 1]
            fields = []
            for line in body.split("\n"):
                fm = field_re.search(line)
                if fm:
                    tag = fm.group(1)
                    if tag != "-":
                        fields.append(tag)
            if fields:
                structs[struct_name] = fields
            pos = i

    return structs


# ─── Payload comparison ──────────────────────────────────────────────────────

HANDLER_STRUCT_MAP = {
    "SettingsUpdateRequest": ["PUT /admin/settings"],
    "CallLogCreateRequest": ["POST /admin/call-logs"],
    "CallLogUpdateRequest": ["PUT /admin/call-logs/:id"],
    "CreateEscalationGroupRequest": ["POST /admin/escalation-groups"],
    "UpdateEscalationGroupRequest": ["PUT /admin/escalation-groups/:id"],
    "IncidentUpdateRequest": ["PUT /incidents/:id"],
    "UserLoginRequest": ["POST /auth/login"],
    "WorkflowTransitionCreateRequest": ["POST /transitions"],
    "WorkflowTransitionUpdateRequest": ["PUT /transitions/:id"],
    "PermissionCreateRequest": ["POST /admin/permissions"],
    "PermissionUpdateRequest": ["PUT /admin/permissions/:id"],
}


def extract_postman_body_fields(data):
    """Return {(METHOD, norm_path): set_of_field_names}."""
    results = {}

    def walk(items):
        for item in items:
            if "item" in item:
                walk(item["item"])
            elif "request" in item:
                req = item["request"]
                url = req.get("url", {})
                raw = url.get("raw", "") if isinstance(url, dict) else url
                method = req.get("method", "")
                path = raw.replace("{{base_url}}", "").split("?")[0].rstrip("/") or "/"
                norm = re.sub(r"\{\{[^}]+\}\}", ":id", path)
                norm = re.sub(r":[a-zA-Z_]+", ":id", norm)

                body_raw = req.get("body", {}).get("raw", "")
                if body_raw:
                    try:
                        parsed = json.loads(body_raw)
                        if isinstance(parsed, dict):
                            results[(method.upper(), norm)] = set(parsed.keys())
                    except (json.JSONDecodeError, TypeError):
                        pass

    walk(data.get("item", []))
    return results


def check_payloads(structs, postman_bodies):
    """Compare struct fields vs Postman body fields. Returns list of warnings."""
    warnings = []
    for struct_name, routes in HANDLER_STRUCT_MAP.items():
        if struct_name not in structs:
            continue
        code_fields = set(structs[struct_name])
        for route_pattern in routes:
            parts = route_pattern.split(" ", 1)
            method = parts[0]
            path = "/api/v1" + parts[1] if not parts[1].startswith("/api") else parts[1]
            norm = re.sub(r":[a-zA-Z_]+", ":id", path)
            key = (method, norm)
            postman_fields = postman_bodies.get(key)
            if postman_fields is None:
                continue
            missing = code_fields - postman_fields
            missing.discard("-")
            if missing:
                warnings.append(
                    f"PAYLOAD: {method} {path} — {struct_name} has fields missing from Postman: "
                    f"{', '.join(sorted(missing))}"
                )
    return warnings


def fix_payloads_in_collection(data, structs):
    """Add missing struct fields to Postman request bodies in the COPY. Returns changes."""
    changes = []

    def walk(items):
        for item in items:
            if "item" in item:
                walk(item["item"])
            elif "request" in item:
                req = item["request"]
                url = req.get("url", {})
                raw = url.get("raw", "") if isinstance(url, dict) else url
                method = req.get("method", "")
                path = raw.replace("{{base_url}}", "").split("?")[0].rstrip("/") or "/"
                norm = re.sub(r"\{\{[^}]+\}\}", ":id", path)
                norm = re.sub(r":[a-zA-Z_]+", ":id", norm)

                body_obj = req.get("body", {})
                body_raw = body_obj.get("raw", "")
                if not body_raw:
                    continue

                try:
                    parsed = json.loads(body_raw)
                except (json.JSONDecodeError, TypeError):
                    continue
                if not isinstance(parsed, dict):
                    continue

                for struct_name, routes in HANDLER_STRUCT_MAP.items():
                    if struct_name not in structs:
                        continue
                    for route_pattern in routes:
                        parts = route_pattern.split(" ", 1)
                        rmethod = parts[0]
                        rpath = (
                            "/api/v1" + parts[1]
                            if not parts[1].startswith("/api")
                            else parts[1]
                        )
                        rnorm = re.sub(r":[a-zA-Z_]+", ":id", rpath)
                        if method.upper() != rmethod or norm != rnorm:
                            continue

                        code_fields = set(structs[struct_name])
                        missing = code_fields - set(parsed.keys())
                        missing.discard("-")
                        if not missing:
                            continue

                        for field in sorted(missing):
                            parsed[field] = ""
                        body_obj["raw"] = json.dumps(parsed, indent=2)
                        changes.append(
                            f"PAYLOAD-FIX: {method} {path} — added missing fields: "
                            f"{', '.join(sorted(missing))}"
                        )

    walk(data.get("item", []))
    return changes


# ─── Route sync ──────────────────────────────────────────────────────────────


def check_routes(code_routes, postman_routes):
    """Compare route sets. Returns (missing_from_postman, stale_in_postman)."""
    code_keys = set(code_routes.keys())
    postman_keys = set(postman_routes.keys())
    return code_keys - postman_keys, postman_keys - code_keys


# ─── Timestamp embedding ─────────────────────────────────────────────────────


def embed_sync_timestamp(data):
    """Add/update 'last synced' note in the collection's info.description."""
    now = datetime.now(timezone.utc).strftime("%Y-%m-%d %H:%M:%S UTC")
    sync_line = f"Last auto-synced: {now}"

    info = data.get("info", {})
    desc = info.get("description", "")

    # Replace existing sync line or append
    if "Last auto-synced:" in desc:
        desc = re.sub(r"Last auto-synced:.*", sync_line, desc)
    else:
        desc = f"{desc}\n\n{sync_line}" if desc else sync_line

    info["description"] = desc.strip()
    data["info"] = info


# ─── Main ────────────────────────────────────────────────────────────────────


def main():
    parser = argparse.ArgumentParser(
        description="Sync Postman collection with codebase"
    )
    parser.add_argument(
        "--check",
        action="store_true",
        help="Report mismatches only (default behavior)",
    )
    parser.add_argument(
        "--fix",
        action="store_true",
        help="Apply fixes and write synced collection to --output",
    )
    parser.add_argument(
        "--output",
        default=DEFAULT_OUTPUT,
        help=f"Output path for the synced collection (default: {DEFAULT_OUTPUT})",
    )
    parser.add_argument(
        "--json", action="store_true", help="Machine-readable JSON output"
    )
    args = parser.parse_args()

    if not os.path.exists(MAIN_GO):
        print(f"ERROR: {MAIN_GO} not found", file=sys.stderr)
        sys.exit(2)
    if not os.path.exists(COLLECTION):
        print(f"ERROR: {COLLECTION} not found", file=sys.stderr)
        sys.exit(2)

    # 1. Determine which collection to read.
    #    If a synced copy already exists at --output, use it as the working base
    #    so that previous fixes accumulate. Otherwise bootstrap from the Backend
    #    original (first run).
    output_path = os.path.expanduser(args.output)
    if os.path.exists(output_path):
        working_collection = output_path
    else:
        working_collection = COLLECTION

    # 2. Extract data — routes/models come from code, collection from working copy
    code_routes = extract_code_routes(MAIN_GO)
    working_data = load_collection(working_collection)
    postman_routes = extract_postman_routes(working_data)
    structs = extract_struct_fields(MODELS_DIR)
    postman_bodies = extract_postman_body_fields(working_data)

    all_issues = []
    all_fixes = []

    # 2. Route check
    missing, stale = check_routes(code_routes, postman_routes)
    for method, path in sorted(missing):
        all_issues.append(
            f"ROUTE-MISSING: {method} {path} — in code but not in Postman"
        )
    for method, path in sorted(stale):
        all_issues.append(
            f"ROUTE-STALE: {method} {path} — in Postman but not in code"
        )

    # 3. Auth check
    public_code = {k for k, p in code_routes.items() if not p}
    protected_code = {k for k, p in code_routes.items() if p}
    for key, auth_type in postman_routes.items():
        method, path = key
        if "/ws" in path:
            continue
        is_bearer = auth_type in ("bearer", "apikey", "oauth2")
        is_noauth = auth_type == "noauth"
        if key in protected_code and is_noauth:
            all_issues.append(
                f"AUTH-MISMATCH: {method} {path} — needs bearer but has noauth"
            )
        elif key in public_code and is_bearer:
            all_issues.append(
                f"AUTH-MISMATCH: {method} {path} — public but has bearer"
            )

    # 4. Payload check
    payload_warnings = check_payloads(structs, postman_bodies)
    all_issues.extend(payload_warnings)

    # 5. Fix mode — apply fixes to the working data (which is either the
    #    existing synced copy or a fresh bootstrap from the original).
    #    The Backend original at COLLECTION is never modified.
    if args.fix:
        auth_fixes = fix_auth_in_collection(working_data, code_routes)
        payload_fixes = fix_payloads_in_collection(working_data, structs)
        all_fixes = auth_fixes + payload_fixes

        embed_sync_timestamp(working_data)
        save_collection(output_path, working_data)

        source_label = "synced copy" if working_collection == output_path else "Backend original (first run)"
        all_fixes.append(f"OUTPUT: wrote to {output_path} (base: {source_label})")

    # 6. Output
    if args.json:
        result = {
            "issues": all_issues,
            "fixes": all_fixes,
            "output_path": output_path if args.fix else None,
            "summary": {
                "routes_in_code": len(code_routes),
                "routes_in_postman": len(postman_routes),
                "routes_missing": len(missing),
                "routes_stale": len(stale),
                "auth_mismatches": sum(
                    1 for i in all_issues if i.startswith("AUTH-")
                ),
                "payload_mismatches": sum(
                    1 for i in all_issues if i.startswith("PAYLOAD")
                ),
                "fixes_applied": len(all_fixes),
            },
        }
        print(json.dumps(result, indent=2))
    else:
        if all_issues:
            print(f"=== {len(all_issues)} issue(s) found ===")
            for issue in all_issues:
                print(f"  {issue}")
        if all_fixes:
            print(f"\n=== {len(all_fixes)} fix(es) applied ===")
            for fix in all_fixes:
                print(f"  {fix}")
        if not all_issues and not all_fixes and not args.fix:
            print("Postman collection is in sync.")
        print(
            f"\nSummary: {len(code_routes)} code routes, "
            f"{len(postman_routes)} Postman routes, "
            f"{len(missing)} missing, {len(stale)} stale, "
            f"{sum(1 for i in all_issues if i.startswith('AUTH-'))} auth issues, "
            f"{sum(1 for i in all_issues if i.startswith('PAYLOAD'))} payload issues"
        )

    if all_issues and not args.fix:
        sys.exit(1)
    sys.exit(0)


if __name__ == "__main__":
    main()
