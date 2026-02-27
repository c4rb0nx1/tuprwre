# Starship Launch Action Items (Agent-Executable)

## Mission
Turn this repository from **2.4/10 launch readiness** into a polished OSS project that is easy to star, fork, and adopt.

Target: complete **P0** before public launch, then finish **P1** in week 1, and **P2** for growth.

---

## Baseline Audit Snapshot
- README quality: **5/10**
- Repo metadata: **2/10**
- Community health: **1/10**
- First impressions: **2/10**
- Discoverability: **2/10**

Overall launch readiness: **2.4/10**.

---

## Execution Rules for Agents
1. Execute tasks in strict order: **P0 → P1 → P2**.
2. Do not skip blockers in P0.
3. For every task, produce proof (file diff, command output, or URL).
4. Keep all changes in small PRs if possible:
   - PR-1: README + CI + metadata docs
   - PR-2: community health files/templates
   - PR-3: repo cleanup + changelog/release
5. If a command fails due to permissions (especially repo settings), report exact failure and provide manual fallback.

---

## P0 — Must Complete Before Public Launch (Today)

### P0-1) Fix installation credibility immediately
**Goal:** remove trust blockers.

#### Required actions
- Update `go.mod` module path from placeholder to real path:
  - from `github.com/c4rb0nx1/tuprwre`
  - to `github.com/c4rb0nx1/tuprwre`
- Align Go version messaging between `README.md` and `go.mod` (single canonical minimum version).

#### Validation
- `go.mod` has correct module path.
- README prerequisite version matches `go.mod` policy.

---

### P0-2) Rebuild README top 60 lines for conversion
**Goal:** improve first-impression conversion to star/adoption.

#### Required actions
- Keep existing hook.
- Add demo visual directly under headline:
  - `assets/demo.gif` (or screenshot fallback)
- Add install-in-under-30-seconds quickstart near top.
- Add compatibility line:
  - `Works with Cursor / Claude Code / OpenCode`
- Add badges at top:
  - CI status
  - latest release
  - Go version
  - license
  - GitHub stars
- Add CTA:
  - `⭐ Star this repo` and sponsor link.
- Move early-stage warning below quickstart (not above proof/demo).

#### Suggested README top section outline
1. Title + one-line value prop
2. Badges row
3. Demo GIF/image
4. 30-second quickstart
5. Works with tools line
6. CTA (Star/Sponsor)
7. Early-stage warning

#### Validation
- First 60 lines contain all conversion elements above.

---

### P0-3) Add CI workflow and badge
**Goal:** establish trust with visible test automation.

#### Required actions
- Create `.github/workflows/ci.yml` with:
  - triggers: `push`, `pull_request`
  - setup Go
  - run `go test ./...`
- Add CI badge to README header.

#### Validation
- Workflow file exists and runs successfully on GitHub Actions.
- README badge points to this workflow.

---

### P0-4) Fix GitHub repo metadata now
**Goal:** improve SEO and discoverability.

#### Required actions
- Set SEO-friendly description:
  - `Sandbox install commands for AI coding agents. Intercept apt/npm/pip on host, run safely in Docker, and use installed tools via transparent shims.`
- Set homepage URL (at least README URL):
  - `https://github.com/c4rb0nx1/tuprwre#readme`
- Add up to 10 topics:
  - `ai-agents`
  - `sandbox`
  - `docker`
  - `cli`
  - `devsecops`
  - `ai-safety`
  - `agentic-workflows`
  - `shell`
  - `security`
  - `supply-chain-security`
- Upload a custom social preview image (1280x640) with clear before/after message.

#### Agent commands (GitHub CLI)
```bash
gh repo edit c4rb0nx1/tuprwre \
  --description "Sandbox install commands for AI coding agents. Intercept apt/npm/pip on host, run safely in Docker, and use installed tools via transparent shims." \
  --homepage "https://github.com/c4rb0nx1/tuprwre#readme"

for topic in ai-agents sandbox docker cli devsecops ai-safety agentic-workflows shell security supply-chain-security; do
  gh repo edit c4rb0nx1/tuprwre --add-topic "$topic"
done
```

#### Note
- Social preview image may require web UI if CLI/API permissions are limited.

#### Validation
- `gh repo view --json description,homepageUrl,repositoryTopics,usesCustomOpenGraphImage`

---

### P0-5) Add community health baseline
**Goal:** make contribution path clear and safe.

