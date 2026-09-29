# Source catalog

Status: Draft v0.2 (2026-09-29). This document is the human-readable seed for `data/sources.yaml`, which also
derives the **egress allowlist** (`docs/security.md §2.3`). Every entry declares a `trust` tier:
`official` (WPILib, FIRST), `vendor` (vendor docs/APIs), `community` (forums, Reddit, YouTube, non-vendor release-note
bodies). Structured payloads (vendordep JSON, Maven metadata, release metadata, specs) land in relational fact tables,
not embeddings.
⚠️ = not yet verified by a probe (see `docs/ingestion.md §5`). Primary research: `docs/research/02-*.md`.

Legend — **Detect**: change-detection rung (`docs/ingestion.md §2`). **Floor**: minimum poll interval. **Prior**:
expected changes/day in the off-season (scaled by the season multiplier). **Pri**: scheduler weight.

## 0. Verified during M1 ingestion (2026-09-29)

- **frc-docs has been renamed to `wpilibsuite/wpilib-docs`.** The license is **CC BY 4.0** (`license.txt`).
  Redistribution with attribution is allowed; every chunk carries its citation.
- **allwpilib is BSD-3-Clause.**
- **Docs are ingested from the Read the Docs htmlzip** (`/_/downloads/en/{stable,latest}/htmlzip/`). One request per
  version covers the whole site, and remote code includes are already resolved.
  - The 2026 build uses `sphinx_rtd_theme`; the 2027 ("latest") build uses **Furo**.
  - The adapter supports both. A theme change would otherwise have emptied a shard silently.
- **`github.wpilib.org/robots.txt` disallows `/allwpilib/docs/beta/` and `/docs/development/`.** 2027 Javadoc
  therefore comes from the versioned Maven artifact
  `org/wpilib/wpilibj/documentation/<ver>/documentation-<ver>.zip`. The 2026 equivalent is
  `edu/wpi/first/wpilibj/documentation/2026.2.2/`. github.wpilib.org is used for links only and is not on the
  fetch allowlist.
- **frcmaven.wpi.edu** has no robots.txt and redirects downloads to `storage.googleapis.com`. **huggingface.co**
  redirects to `*.hf.co`. Both hosts are on the allowlist with the reason documented in `data/sources.yaml`.
- **The 2027 renames seen in the docs** are: `ChassisSpeeds` → `ChassisVelocities`; the new OpMode framework and
  Utility Mode; `commandbased/commands-v2/`; the FIRST Driver Station replacing the NI DS; Systemcore pages; and a
  `yearly-overview/removed-features` page.

- **Vendordep catalog** (`wpilibsuite/vendor-json-repo`):
  - Folder listings come from the GitHub contents API: 107 files for 2026 and 8 for `2027_alpha7`. Individual JSONs
    come from `raw.githubusercontent.com` (ETag, `max-age=300`).
  - The `*_metadata.json` files hold only uuid, name and description; version data lives in the per-year folders.
  - Catalog quirks we keep verbatim: `PathplannerLib-2025.1.2.json` in the 2026 folder contains version 2026.1.2.
    Phoenix 6 for 2027 is already `26.70.0-alpha-2`, newer than the research's alpha-1.

### 0.1 Verified during M3 vendor ingestion (2026-09-29)

