#!/usr/bin/env python3
"""Provision the GOBELISK project in PiRat via GraphQL API.

Requires PIRAT_TOKEN env var.
Endpoint: https://pira.hu/api/graphql
"""
import json
import os
import urllib.request

TOKEN = os.environ.get("PIRAT_TOKEN", "")
URL = os.environ.get("PIRAT_GRAPHQL_URL", "https://pira.hu/api/graphql")

COMPONENTS = [
    ("Core SIP", "Core SIP signaling features"),
    ("Trunks & Routing", "Trunks and route management"),
    ("IVR", "Interactive voice response"),
    ("Voicemail", "Voicemail system"),
    ("Conferencing", "Conference bridge"),
    ("Call Center", "Queues and ring groups"),
    ("Recording & Analytics", "Call recording and CDR"),
    ("Pir2 Integration", "Pir2 meeting dial-in"),
    ("System Admin", "Administration and security"),
    ("Integrations", "External integrations"),
]


def gql(query, variables=None):
    payload = {"query": query}
    if variables:
        payload["variables"] = variables
    req = urllib.request.Request(
        URL,
        data=json.dumps(payload).encode(),
        headers={
            "Authorization": f"Bearer {TOKEN}",
            "Content-Type": "application/json",
        },
    )
    with urllib.request.urlopen(req) as resp:
        return json.loads(resp.read().decode())


def main():
    if not TOKEN:
        raise SystemExit("PIRAT_TOKEN is required")

    # Update project metadata
    gql(
        'mutation { updateProject(input: { id: "GOBELISK", description: "Gobelisk is a next-generation PBX management system written purely in Go. Repository: https://github.com/pilab-cloud/gobelisk" }) { id } }'
    )

    # Create components
    comps = [{"name": n, "description": d} for n, d in COMPONENTS]
    gql(
        "mutation($comps: [ProjectComponentInput!]!) { updateProject(input: { id: \"GOBELISK\", components: $comps }) { id components { name } } }",
        {"comps": comps},
    )

    # Create initial version
    gql(
        'mutation { updateProject(input: { id: "GOBELISK", versions: [{ name: "0.1.0", description: "Initial SIP edge release", released: false }] }) { id } }'
    )

    print("Provisioning complete.")


if __name__ == "__main__":
    main()
