# Recommended Custom Fields for Gobelisk / PiRat

For effective roadmap + development lifecycle tracking, use roughly **25–30 custom fields**
on top of built-in fields (status, priority, assignee, reporter, labels, components,
versions, due date, time tracking).

## Recommended custom fields (grouped)

### Planning / Roadmap (6)
1. Epic Link
2. Sprint
3. Story Points
4. Business Value
5. Team
6. Target Release / Fix Version (version field)

### Development (5)
7. Acceptance Criteria
8. Estimated Time (often built-in, but keep)
9. Remaining Time (built-in)
10. Risk / Complexity
11. Dependencies / Blocked By (or issue links)

### Quality / Bug tracking (8)
12. Severity
13. Reproducibility
14. Environment
15. Found In
16. Steps to Reproduce
17. Expected vs Actual
18. Root Cause
19. Resolution / Triage Outcome

### Release management (3)
20. Affects Version
21. Fix Version
22. Release Notes / Change Summary

### Agent / Automation (optional, 3–5)
23. Agent Plan fields (omo.*)
24. AI Progress
25. Evidence / QA Scenarios

## Current state in PiRat

PiRat already provides **21 custom fields**, covering most of the above.  
To fully support roadmap + lifecycle tracking, add **4–6 more**:

- Risk / Complexity
- Dependencies / Blocked By (or use issue links)
- Release Notes / Change Summary
- Customer / Stakeholder
- Test Coverage / QA Status

That yields about **25–27 custom fields**, a healthy Jira-like setup without
overloading the UI.
