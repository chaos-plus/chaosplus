# Security Policy

Chaosplus IAM is an authentication, authorization, and tenant-management
service. Security issues are handled with priority.

## Reporting a vulnerability

Use GitHub private vulnerability reporting for this repository
(Code Security > Report a vulnerability). Do not open a public issue.

Please include:

- The affected component (Go backend, `apps/admin`, `docs`, deployment).
- A minimal reproduction: configuration, request, and observed behavior.
- Impact and, if known, a suggested fix.

## What happens next

- The report is acknowledged within 5 business days.
- The issue is triaged, fixed on `iam-ds`, and covered by a regression test
  before the fix is announced.
- A coordinated disclosure is published after a release containing the fix.

## Scope

In scope: the code in this repository.

Out of scope: vulnerabilities in third-party dependencies; report those to
their respective upstream projects. Never include live credentials, secrets,
or real tenant data in a report.
