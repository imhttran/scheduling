---
type: log
title: Session Memory
description: Running log of context, decisions, and project state across sessions
tags: [log, context, decisions]
timestamp: 2026-07-23T00:00:00Z
---

# Memory

Running log of context, decisions, and project state. Prevents conversations from having to restart from zero. Append new entries at the bottom. Update existing entries when new information supersedes old information rather than duplicating it.

## Format

**[Date] — [Topic]**
What happened or was decided. Why, if relevant. Any next steps or open questions.

---

## Log

**2026-06-15 — Setup**
Tom set up his global context system today. Created about-me.md, writing-rules.md, and memory.md in ~/codework/about-me/. Updated CLAUDE.md to reference all three as required reading at the start of every session. Jira board is TS, team is Atlas, focus areas are TeamServer, eop, hub, agent-dashboard.

**2026-06-15 — Workspace structure update**
Established a consistent folder convention for ~/codework. All outputs go to ~/codework/outputs/, with a subfolder per project. Updated about-me.md to reflect this.

**2026-06-16 — Agent Dashboard projects cloned**
Tom cloned three related repos into ~/codework/Contrast/:

- agent-dashboard (https://github.com/Contrast-Security-Inc/agent-dashboard) — Java/Gradle monorepo, requires Java 25. Build with `./gradlew clean build`.
- agent-dashboard-deployment (https://github.com/Contrast-Security-Inc/agent-dashboard-deployment) — Helm charts, staging is the primary supported environment.
- contrast-adr_postgres-tfp (https://github.com/Contrast-Security-Inc/contrast-adr_postgres-tfp) — Terraform for ADR Postgres resources. Requires SSH tunnel before plan/apply. Code in src/main/tf/.

**2026-07-15 — Knowledge system consolidated into knowledge/**
A duplicate memory system that had grown in ~/codework/memory/ was reconciled and migrated into ~/codework/knowledge/ (OKF-formatted, updated by the okf-sync skill via daily Drive digests). Project tracking (CVE Shield, SSRF hardening, EOP Ubuntu 24.04, downloads cleanup) moved to knowledge/systems/. People (atlas-team, Nicholas Scott, fjpgtt) moved to knowledge/references/atlas-team.md. Glossary merged into knowledge/concepts/glossary.md.

**2026-07-23 — memory/ and projects/ deleted; CLAUDE.md updated**
~/codework/memory/ and ~/codework/projects/ removed — all content was superseded by knowledge/. CLAUDE.md session start now loads knowledge/ (index.md, references/company.md, references/atlas-team.md, concepts/glossary.md, systems/index.md) instead. knowledge/references/company.md created to hold company context and Tom's role (the one piece not previously in knowledge/). This file converted to OKF format.
