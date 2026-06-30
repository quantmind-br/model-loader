package skills

import json
import os
import re

def grader(output):
    try:
        data = json.loads(output)
        return data
    except Exception as e:
        return {"error": f"failed to parse audit: {e}"}
