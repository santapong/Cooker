DRAFT -- for review

# Cooker GitHub Sponsors — tier copy

> Context: product-plan §7 classifies sponsorships as signal, not income. Realistic early
> revenue at this stage is $0–200/month. These tiers exist to give community members a
> concrete way to express support and to make contributor health visible to future employers,
> collaborators, and users evaluating Cooker for production use. They are not a revenue line.

---

## Why sponsor

Cooker is a self-hosted CI/CD tool built and maintained by one person in the open. There is
no company, no VC runway, and no paid team. Sponsoring keeps the project actively maintained:
it signals that real users depend on it, which is the honest argument for prioritising a bug
fix over a weekend, publishing a new guide, or reviewing an outside PR. If you are running
Cooker in production or find the codebase useful as a reference, a sponsorship is the most
direct way to say so.

This is not a support contract and it is not a hosted service. Perks are acknowledgements,
not deliverables. The core project remains Apache-2.0 at every tier.

---

## Tiers

### Supporter — $5 / month

For individuals who want to say thanks and keep the lights on.

Perks:
- Name in the SUPPORTERS section of the README (alphabetical, no logos).
- Access to the sponsors-only GitHub Discussions category for release notes and roadmap notes
  before they are posted publicly.

---

### Backer — $25 / month

For developers who use Cooker regularly and want to back continued development.

Perks:
- Everything in Supporter.
- Name listed on the project website's sponsors page (when the site exists).
- Issues you file are tagged `backer-reported` — not a priority queue, but a visible signal
  that the reporter is invested in the project.

---

### Sponsor — $100 / month

For teams or individuals who rely on Cooker in production and want that reflected in the
project's visible community.

Perks:
- Everything in Backer.
- Company or personal name (text, no logo) in the README's SPONSORS section, above the
  supporter list.
- One GitHub Discussions post per quarter where you can describe your deployment and
  use-case — useful for the project's case-study record and for other users evaluating
  Cooker.

---

### Org Sponsor — $250 / month

For organisations that depend on Cooker as part of their engineering infrastructure and want
logo-level visibility in the project.

Perks:
- Everything in Sponsor.
- Company logo (SVG, max 120 px wide) in the README's ORG SPONSORS section, linked to your
  site. Logo is added within five business days of the first successful payment.
- Logo on the project website's homepage sponsors block (when the site exists).
- A brief "how we use Cooker" writeup (supplied by you, reviewed for accuracy) published as
  a case study in the project docs.

Note: logo placement is contingent on the organisation's public web presence being consistent
with the project's values (no spyware, no malware, no deceptive-pattern products). The
maintainer reserves the right to decline or remove a logo at any time with a full refund of
the current month.

---

## A note on expectations

Sponsorship revenue at this stage will realistically be $0–200/month (product-plan §7).
That does not mean the tiers are not worth setting up — it means they should be read as a
community health signal rather than a funding source. A project with ten $25 backers is
demonstrably more adopted than one with zero, regardless of whether $250/month changes
anyone's economics.

Open Collective is listed as a secondary option in FUNDING.yml (currently commented out, pending
collective creation). Use it if your organisation needs an invoice or transparent fiscal
accounting; individual developers will generally find GitHub Sponsors simpler.

---

## Activation checklist

- [ ] GitHub Sponsors profile enabled for the `santapong` account (requires US bank or
      Stripe Connect, W-9 or W-8 form, 30-day approval window).
- [ ] Tiers created in the GitHub Sponsors UI matching the copy above.
- [ ] FUNDING.yml moved from `docs/marketing/research/launch-kit/FUNDING.yml` to
      `.github/FUNDING.yml` in the repo root.
- [ ] README updated with SUPPORTERS / SPONSORS / ORG SPONSORS sections (can be empty
      placeholders at launch).
- [ ] Open Collective created and slug added to FUNDING.yml if invoice-based donations
      are wanted.