#### Required actions
Create these files:
- `CONTRIBUTING.md`
- `CODE_OF_CONDUCT.md`
- `.github/ISSUE_TEMPLATE/bug_report.yml`
- `.github/ISSUE_TEMPLATE/feature_request.yml`
- `.github/PULL_REQUEST_TEMPLATE.md`

Enable GitHub Discussions with categories:
- Q&A
- Ideas
- Show-and-tell

#### Agent command (if permissions allow)
```bash
gh repo edit c4rb0nx1/tuprwre --enable-discussions
```

#### Validation
- Templates appear in GitHub issue/PR creation flow.
- Discussions are enabled.

---

## P1 — Week 1 Polish

### P1-6) Clean repo surface
**Goal:** remove noise from first impression.

#### Required actions
- Remove tracked noise from root:
  - `.DS_Store`
  - `output.json`
  - `test.json`
- Expand `.gitignore`:
  - `.DS_Store`
  - local/debug artifacts
- Optional: move internal-only files (`AGENTS.md`, `.beads/*`) under `docs/internal/` if not contributor-facing.

#### Validation
- Root directory is clean and purpose-driven.

---

### P1-7) Add releases + changelog
**Goal:** establish shipping cadence and trust.

#### Required actions
- Add `CHANGELOG.md` using Keep a Changelog structure.
- Create first release tag:
  - `v0.1.0-alpha.1` (acceptable)
- Publish GitHub release notes and attach binaries.

#### Agent command examples
```bash
git tag v0.1.0-alpha.1
git push origin v0.1.0-alpha.1

# Optional example (adjust assets as needed)
gh release create v0.1.0-alpha.1 \
  --title "v0.1.0-alpha.1" \
  --notes "Initial alpha release with shell interception, sandbox install, shim lifecycle, and doctor checks." \
  build/tuprwre*
```

#### Validation
- Tag exists.
- Release page exists with notes and artifacts.

---

### P1-8) Pin onboarding issues
**Goal:** direct new contributors fast.

#### Required actions
Create/pin 2–3 onboarding issues:
- `🚀 Roadmap to v0.1`
- `🟢 Good first issue: Homebrew tap`
- `🎬 Demo GIF improvements`

#### Agent command examples
```bash
gh issue create --title "🚀 Roadmap to v0.1" --body "Milestones and scope for the first stable release."
gh issue create --title "🟢 Good first issue: Homebrew tap" --label "good first issue" --body "Set up and document Homebrew installation path."
gh issue create --title "🎬 Demo GIF improvements" --body "Add and optimize demo GIF for README conversion."
```

#### Optional (GraphQL pinning, if needed)
- Use GitHub GraphQL API `pinIssue` mutation with issue node IDs.

#### Validation
- Pinned issues are visible at top of repo issues page.

---

## P2 — Discoverability Growth

### P2-9) Improve SEO and awesome-list eligibility
**Goal:** increase inbound discovery and social proof.

#### Required actions
Add README sections:
- `Why not just devcontainers?`
- `Threat model`
- `Who this is for`

Add practical evidence:
- real-world examples in `examples/`
- one benchmark-style comparison (even lightweight)

Then submit to relevant awesome lists:
- AI agents
- DevSecOps CLI
- container tooling

#### Validation
- README includes all sections.
- `examples/` contains concrete usage.
- Submission PRs to awesome lists are opened.

---

## Suggested File/Folder Additions Summary
- `.github/workflows/ci.yml`
- `.github/ISSUE_TEMPLATE/bug_report.yml`
- `.github/ISSUE_TEMPLATE/feature_request.yml`
- `.github/PULL_REQUEST_TEMPLATE.md`
- `CONTRIBUTING.md`
- `CODE_OF_CONDUCT.md`
- `CHANGELOG.md`
- `assets/demo.gif` (or screenshot)
- `examples/` (new usage examples)
- README updates

---

## Completion Checklist (Definition of Done)
- [ ] P0-1 module path/version alignment complete
- [ ] P0-2 README conversion-focused top section complete
- [ ] P0-3 CI workflow live + badge green
- [ ] P0-4 metadata/topics/homepage updated + social preview customized
- [ ] P0-5 community files/templates/discussions enabled
- [ ] P1-6 root cleanup complete + `.gitignore` expanded
- [ ] P1-7 changelog + tag + GitHub release published
- [ ] P1-8 onboarding issues created and pinned
- [ ] P2-9 SEO sections/examples/awesome-list submissions complete

When all boxes are checked, re-run a launch audit and target **>=8/10** overall.
