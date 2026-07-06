# OWASP, CWE & ASVS — the standards mapping

The tagging taxonomy for the review. Every finding that clears the reporting bar in `severity-model.md` gets a three-part label before it goes in the report:

1. **OWASP Top 10 category** — the class of failure, for the human skimming the report ("this is an access-control problem").
2. **CWE id** — the precise weakness, for dedup, trend tracking, and linking to a remediation reference.
3. **ASVS control** — the positive control that *fixes* it, so the report points at what right looks like instead of only what's wrong.

Tag from this file, not from memory. The categories drift year to year and a wrong id makes findings look sloppy and breaks cross-review consistency. `vulnerability-playbooks.md` tells you how to *find* each weakness; this file tells you how to *name* it.

## OWASP Top 10 (2021) — the category axis

Reproduce these ids and names verbatim. This is the stable mapping baseline for every review.

| ID | Category |
|---|---|
| **A01:2021** | Broken Access Control |
| **A02:2021** | Cryptographic Failures |
| **A03:2021** | Injection |
| **A04:2021** | Insecure Design |
| **A05:2021** | Security Misconfiguration |
| **A06:2021** | Vulnerable and Outdated Components |
| **A07:2021** | Identification and Authentication Failures |
| **A08:2021** | Software and Data Integrity Failures |
| **A09:2021** | Security Logging and Monitoring Failures |
| **A10:2021** | Server-Side Request Forgery (SSRF) |

**On the 2025 refresh.** OWASP is refreshing the Top 10 (a 2025 edition is in progress, with category names and rankings expected to shift). Until it is finalized and widely adopted, **tag against 2021** — it is the version tools, auditors, and compliance mappings still key on. When the 2025 list lands, update this table and re-map the cross-reference below; do not mix editions inside one report. Note for later: 2017's dedicated *A4 XML External Entities (XXE)* was folded into **A05 Security Misconfiguration** in 2021, and *A7 XSS* was folded into **A03 Injection** — reviewers who learned the old list should map accordingly.

## OWASP ASVS 5.0 — the positive-control checklist

The Top 10 is a list of what goes wrong; the **Application Security Verification Standard (ASVS) 5.0** is the list of what "correct" looks like — testable requirements, organized into chapters, gated by three assurance levels (**L1** opportunistic / L2 standard for most apps / **L3** high-assurance). Use it to phrase the *fix* on a finding and to run a positive sweep ("is this control present?") rather than only a negative one ("is this bug present?").

ASVS 5.0 renumbered its chapters. Map each OWASP category to the ASVS chapter(s) that supply its positive control:

| OWASP category | ASVS 5.0 chapter(s) — the control to verify present |
|---|---|
| A01 Broken Access Control | V8 Authorization (deny-by-default, enforce at the trust boundary), V7 Session Management |
| A02 Cryptographic Failures | V11 Cryptography (approved algorithms, key management, secure random), V12 Secure Communication (TLS), V14 Data Protection |
| A03 Injection | V1 Encoding and Sanitization (context-correct output encoding), V2 Validation and Business Logic (parameterized queries, allow-list input) |
| A04 Insecure Design | V15 Secure Coding and Architecture (threat modeling, secure-by-default, defense in depth), V2 business-logic limits |
| A05 Security Misconfiguration | V13 Configuration (hardening, headers, no debug/defaults in prod), V5 File Handling |
| A06 Vulnerable and Outdated Components | V13 Configuration / V15 Architecture (dependency inventory, patch process, no unmaintained components) |
| A07 Identification and Authentication Failures | V6 Authentication, V7 Session Management, V9 Self-contained Tokens, V10 OAuth and OIDC |
| A08 Software and Data Integrity Failures | V1/V2 (safe deserialization), V15 Architecture (verify integrity of updates, CI/CD, and untrusted data) |
| A09 Security Logging and Monitoring Failures | V16 Security Logging and Error Handling (log security events, no sensitive data in logs, fail closed) |
| A10 Server-Side Request Forgery | V2 Validation (allow-list outbound destinations), V4 API and Web Service, V12 Secure Communication |

You do not need to cite an exact ASVS requirement number in every finding; naming the chapter and the control ("ASVS V8 — enforce authorization server-side, deny by default") is enough to point the reader at the fix.

## CWE Top 25 (2024) — the weakness taxonomy

The **CWE Top 25 Most Dangerous Software Weaknesses** is the precise-id axis. Tag the CWE that names the *specific* weakness, then roll it up to its OWASP category. Below is the 2024 edition (the most recent finalized list; the 2025 edition follows the same data-driven methodology and re-ranks the same weakness pool — swap the ids in when it publishes, the taxonomy is continuous). These are the ~15 you will reach for most often:

