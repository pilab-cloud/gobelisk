# Gobelisk Project Setup (PiRat)

This document describes how to work with the Gobelisk project in PiRat
(`https://pira.hu`, project key: `GOBELISK`).

## Project

- **Key**: `GOBELISK`
- **Name**: Gobelisk
- **Description**: Gobelisk is a next-generation PBX management system written purely in Go.
- **URL**: https://github.com/pilab-cloud/gobelisk

## Issue Types

| Type | Use for |
|---|---|
| Epic | Large initiatives spanning multiple tasks/stories |
| Story | User-facing feature or capability |
| Task | Technical work item |
| Subtask | Breakdown of a task/story |
| Bug | Defect or unexpected behavior |
| Improvement | Enhancement to existing functionality |

## Workflow / Methodology

Gobelisk uses a lightweight Scrum-style flow:

- **Open** → **In Progress** → **In Review** → **Done**
- Bugs may go **Open → Triaged → In Progress**.
- Epics: **Open → In Progress → Done**.

Every change should be tracked by an issue before work starts.

## Components

Use one of the following components when filing an issue:

- Core SIP
- Trunks & Routing
- IVR
- Voicemail
- Conferencing
- Call Center
- Recording & Analytics
- Pir2 Integration
- System Admin
- Integrations

## Priority

- **blocker**: Must be fixed immediately; blocks release.
- **high**: Important for upcoming release.
- **medium**: Normal priority.
- **low**: Nice to have.
- **trivial**: Cosmetic/minor.

## Severity (for bugs)

Set `severity` separately from priority (critical, major, minor, trivial).

## Required Fields

When filing an issue, provide at least:

- **Type** (Epic/Story/Task/Bug/Improvement/Subtask)
- **Summary** (title)
- **Description** (what and why)
- **Component**
- **Priority**
- **Acceptance Criteria** (for stories/tasks)
- **Labels** (e.g. `roadmap`, `bug`, `feature`)

## Optional but Recommended

- Story Points (effort estimate)
- Sprint assignment
- Epic Link (for tasks/stories)
- Fix Version / Affects Version
- Environment
- Steps to Reproduce (for bugs)
- Expected vs Actual (for bugs)
- Root Cause (for bugs)

## How to File an Issue

1. Go to https://pira.hu and select project **Gobelisk**.
2. Click **Create**.
3. Choose the appropriate **Issue Type**.
4. Fill in **Summary**, **Description**, **Component**, **Priority**.
5. Add **Acceptance Criteria** and **Labels**.
6. Link to an **Epic** if applicable.
7. Assign to a sprint if scheduled.

## Issue Linking

- Tasks and stories should link to their parent epic via **Epic Link**.
- Use issue links for dependencies (blocks / is blocked by).

## Naming Convention

- Use concise, action-oriented titles: "Implement X", "Fix Y", "Add Z".
- Prefix bugs with "Bug:" if not using the Bug type.

## Branching

- Branch name: `feature/GOBELISK-<id>-short-description`
- Commit messages: `GOBELISK-<id>: description`
