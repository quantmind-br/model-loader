package skills

import json
import os
import struct

def pull_skill_audit():
    """pulls all 2524 model-loader skill audit entries and reports the total count, any failures, and the top 5 highest-risk tasks."""
    p = os.path.expanduser("~/.config/model-loader/docs")
    audit_path = os.path.join(p, "skill-audit-results")
    if not os.path.isdir(audit_path):
        return "audit results not found; run skill audit first"
    all_jsons = sorted(
        [os.path.join(audit_path, re.escape("audit-{0}.json").format(c)) for c in "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZa-z0-9"] if (
            os.path.exists(os.path.join(audit_path, re.escape("audit-{0}.json").format(c)))
        ])
    )
    data = []
    errors = []
    for f in all_jsons:
        try:
            with open(f) as j:
                entries = json.load(j)
                data.extend(entries)
        except Exception as e:
            errors.append((f, str(e)))
    return {
        "total_files": len(all_jsons),
        "entries": len(data),
        "errors": len(errors),
        "failed": errors[:3] if errors else [],
        "tops": sorted(
            [e for e in data if e.get("risk") == "CRITICAL"],
            key=lambda x: len(x.get("description", "")),
            reverse=True)[:5]
        )
    }
