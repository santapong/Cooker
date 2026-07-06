// Template: SECURITY REVIEW — finder-per-category → dedup barrier → adversarial verify.
// A specialization of the workflow skill's parallel fan-out pattern with the OWASP/CWE
// categories, severity model, and false-positive suppression pre-wired.
// The barrier is earned per harness policy H2: the same weakness surfaces from several
// category lenses, so dedup needs ALL finder output at once (and lets us early-exit on zero).
//
// Invoke with: Workflow({ script, args: { target: "...", scope: "..." } })
// input.target — what to review (a diff, a PR, a path, a repo). Used in every finder prompt.
// input.scope  — one-line scope note (e.g. "only changed lines in git diff main...HEAD").

export const meta = {
  name: 'security-review', // EDIT ME
  description: 'Fan out one finder per vuln category, dedup across all lenses, then adversarially verify each survivor', // EDIT ME
  phases: [
    { title: 'Find', detail: 'one finder per OWASP / vuln category' },
    { title: 'Verify', detail: 'one skeptic per deduped finding, prompted to refute it' },
  ],
}

// Some harnesses deliver args as a JSON-encoded string — normalize before use.
const input = typeof args === 'string' ? JSON.parse(args) : args

// EDIT ME: one finder per category from SKILL §4. Diversity beats redundancy (harness policy
// H4): each finder hunts a DIFFERENT weakness class, not the same code from N identical angles.
// Trim categories that cannot apply to this target (e.g. no crypto in a static-site diff) and
// log the trim below rather than running empty finders.
const FINDERS = [
  { key: 'injection', prompt: 'Injection: SQL, NoSQL, OS command, LDAP, and template injection. Trace each tainted source (request, file, env, third party) to its dangerous sink (query, exec, deserializer, template). Tag CWE-89/78/77/79/94/20; OWASP A03.' },
  { key: 'access-control', prompt: 'Broken access control / authorization: IDOR, missing or incorrect authorization checks, privilege escalation, path traversal, CSRF. Tag CWE-862/863/639/22/352; OWASP A01.' },
  { key: 'auth-session', prompt: 'Authentication & session management: missing auth on critical functions, weak session handling, fixation, broken password reset. Tag CWE-287/306/384/620/640; OWASP A07.' },
  { key: 'crypto', prompt: 'Cryptographic failures: broken/weak algorithms, bad or predictable randomness, misused primitives, hard-coded crypto keys, weak password hashing. Tag CWE-327/326/331/321/916; OWASP A02.' },
  { key: 'ssrf', prompt: 'Server-side request forgery: user-controlled outbound destinations reaching an internal network or metadata endpoint without an allow-list. Tag CWE-918; OWASP A10.' },
  { key: 'deserialization', prompt: 'Insecure deserialization / data-integrity failures: untrusted data fed to a deserializer, unsigned updates, unverified data authenticity. Tag CWE-502/345/494; OWASP A08.' },
  { key: 'secrets', prompt: 'Hard-coded secrets & credential leakage: API keys, passwords, tokens, private keys committed in source or config; credentials in logs. Tag CWE-798/321/522; OWASP A07/A02.' },
  { key: 'validation-encoding', prompt: 'Input validation & output encoding: reflected/stored XSS, path traversal, open redirect, unrestricted file upload. Confirm a context-correct encoding or allow-list is missing at the sink. Tag CWE-79/22/601/434; OWASP A03/A01.' },
  { key: 'misconfiguration', prompt: 'Security misconfiguration: missing/loose security headers, permissive CORS, debug flags in prod, insecure defaults, XXE, directory listing. Tag CWE-16/611/732/1004; OWASP A05.' },
  { key: 'components', prompt: 'Vulnerable & outdated components (SCA): dependencies with known CVEs, unmaintained packages, pinned-vulnerable versions in manifests/lockfiles. Tag CWE-1104/937; OWASP A06.' },
  { key: 'logging', prompt: 'Security logging & monitoring failures: absent logging of security events, sensitive data written to logs, log injection, fail-open error handling. Tag CWE-778/117/532; OWASP A09.' },
  // EDIT ME: add project-specific lenses (e.g. framework-specific auth, tenant isolation, a
  // house crypto wrapper) or drop rows that cannot apply to this target.
]

