# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in `sdk-go`, please report it
**privately** by emailing **security@instanode.dev**.

Please include:

- A clear description of the issue and its impact.
- Steps to reproduce (proof-of-concept code, request/response captures, etc.).
- The affected version or commit SHA.
- Any suggested mitigation, if known.

We commit to:

- Acknowledging your report within **2 business days**.
- Providing an initial assessment within **7 days**.
- Working toward a fix and coordinated disclosure within **90 days** of the
  initial report, faster for actively exploited or critical-severity issues.

Please do **not** open a public GitHub issue, pull request, or discussion for
suspected vulnerabilities until we have coordinated a release.

## Scope

In scope:

- The Go SDK code in this repository.
- Documentation that describes authentication, credential handling, or
  cryptographic behavior in a way that could mislead users into insecure
  configurations.

Out of scope:

- Vulnerabilities in third-party dependencies (please report those upstream;
  we will fast-track upgrades once a fix is available).
- Issues in the [instanode.dev](https://instanode.dev) platform itself —
  please report those to the same `security@instanode.dev` address but note
  that the platform is tracked separately.
- Social engineering, physical attacks, or denial-of-service against
  third-party infrastructure.

## Safe Harbor

We support safe-harbor research conducted in good faith and in accordance with
this policy. We will not pursue legal action against researchers who:

- Make a good-faith effort to avoid privacy violations, destruction of data,
  or interruption or degradation of our services.
- Only interact with accounts you own or have explicit permission to access.
- Give us reasonable time to investigate and mitigate before any public
  disclosure.

Thank you for helping keep InstaNode users safe.