- **Seasons come from pinned refs, never from `main` / `latest`.** Vendor `main` branches already carry 2027-alpha code
  (PhotonVision C++ examples use `wpi::math`, Choreo's install page points at `ChoreoLib2027Alpha.json`). 2026 docs are
  therefore read at the last 2026 release tag, 2027 docs at the newest alpha tag:

  | Library | 2026 ref | 2027 ref | Site generator → adapter |
  |---|---|---|---|
  | CTRE Phoenix 6 | RTD `stable` htmlzip (127 MB, 116 pages; Furo) | — (not yet ingested) | Sphinx → `sphinx-htmlzip` |
  | PhotonVision | `v2026.3.4` | `v2027.0.0-alpha-2` | MyST Markdown in `docs/source/` → `github-markdown` |
  | AdvantageKit | `v26.0.2` | `v27.0.0-alpha-6` | Docusaurus in `docs/docs/` → `github-markdown` |
  | Choreo / ChoreoLib | `v2026.0.3` | `v2027.0.0-alpha-3` | MkDocs Material in `docs/` → `github-markdown` |
  | PathPlannerLib | `v2026.1.2` | — (no 2027 release yet) | Writerside topics → `github-markdown` |
  | REVLib | GitBook (unversioned) | — | `llms.txt` + per-page `.md` → `gitbook-llms` |
  | YAGSL | GitBook (unversioned) | — | `llms.txt` + per-page `.md` → `gitbook-llms` |

- **Repository docs are read file by file, not as a tarball.** The git tree API lists the files for a ref (one request),
  then only the Markdown under `include` is fetched from `raw.githubusercontent.com`. PhotonVision's codeload tarball is
  about 195 MB and takes over 100 s for roughly 100 Markdown files. A tag's files never change, so re-runs are served
  from the conditional-GET cache.
- **PhotonVision** serves versioned docs (`/en/v2026.3.4/…`); `/en/stable/` returns 404 for many pages, so links use the
  tag path. AdvantageKit, Choreo, PathPlanner, REV and YAGSL publish a single version, so links point at the live site.
- **GitBook** (REV, YAGSL) publishes `llms.txt` (a Markdown link list of every page's `.md` rendition) and
  `llms-full.txt`. `llms-full.txt` has no page boundaries or URLs, so the adapter uses `llms.txt`. REV's `llms.txt`
  covers every REV product; `include: /revlib/` keeps REVLib only.
- `docs.photonvision.org` has no htmlzip download enabled; `pathplanner.dev` and `docs.advantagekit.org` publish no
  `llms.txt`.

### 0.1.1 Verified during forum ingestion (2026-09-29)

Chief Delphi (`www.chiefdelphi.com`, Discourse behind Cloudflare), probed by hand with an identifying User-Agent:

- **`/robots.txt`** (`cache-control: max-age=14400`): `User-agent: *` disallows `/admin/`, `/auth/`, `/email/`,
  `/session`, `/user-api-key`, `/*?api_key*`, `/badges`, `/my`, `/search`, `/tag/*/l`, `/g`, **`/t/*/*.rss`** and
  **`/c/*.rss`**. `/latest.rss` and `/posts.rss` are allowed. The **category feeds planned in §5 are disallowed**,
  so they are not used; the category filter runs on the `<category>` element of `latest.rss` instead.
- **Category ids** (from the `/categories` and `/c/technical/9` HTML pages, not from the disallowed feeds): the plan's
  `programming/11` and `control-systems/9` were wrong. Actual: `technical/9`, `technical/programming/30`,
  `technical/control-system/72`, `technical/java/75`, `technical/c-c/74`, `technical/python/77`, `technical/can/76`,
  `technical/sensors/70`, `technical/motors/31`, `technical/electrical/32`, `technical/photonvision/87`,
  `technical/technical-discussion/25`.
- **Feeds:** `latest.rss` has the 30 most recently active topics (title, `dc:creator`, `<category>`, the first post's
  cooked HTML plus a "N posts - M participants" / "Read full topic" footer, `pubDate`, GUID
  `www.chiefdelphi.com-topic-<id>`). `posts.rss` has the 50 newest posts (no category; `dc:creator` is
  `@username Display Name`; link `/t/<slug>/<topic>#post_<n>`; GUID `www.chiefdelphi.com-post-<id>`). Both answer
  `cache-control: no-cache, no-store` with **no ETag or Last-Modified**, so change detection is the byte hash (rung 4)
  and `min_interval: 30m` is the politeness floor.
- **[Terms of Service](https://www.chiefdelphi.com/tos)** — nothing forbids automated access or feed readers. §3
  "User Content License": *"User contributions are licensed under a Creative Commons Attribution-NonCommercial-ShareAlike
  3.0 Unported License."* The [FAQ / guidelines](https://www.chiefdelphi.com/guidelines) add "Post Only Your Own
  Stuff" and nothing about reuse or crawling. The site is open to users from 13 years old.
- **Decision:** ingest, but **never redistribute**. Posts carry `LicenseRef-ChiefDelphi-UserContent`, not the
  CC BY-NC-SA 3.0 id: redistributing would require per-post author attribution, a ShareAlike shard license and
  honoring moderator removals in copies we cannot recall, and many authors are minors. `index publish` never ships a
  `LicenseRef-*-UserContent` shard, even with `--include-unlicensed`; the `forum` shard exists only in locally built
  indexes. The shard mirrors the feeds' rolling window (no archive), so posts removed upstream disappear on the next
  build.
- **First real build (2026-09-29 17:27 UTC):** 30 topics → 6 kept in allowed categories (Programming ×3, Control
  System, Motors, Technical), 24 filtered; 50 replies → 0 kept (none were in those 6 topics; the window was dominated
  by forum-game threads), 0 invalid after the `?page=N` fix, **0 suspect**. `forum` shard: 6 chunks. The real-index
  detector check (`TestRealIndexNotSuspect`) stays at 0 flags over 17,298 chunks. Forum recall is therefore limited
  to what is active at build time; an archive would need its own policy for moderator removals.

### 0.1.2 Vendor C++ APIs (probed 2026-09-29)

**Decision: the C++ tables come from each vendor's Maven `headers.zip`, not from their Doxygen sites.** Every C++
vendordep in the catalog lists a `headerClassifier: headers` artifact next to its binaries. That zip is the public
API the compiler sees, pinned to the exact vendordep version, and it is one small download per release. The Doxygen
sites are crawled HTML that track a single release, so they are used for links only. The `cpp-headers-zip` adapter
(`internal/ingest/source/cppheader`) scans declarations; `docs/ingestion.md` §2.2 describes it.

What the probe found (HEAD/GET with the indexer User-Agent):

| Library | Artifacts (Maven) | Size | License shipped in the zip | Doxygen site (links) |
|---|---|---|---|---|
| CTRE Phoenix 6 | `com/ctre/phoenix6/wpiapi-cpp/{26.3.0,26.70.0-alpha-2}/…-headers.zip` and `…/tools/…-headers.zip` (`signals::*Value`, `ctre::phoenix::StatusCode`) | 573 / 578 KB + 93 / 92 KB | `CTRE_LICENSE.txt`: CTRE EULA | `api.ctr-electronics.com/phoenix6/{stable,latest}/cpp/` (banners read 26.3.0 and 26.70.0-alpha-2) |
| REVLib | `com/revrobotics/frc/REVLib-cpp/{2026.0.5,2027.0.0-alpha-7}/…-headers.zip` (redirects to REV-Software-Binaries release assets) | 142 / 145 KB | `LICENSE.txt` and every header: BSD-3-Clause | `codedocs.revrobotics.com/cpp/` (one version) |
| PhotonLib | `org/photonvision/{photonlib-cpp,photontargeting-cpp}/{v2026.3.4,v2027.0.0-alpha-2}/…-headers.zip` | 34 + 50 KB | photonlib-cpp: MIT; photontargeting-cpp: GPL-3.0 | `javadocs.photonvision.org/cpp/` (the 2027 alpha; `/release/cpp/` is 404) |

- **No bulk Doxygen artifact exists.** `…-docs.zip`, `…-doxygen.zip`, `…-documentation.zip` and a `.tag` file all
  return 404 on the three Maven repos and sites. Crawling the sites would take hundreds of pages per release
  (CTRE's `TalonFX` page alone is 373 KB) and would still give only the current release.
- **Robots:** the three Maven hosts, `api.ctr-electronics.com` and `codedocs.revrobotics.com` serve no robots.txt
  (404); `javadocs.photonvision.org/robots.txt` is empty. The build fetches one zip per artifact and no site page.
- **Links:** Doxygen names pages from the FQN (`classctre_1_1phoenix6_1_1hardware_1_1_talon_f_x.html`), so links are
  computed. 268 of CTRE's 278 documented 2026 class pages (274 of 288 for 2027) match a computed page name exactly;
  the rest are `impl` classes, which frc-mcp drops by design, and `std::hash`/`wpi::Struct` specializations. A
  random sample of 35 computed REV, PhotonVision and CTRE pages, plus namespace pages, all returned 200.
- **Left out on purpose:** CTRE's `controls/compound/` headers (compound differential requests) and `spns/` (raw
  signal ids) are not in CTRE's own API reference for either season, so `skip` drops them.
- **Namespaces:** `ctre::phoenix6::…` (plus `ctre::phoenix::StatusCode` and, in 2026, `ctre::phoenix::unmanaged`),
  `rev::`, `rev::spark::`, `photon::`. These match the verifier's C++ namespace map (`internal/verify/refcheck.go`).
- **Built tables** (`frc-mcp index run`, 2026-09-29):

  | Library | 2026 symbols (types) | 2027 symbols (types) | C++ moves mapped by the season diff |
  |---|---|---|---|
  | Phoenix 6 | 6,064 (287) — 26.3.0 | 6,077 (294) — 26.70.0-alpha-2 | 7 (`ctre::phoenix::unmanaged::*` → `ctre::phoenix6::unmanaged::*`) |
  | REVLib | 1,652 (148) — 2026.0.5 | 1,591 (149) — 2027.0.0-alpha-7 | 0 (84 symbols removed, e.g. `SparkBase#Get/Set`, `SparkClosedLoopController#SetReference`) |
  | PhotonLib | 428 (36) — v2026.3.4 | 423 (36) — v2027.0.0-alpha-2 | 0 |

## 0.2 Licensing and redistribution

Shards redistribute text, so every source records the license of its **documentation** (which can differ from the
code license):

| Library | Docs license | Redistributed? |
|---|---|---|
| WPILib docs | CC BY 4.0 | yes |
| CTRE Phoenix 6 docs | CC BY-NC-ND 4.0 (`license.txt` in `CrossTheRoadElec/Phoenix6-Documentation`) | yes. frc-mcp is free and non-commercial, and NoDerivatives 4.0 permits sharing the material "in whole or in part"; converting format (HTML → Markdown) is a technical modification, not an adaptation (§2(a)(4)). Every excerpt keeps its citation. |
| PhotonVision docs | CC BY 4.0 (`docs/LICENSE`) | yes |
| AdvantageKit docs | BSD-3-Clause (repository `LICENSE`) | yes |
| Choreo docs | BSD-3-Clause | yes |
| PathPlanner docs | MIT | yes |
| REVLib docs | none published | yes, by decision (`LicenseRef-REV-Docs-NoLicense`) |
| REV and WCP motor spec pages (`frc_hardware` rows `rev-docs`, `wcp-docs`) | none published | yes, by decision (`LicenseRef-REV-Docs-NoLicense`, `LicenseRef-WCP-Docs-NoLicense`; shard `hardware-restricted`) |
| ReCalc motor table (`recalc`) | MIT (`tervay/recalc` `LICENSE.txt`) | yes |
| YAGSL docs | none published (`YAGSL-Gitbook` has no license; the library itself is LGPL-2.1) | yes, by decision (`LicenseRef-YAGSL-Docs-NoLicense`) |
| Phoenix 6 Java API (Javadoc from source comments) | none found in `wpiapi-java` jars (the javadoc jar's `legal/` is the JDK doclet's own license) | yes, by decision (`LicenseRef-CTRE-Phoenix-API`) |
| REVLib Java API | none found in `REVLib-java` jars | yes, by decision (`LicenseRef-REVLib-API`) |
| PhotonLib / PhotonTargeting Java API | GPL-3.0 (repository) | yes |
| Phoenix 6 C++ API (headers) | CTRE EULA (`CTRE_LICENSE.txt` in the zip): the Software, "including … documentation", may not be distributed or made available to any third party | **never** (`LicenseRef-CTRE-Phoenix-EULA`, shard `vendor-eula-api-*`; not even with `--include-unlicensed`) |
| REVLib C++ API (headers) | BSD-3-Clause (`LICENSE.txt` and each header) | **yes** (`vendor-api-*`, with the BSD notice in the citation), like the robotpy-rev Python tables; REVLib Java (no license) stays in `vendor-restricted-api-*` |
| PhotonLib C++ API (headers) | photonlib-cpp: MIT; photontargeting-cpp: GPL-3.0 (the `LICENSE` in each zip) | yes |
| PathPlannerLib Java API | MIT | yes |
| ChoreoLib Java API | BSD-3-Clause | yes |
| AdvantageKit Java API | BSD-3-Clause | yes |
| YAGSL Java API | LGPL-2.1 | yes |
| Python APIs (PyPI wheels) | RobotPy, robotpy-rev, choreolib: BSD-3-Clause; photonlibpy, pathplannerlib: MIT; CTRE `phoenix6`: none declared | yes (`phoenix6` by decision: `LicenseRef-CTRE-Phoenix-API`, shard `vendor-restricted-api-*`) |
| Release notes (GitHub releases) | the repository's license (BSD-3/MIT/GPL-3.0/LGPL-2.1); CTRE `Phoenix-Releases` and REV `REV-Software-Binaries` have none | yes (CTRE/REV by decision: `LicenseRef-*-Release-Notes`, shard `releases-restricted`) |
| Chief Delphi posts | CC BY-NC-SA 3.0 per ToS §3 (user content) | **never** (`LicenseRef-ChiefDelphi-UserContent`, shard `forum`; not even with `--include-unlicensed`; §0.1.1) |

`LicenseRef-*` marks content without a redistribution grant, in three classes:

- **States no license** (REV and YAGSL docs, REV/WCP spec pages, Phoenix 6 Java and Python, REVLib Java, CTRE/REV
  release notes): published and served **by the Foundation's decision** (2026-09-30), because the publishers make it
  freely available for FRC teams to use. The index workflow passes `--include-unlicensed`, and so does the hosted
  server. Every excerpt keeps its citation, and links back to the publisher. A publisher that objects gets its content
  removed: give its sources a prohibited id (`LicenseRef-<Publisher>-EULA`) or remove them; the next daily index
  no longer carries it.
- **Forbids distribution** (`LicenseRef-*-EULA`: CTRE's C++ header EULA): **never** published or served by a shared
  server, whatever the flags (`sources.Prohibited`), in shards of their own (`vendor-eula-api-*`). Build it locally.
- **User content** (`LicenseRef-*-UserContent`: Chief Delphi posts): **never** published or served (§0.1.1).

**Open:** the permission requests to CTRE, REV Robotics and the YAGSL maintainers still go out; a written grant is
linked here when it arrives. CTRE's header EULA also forbids "derivative works"; whether a locally built C++ symbol
table is acceptable to CTRE belongs in the same request.

## 1. WPILib core

| Source | Endpoint(s) | Detect | Floor | Prior | Pri |
|---|---|---|---|---|---|
| allwpilib releases | GraphQL batch; fallback `https://github.com/wpilibsuite/allwpilib/releases.atom` | marker (`tagName`) + ETag | 15 m | 0.1 | critical |
| frc-docs source (stable + latest) | `wpilibsuite/frc-docs` commits `?path=source` (⚠️ possible rename to `wpilib-docs`; GitHub redirects) | commit SHA → tree diff | 1 h | 1.0 | critical |
| Known issues / yearly changelog | part of frc-docs (`docs/yearly-overview/…`) | norm hash | 1 h | 0.2 | critical |
| Java API (Javadoc) | `https://github.wpilib.org/allwpilib/docs/release/java/` (+ `/beta/`) | release-triggered | — | — | high |
| C++ API (Doxygen) | `https://github.wpilib.org/allwpilib/docs/release/cpp/` | release-triggered | — | — | high |
| RobotPy | PyPI JSON `https://pypi.org/pypi/{robotpy,wpilib,…}/json` | `X-PyPI-Last-Serial` + ETag | 15 m | 0.1 | high |
| WPILib Maven | `https://frcmaven.wpi.edu/artifactory/release/…/maven-metadata.xml` | `<lastUpdated>` + ETag | 6 h | 0.05 | normal |
| Vendor JSON repo (canonical catalog) | `raw.githubusercontent.com/wpilibsuite/vendor-json-repo/main/{2026,2026beta,2027_alpha7}_metadata.json` + contents API | ETag (`max-age=300`) + commit SHA | 5 m | 0.3 | critical |
| SystemcoreTesting (2027 compat matrix) | `wpilibsuite/SystemcoreTesting` commits + issues `?since=` | commit SHA | 1 h | 0.5 | critical (alpha) |
| GradleRIO | `wpilibsuite/GradleRIO` releases | marker | 1 h | 0.05 | normal |
| WPILib blog | `https://wpilib.org/blog?format=rss` ⚠️ (Squarespace pattern) | GUID + hash | 6 h | 0.03 | normal |

## 2. Vendor libraries

| Library | Docs | Vendordep (2026) | Releases / API | Detect | Pri |
|---|---|---|---|---|---|
| CTRE Phoenix 6 | `v6.docs.ctr-electronics.com` (source `CrossTheRoadElec/Phoenix6-Documentation`) | `https://maven.ctr-electronics.com/release/com/ctre/phoenix6/latest/Phoenix6-frc2026-latest.json` (+ replay, beta) | `CrossTheRoadElec/Phoenix-Releases`; API `api.ctr-electronics.com/phoenix6/latest/{java,cpp,python}/` | vendordep `version` + commit SHA | critical |
| CTRE changelog RSS | `https://api.ctr-electronics.com/rss/rss.xml` ⚠️ | — | — | GUID + hash, 30 m | high |
| CTRE Phoenix 5 | same portal | `…/phoenix/Phoenix5-frc2026-latest.json` | — | vendordep `version` | normal |
| REVLib | `docs.revrobotics.com/revlib` (GitBook: `llms.txt` ⚠️) | `https://software-metadata.revrobotics.com/REVLib-2026.json` | `REVrobotics/*` | vendordep + page ETag | critical |
| ReduxLib | `docs.reduxrobotics.com/reduxlib`; API `apidocs.reduxrobotics.com/current/{java,cpp}/` | `https://frcsdk.reduxrobotics.com/ReduxLib_2026.json` | `Redux-Robotics/canandrepo-public` | vendordep + marker | normal |
| Studica (navX) | `Studica-Robotics/NavX` | `https://dev.studica.com/maven/release/2026/json/Studica-2026.0.0.json` | — | vendordep | normal |
| Playing With Fusion | playingwithfusion.com docid=1205 | `https://www.playingwithfusion.com/frc/playingwithfusion2026.json` | — | vendordep | low |
| Grapple LaserCAN | grapplerobotics.au | `https://storage.googleapis.com/grapple-frc-maven/libgrapplefrc2026.json` ⚠️ | `GrappleRobotics/libgrapplefrc` | probe | low |
| ThriftyLib | `docs.home.thethriftybot.com` | `https://docs.home.thethriftybot.com/ThriftyLib-2026.json` | — | vendordep | low |
| PhotonVision | `docs.photonvision.org` (RST in main repo) | release asset `photonlib-v2026.x.y.json` | `PhotonVision/photonvision` releases; PyPI `photonlibpy` | marker + commit SHA | high |
| Limelight | `docs.limelightvision.io` | LimelightLib 2 vendordep ⚠️ (`com.limelightvision.Limelight`); classic `LimelightHelpers` single file | `LimelightVision/limelightlib-wpijava`, `-wpicpp` releases; Javadoc site | marker + ETag | high |
| QuestNav | `questnav.gg/docs` | `questnavlib.json` (release asset) ⚠️ | GitHub releases | marker | low |

## 3. Community libraries & tools

| Library | Docs | Vendordep | Releases | Pri |
|---|---|---|---|---|
| PathPlanner / PathPlannerLib | `pathplanner.dev` | `https://3015rangerrobotics.github.io/pathplannerlib/PathplannerLib.json` | `mjansen4857/pathplanner` ⚠️ owner | high |
| Choreo / ChoreoLib | `choreo.autos` (MD in repo `docs/`) | `https://choreo.autos/lib/ChoreoLib2026.json` | `SleipnirGroup/Choreo`; PyPI `sleipnirgroup-choreolib` | high |
| AdvantageKit | `docs.advantagekit.org` | `https://github.com/Mechanical-Advantage/AdvantageKit/releases/latest/download/AdvantageKit.json` | releases; 2027 notes in `SystemcoreTesting/AdvantageKit.md` | high |
| AdvantageScope | `docs.advantagescope.org` | app | `Mechanical-Advantage/AdvantageScope` releases | normal |
| URCL | AdvantageScope docs | `https://raw.githubusercontent.com/Mechanical-Advantage/URCL/main/URCL.json` | commits | low |
| YAGSL | `yagsl.yassrobotics.com` (`llms.txt`, `.md` per page) | `…/YAGSL/yagsl.json` vs `…/YAGSL/yagsl/yagsl.json` ⚠️ conflict — probe both | `Yet-Another-Software-Suite/YAGSL` | high |
| YALL | GitHub | `https://Yet-Another-Software-Suite.github.io/YALL/yall.json` ⚠️ | releases | low |
| maple-sim | `shenzhen-robotics-alliance.github.io/maple-sim` | `…/maple-sim/vendordep/maple-sim.json` | releases | normal |
| Elastic dashboard | WPILib docs | bundled | `Gold872/elastic-dashboard` ⚠️ owner | low |

## 4. Rules & official FIRST

| Source | Endpoint | Detect | Cadence notes | Pri |
|---|---|---|---|---|
| Game Manual & Team Updates | `https://www.firstinspires.org/resources/library/frc/season-materials` → PDF links | page hash + PDF `HEAD` ETag/Last-Modified | Team Updates typically Tue/Fri in season | critical (season) |
| FRC Q&A | `https://frc-qa.firstinspires.org/` (RSS `answers.rss` ⚠️) | GUID / list-page delta by question ID | daily in season | high (season) |
| FIRST Community Blog (FRC) | `https://community.firstinspires.org/topic/frc` (RSS ⚠️) | list-page norm hash; Chief Delphi "[FRC Blog]" cross-posts as backup | weekly | normal |

## 5. Community & news (low authority, troubleshooting signal)

| Source | Endpoint | Detect | Floor | Notes |
|---|---|---|---|---|
| Chief Delphi topics | `https://www.chiefdelphi.com/latest.rss` (adapter `discourse-rss`, id `chiefdelphi`) | GUID + item hash (no validators) | 30 m (`min_interval`) | JSON API is Cloudflare-blocked; site-level RSS is allowed by robots.txt. Category allowlist on `<category>` (Programming, Java, C/C++, Python, Control System, CAN, Sensors, Motors, Electrical, PhotonVision, Technical, Technical Discussion). Opt-in only, never published (§0.1.1) |
| Chief Delphi replies | `https://www.chiefdelphi.com/posts.rss` (`posts_url` of the same source) | same | 30 m | no category in the feed: a reply is kept only when its topic is in `latest.rss` in an allowed category |
| ~~Chief Delphi category feeds~~ | `/c/technical/programming/30.rss`, `/c/technical/control-system/72.rss` | — | — | **not used: robots.txt disallows `/c/*.rss`** (and `/t/*/*.rss`); the plan's ids 11 and 9 were also wrong |
| YouTube (WPILib, FIRST, vendors) | `https://www.youtube.com/feeds/videos.xml?channel_id={id}` | ETag; WebSub optional when hosted | 1 h | titles/descriptions only |
| r/FRC | `https://www.reddit.com/r/FRC/.rss` ⚠️ | GUID | 1 h | custom UA required |
| Discord | — | — | — | out of scope (auth + ToS) |

## 6. Hardware specs (reference data, human-reviewed → `hw_spec` table, served by `frc_hardware`)

Every source is its own labeled row per part, **never merged or averaged**: NEO stall torque is 2.6 N·m in WPILib's
`DCMotor`, 4.201 N·m in ReCalc (CTRE dyno) and 3.75 N·m on REV's NEO 2.0 page. Part ids are shared across sources
(`krakenx60`, `krakenx60-foc`, `neo`, `neovortex`, `neo550`, …) so one query returns every column. Rows that do not
depend on the FRC season have `season: all` and answer every season. Probed 2026-09-29.

| Source label | Adapter / file | Upstream (pinned) | License → shard | Rows |
|---|---|---|---|---|
| `wpilib-dcmotor` | `wpilib-dcmotor` | `DCMotor.java` at `v2026.2.2` and `v2027.0.0-alpha-7` | BSD-3-Clause → `hardware` | 19 motors × 2 seasons |
| `recalc` | `recalc-motors` | `tervay/recalc` `app/lib/models/Motor.ts` (`ALL_MOTORS`) at commit `8702a80eb61300b3cbfd7b800ea8d4b0e7614ab9`; `LICENSE.txt` is MIT | MIT → `hardware` | 21 FRC motors (the 7 FTC/VEX V5 motors are skipped); 5 are new parts: `neo2`, `minionadvhall`, `thriftypulsar`, `775redline`, `snowblower` |
| `rev-docs` | `gitbook-spec-table` | REV GitBook `.md` pages: `brushless/neo/v1.1`, `/2.0`, `/vortex`, `/550` (unversioned; ETag is the revision) | none published (`LicenseRef-REV-Docs-NoLicense`) → `hardware-restricted` | 4 motors |
| `wcp-docs` | `gitbook-spec-table` | WCP GitBook `.md` motor-performance pages for Kraken X60 and X44 (a trapezoidal and an FOC tab each) | none published (`LicenseRef-WCP-Docs-NoLicense`) → `hardware-restricted` | 4 rows (X60, X60 FOC, X44, X44 FOC) |
| `sds`, `rev`, `wcp`, `ctre`, `redux` | `data/hardware/*.yaml` (curated) | the vendor page in each row's `citation` (plus `see_also`), read on `checked` | the file is our MIT compilation of cited facts → `hardware` | swerve modules, encoders, IMUs |

- **ReCalc** stores five DCMotor numbers per motor plus motor and controller weight; resistance, kV and kT are
  computed at runtime in ReCalc and are not upstream data, so they are not stored. The row note carries ReCalc's own
  `dataSource` (whose dyno the numbers come from), type and vendors. A unit other than the expected one (`N*m`, `A`,
  `rpm`, `V`, `lb`) fails the build.
- **Vendor spec pages** are read only when machine-readable: GitBook publishes a `.md` rendition of every page (see
  `llms.txt`), and both robots.txt files allow everything. Only listed parameters are read (free speed, free/stall
  current, stall torque, Kv, peak power, max efficiency, kT) and each must carry the expected unit. The REV/WCP
  docs carry no license, so these rows are in `hardware-restricted`, which `index publish` leaves out until
  permission is recorded (§0.2). `Reader.Licenses` now covers `hw_spec`, `symbol` and `release` rows too, so a shard
  without chunks is still held back.
- **Curated data** (`data/hardware/`): one row per part and vendor with unit-suffixed numeric fields, a citation and
  a note (interfaces and conditions go in the note). Validated at build and by `go test ./internal/hwdata`: unknown
  keys, keys without a unit suffix (ratios excepted), non-https citations, duplicate (part, source) and categories
  other than motor/encoder/imu/swerve_module are rejected. Contents: CTRE Minion (store tech specs); SDS MK4, MK4i, MK4n, MK5i, MK5n; REV MAXSwerve;
  WCP Swerve X, X2, X2S, XS; CTRE CANcoder; REV Through Bore V1 and V2; Redux Canandmag; CTRE Pigeon 2.0; Redux
  Boron Canandgyro.

**Looked at and skipped (2026-09-29):**

- **Swerve drive ratios** (SDS L1–L4 / L1+–L3+ / MK5, WCP X1–X4, REV MAXSwerve low/mid/high): published only as a
  table image (SDS product pages, REV product page) or an SVG (WCP ratio pages). Not transcribed; the curated rows
  hold the steering ratio, which every vendor states in text.
- **CTRE motor data**: the Phoenix 6 hardware-reference pages carry no performance table, and the CTRE store's
  Minion page states only stall torque, peak power and weight in prose (curated as the `ctre` row for `minion`).
  Kraken X60/X44 tables are on WCP's pages (`wcp-docs`), and ReCalc's `recalc` rows carry CTRE's dyno values
  (`dataSource: CTRE`).
- **CANcoder resolution**: the store page states supply range and current only; the resolution is in the user's guide
  PDF, which is not scraped.
- **Studica navX2-MXP**: specs are in a PDF user guide and the store pages return 403 to non-browser clients.
- **SDS `swerve-lib`** (`SdsModuleConfigurations.java`, gear constants in code): the repository is named
  "Do not use … unmaintained" and has no license.
- **Motor controllers** (`controller` category): no machine-readable, citable spec table was found; not added.

Spec pages change without notice: re-run `make index` and review the `hw_spec` diff; curated rows are re-checked by
hand (update `checked`).

## 7. Optional event-data adapters (off by default)

FRC Events API (`frc-api.firstinspires.org`, HTTP Basic, non-commercial, `If-Modified-Since`), The Blue Alliance v3
(`X-TBA-Auth-Key`, `Last-Modified`, HMAC-signed webhooks), Statbotics v3 (`api.statbotics.io/v3`, no auth — be gentle).
Tokens are never shipped in shards or binaries.

## 8. 2027 transition watchlist

- Package moves: `edu.wpi.first` → `org.wpilib` (Java), `frc::` → `wpi::` (C++).
- SmartDashboard/SendableChooser → Telemetry/Tunables; `LoggedDashboardChooser` → `LoggedNetworkChooser` (AdvantageKit).
- AprilTag and CameraServer moved to vendordeps; vendordeps must be re-imported for alpha-7.
- CTRE: `<Device>(int id, String canbus)` constructors deprecated in 2026, removed in 2027 (explicit `CANBus`).
- ChoreoLib 2027 alpha depends on Commands v2 only (`conflictsWith` Commands v3).
- Docs location conflict: `docs.wpilib.org/en/latest/` vs `/en/2027/` — index both, dedupe by canonical URL.
These seed `data/migrations/*.yaml`; each rule must cite its upstream changelog.