| CWE | Name | Rolls up to |
|---|---|---|
| **CWE-79** | Improper Neutralization of Input During Web Page Generation (XSS) | A03 |
| **CWE-89** | SQL Injection | A03 |
| **CWE-78** | OS Command Injection | A03 |
| **CWE-77** | Command Injection | A03 |
| **CWE-20** | Improper Input Validation | A03 (cross-cuts) |
| **CWE-22** | Improper Limitation of a Pathname to a Restricted Directory (Path Traversal) | A01 |
| **CWE-352** | Cross-Site Request Forgery (CSRF) | A01 |
| **CWE-862** | Missing Authorization | A01 |
| **CWE-863** | Incorrect Authorization | A01 |
| **CWE-434** | Unrestricted Upload of File with Dangerous Type | A04 / A05 |
| **CWE-287** | Improper Authentication | A07 |
| **CWE-306** | Missing Authentication for Critical Function | A07 |
| **CWE-798** | Use of Hard-coded Credentials | A07 / A02 |
| **CWE-502** | Deserialization of Untrusted Data | A08 |
| **CWE-918** | Server-Side Request Forgery (SSRF) | A10 |
| **CWE-125** | Out-of-bounds Read | (memory-safety; no direct OWASP web category) |
| **CWE-787** | Out-of-bounds Write | (memory-safety; no direct OWASP web category) |
| **CWE-416** | Use After Free | (memory-safety) |

The memory-safety entries (CWE-125, -787, -416, plus CWE-119/-476/-190) dominate the CWE Top 25 because it spans all software, not just web apps — they have no clean OWASP Top 10 home. Tag them with the CWE id alone and note "memory safety (C/C++/unsafe)" in the finding; do not force an OWASP category that does not fit.

**SANS is not a separate list.** "SANS Top 25" and "CWE Top 25" are the same lineage — the list originated as the *CWE/SANS Top 25 Most Dangerous Software Errors* and is now maintained by MITRE as the **CWE Top 25 Most Dangerous Software Weaknesses**. If a user or tool says "SANS Top 25", they mean this list. Do not tag a finding twice or present them as two standards.

## OWASP ↔ CWE cross-map

When you have the OWASP category but need a specific CWE (or want to sanity-check a roll-up), use this. It lists the CWEs OWASP itself maps to each 2021 category — pick the one that matches the exact weakness, not just the first row.

| OWASP category | Common mapped CWEs |
|---|---|
| **A01 Broken Access Control** | CWE-22 Path Traversal, CWE-352 CSRF, CWE-862 Missing Authorization, CWE-863 Incorrect Authorization, CWE-639 Authorization Bypass Through User-Controlled Key (IDOR), CWE-284 Improper Access Control, CWE-200 Sensitive Info Exposure, CWE-269 Improper Privilege Management |
| **A02 Cryptographic Failures** | CWE-327 Broken/Risky Crypto Algorithm, CWE-326 Inadequate Encryption Strength, CWE-331 Insufficient Entropy, CWE-321 Hard-coded Crypto Key, CWE-916 Weak Password Hash, CWE-798 Hard-coded Credentials |
| **A03 Injection** | CWE-79 XSS, CWE-89 SQLi, CWE-78 OS Command Injection, CWE-77 Command Injection, CWE-94 Code Injection, CWE-20 Improper Input Validation, CWE-116 Improper Encoding/Escaping of Output |
| **A04 Insecure Design** | CWE-73 External Control of File Name/Path, CWE-209 Sensitive Info in Error Message, CWE-256 Plaintext Credential Storage, CWE-522 Insufficiently Protected Credentials, CWE-602 Client-Side Enforcement of Server-Side Security, CWE-434 Unrestricted Upload |
| **A05 Security Misconfiguration** | CWE-16 Configuration, CWE-611 XXE, CWE-732 Incorrect Permission Assignment, CWE-1004 Sensitive Cookie Without HttpOnly, CWE-548 Directory Listing Exposure |
| **A06 Vulnerable and Outdated Components** | CWE-1104 Use of Unmaintained Third-Party Components, CWE-937 OWASP Top Ten Known Vulnerable Components, CWE-1035 (SCA class) |
| **A07 Identification and Authentication Failures** | CWE-287 Improper Authentication, CWE-306 Missing Authentication for Critical Function, CWE-798 Hard-coded Credentials, CWE-384 Session Fixation, CWE-620 Unverified Password Change, CWE-640 Weak Password Recovery, CWE-521 Weak Password Requirements |
| **A08 Software and Data Integrity Failures** | CWE-502 Deserialization of Untrusted Data, CWE-345 Insufficient Verification of Data Authenticity, CWE-494 Download of Code Without Integrity Check, CWE-829 Inclusion of Functionality from Untrusted Control Sphere, CWE-565 Reliance on Cookies Without Integrity |
| **A09 Security Logging and Monitoring Failures** | CWE-778 Insufficient Logging, CWE-117 Improper Output Neutralization for Logs (log injection), CWE-223 Omission of Security-relevant Information, CWE-532 Insertion of Sensitive Information into Log File |
| **A10 Server-Side Request Forgery** | CWE-918 SSRF |

## Tagging rules

- **One primary CWE per finding.** Pick the most specific weakness that describes the root cause. If a bug is both "missing check" and "wrong check", it is one or the other — decide, don't tag both.
- **Roll the CWE up to exactly one OWASP category** using the cross-map. When a CWE legitimately spans two (e.g. CWE-798 hard-coded credentials is both A07 and A02), pick the one that matches the *impact in this codebase* and mention the other in prose.
- **Prefer the specific over the generic.** Use CWE-89 (SQLi) over CWE-20 (Improper Input Validation) when the sink is a SQL query; reserve CWE-20 for validation gaps with no more specific sink.
- **Cite ASVS as the remediation anchor**, not as a category — it belongs in the "how to fix" line of the finding, pointing at the control that would have prevented it.
- **Do not invent ids.** If nothing here fits, look up the exact CWE rather than approximating; a plausible-but-wrong id is worse than "CWE: (unmapped — see description)".