// Finders return raw candidate findings — their final text is a return value, not prose (H3).
const FINDINGS_SCHEMA = {
  type: 'object',
  properties: {
    findings: {
      type: 'array',
      items: {
        type: 'object',
        properties: {
          title: { type: 'string' }, // short weakness name
          detail: { type: 'string' }, // the source→sink trace that makes it exploitable
          location: { type: 'string' }, // file:line or file:symbol
          category: { type: 'string' }, // OWASP Top 10 id, e.g. "A03:2021"
          cwe: { type: 'string' }, // single most-specific CWE id, e.g. "CWE-89"
          severity: { type: 'string', enum: ['high', 'medium', 'low'] },
          confidence: { type: 'number' }, // finder's prior; the verify stage sets the real gate
        },
        required: ['title', 'detail', 'location', 'category', 'cwe', 'severity', 'confidence'],
      },
    },
  },
  required: ['findings'],
}

// Adversarial verdict: re-derive exploitability from scratch and default to refuted (H4).
const VERDICT_SCHEMA = {
  type: 'object',
  properties: {
    isReal: { type: 'boolean' }, // true only if a concrete source→sink exploit path was re-derived
    confidence: { type: 'number' }, // 0..1 — how sure the skeptic is in isReal
    reason: { type: 'string' }, // what path was proven, or why it was refuted
  },
  required: ['isReal', 'confidence', 'reason'],
}

// The false-positive bar (SKILL §5). EDIT ME if the user set a different threshold.
const MIN_CONFIDENCE = 0.8

// BARRIER (harness policy H2): wait for every finder, because the same weakness surfaces from
// multiple category lenses and dedup needs the full result set. This is also what lets us
// early-exit when nothing was found instead of spinning up an empty verify stage.
const sweeps = await parallel(
  FINDERS.map((f) => () =>
    agent(
      `Security review target: ${input.target}\nScope: ${input.scope || 'as given by the target'}\n\n` +
        `${f.prompt}\n\n` +
        `Report ONLY findings with a concrete, reachable source→sink path — a pattern match with no ` +
        `exploit path is not a finding. Return raw data conforming to the schema.`,
      { label: `find:${f.key}`, phase: 'Find', schema: FINDINGS_SCHEMA },
    ),
  ),
)

// .filter(Boolean): a skipped or dead finder resolves to null (harness policy H5).
const all = sweeps.filter(Boolean).flatMap((s) => s.findings)

// Dedup in plain JS — never spend an agent on it (loop policy L3). Key on location+title so the
// same weakness reported by two lenses collapses to one candidate before the expensive verify.
const key = (f) => `${f.location}::${f.title}`
const deduped = [...new Map(all.map((f) => [key(f), f])).values()]
log(`${all.length} raw candidates from ${FINDERS.length} finders → ${deduped.length} after dedup`) // no silent caps (H6)

// Early-exit: the other legitimate use of the barrier (H2). Nothing to verify.
if (deduped.length === 0) {
  return { confirmed: [], note: 'no candidate findings from any category' }
}

// Adversarial verify (harness policy H4): one independent skeptic per candidate, prompted to
// REFUTE it and default to isReal=false when the exploit path is unproven.
const verified = await parallel(
  deduped.map((f) => () =>
    agent(
      `You are a skeptical reviewer trying to REFUTE a reported vulnerability. Re-derive its ` +
        `exploitability from the source yourself — do not trust the finder.\n\n` +
        `Finding: ${f.title} [${f.category} / ${f.cwe}, severity=${f.severity}]\n` +
        `Location: ${f.location}\n` +
        `Claimed path: ${f.detail}\n\n` +
        `Set isReal=true ONLY if you can re-derive a concrete, reachable source→sink exploit path. ` +
        `If the input is already sanitized, the sink is safe, the code is unreachable, or you are ` +
        `unsure — set isReal=false. Report your confidence in isReal as 0..1.`,
      { label: `verify:${f.title}`, phase: 'Verify', schema: VERDICT_SCHEMA },
    ).then((v) => ({ ...f, verdict: v })),
  ),
)

// Keep only findings the skeptic confirmed with high confidence — the §5 bar.
const survivors = verified.filter(Boolean)
const confirmed = survivors.filter((f) => f.verdict.isReal && f.verdict.confidence >= MIN_CONFIDENCE)

// Log everything dropped, never truncate silently (harness policy H6).
const dropped = survivors.filter((f) => !(f.verdict.isReal && f.verdict.confidence >= MIN_CONFIDENCE))
log(`${confirmed.length}/${deduped.length} findings confirmed at confidence ≥ ${MIN_CONFIDENCE}`)
for (const f of dropped) {
  log(`dropped ${f.title} @ ${f.location} — isReal=${f.verdict.isReal} conf=${f.verdict.confidence}: ${f.verdict.reason}`)
}

return { confirmed }
