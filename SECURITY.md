# Security Policy

## Disclaimer

This repository is an open-source research version provided for research, review, and experimentation only. It has not completed a comprehensive production security audit or full safety validation.

Use it at your own risk. BlockSec does not guarantee the security, correctness, availability, or fitness of this software, and is not responsible for security incidents, functional failures, transaction errors, key loss, asset loss, or any direct or indirect losses arising from use of this repository or derived deployments.

## Reporting a Vulnerability

If you discover a security vulnerability in Web3 Companion, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, email us at: **contact@blocksec.com**

Include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

We will acknowledge your report within 48 hours and aim to provide a fix within 7 days for critical issues.

## Scope

The following are in scope:
- Authentication bypass (Passkey, JWT)
- Private key exposure or SSM encryption weaknesses
- Unauthorized transaction signing
- Injection attacks (SQL, command, XSS)
- Privilege escalation

The following are out of scope:
- Denial of service on a self-hosted instance you control
- Social engineering
- Issues in third-party dependencies (report upstream)

## Disclosure

We practice coordinated disclosure. We will credit reporters in the release notes unless they prefer anonymity.
