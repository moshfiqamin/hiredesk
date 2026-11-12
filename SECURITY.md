# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for a security problem. Report it
privately through GitHub's
[private vulnerability reporting](https://github.com/moshfiqamin/hiredesk/security/advisories/new)
instead, with the steps to reproduce it and the impact you expect.

You will get an acknowledgement within a few days. A fix is developed in a
private fork and released with an advisory that credits you, unless you prefer
otherwise.

## Scope

This is a portfolio project, not a hosted service: there is no production
deployment to test against. Reports about the code itself are welcome, for
example an authorization bypass, token handling, SQL built from user input,
or a secret committed to the history.

## What is already in place

- Passwords are hashed with bcrypt. Refresh tokens are stored only as hashes,
  rotated on every use, and reuse of an old token revokes every session of
  that user.
- Every protected route is covered by an authorization matrix test that runs
  against a real PostgreSQL in CI.
- A secret scan runs over the full git history on every push.
- The API refuses to start with a missing or weak `JWT_SECRET`, and the
  development seeder refuses to run when `ENV=production`.
- Credentials never appear in logs. A test fails if a password, token or
  `Authorization` header is written at any log level.
