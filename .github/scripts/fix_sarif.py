#!/usr/bin/env python3
"""
Fix gosec SARIF file format to ensure artifactChanges is an array.
This fixes the issue: "instance.runs[0].results[X].fixes[0].artifactChanges is not of a type(s) array"
"""
import json
import sys

try:
    with open('gosec.sarif', 'r') as f:
        data = json.load(f)
    
    # Process each run
    for run in data.get('runs', []):
        for result in run.get('results', []):
            fixes = result.get('fixes', [])
            for fix in fixes:
                if 'artifactChanges' in fix:
                    artifact_changes = fix['artifactChanges']
                    # If it's not a list, wrap it in a list
                    if not isinstance(artifact_changes, list):
                        fix['artifactChanges'] = [artifact_changes]
    
    with open('gosec.sarif', 'w') as f:
        json.dump(data, f, indent=2)
    
    print("✅ SARIF file fixed successfully")
    sys.exit(0)
except FileNotFoundError:
    print("⚠️ No gosec.sarif file found, skipping")
    sys.exit(0)
except Exception as e:
    print(f"⚠️ Error fixing SARIF: {e}")
    sys.exit(0)

