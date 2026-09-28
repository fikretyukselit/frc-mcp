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
| Chief Delphi latest | `https://www.chiefdelphi.com/latest.rss` | GUID + item hash (no validators) | 15 m | JSON API is Cloudflare-blocked; RSS is explicitly allowed |
| Chief Delphi Programming / Control Systems | `/c/technical/programming/11.rss`, `/c/technical/control-systems/9.rss` ⚠️ IDs | same | 30 m | |
| YouTube (WPILib, FIRST, vendors) | `https://www.youtube.com/feeds/videos.xml?channel_id={id}` | ETag; WebSub optional when hosted | 1 h | titles/descriptions only |
| r/FRC | `https://www.reddit.com/r/FRC/.rss` ⚠️ | GUID | 1 h | custom UA required |
| Discord | — | — | — | out of scope (auth + ToS) |

## 6. Hardware specs (reference data, human-reviewed → `hw_spec` table, served by `frc_hardware`)

Motor constants are served **with both sources labeled** — never merged: WPILib `DCMotor` (pinned to a release tag,
`wpimath/.../DCMotor.java`) and ReCalc / CTRE dyno data (`tervay/recalc` ⚠️ path). NEO and NEO Vortex differ
materially between sources (e.g. NEO stall torque 2.6 Nm vs 4.20 Nm). Spec pages (SDS, WCP, AndyMark, REV, CTRE)
are polled weekly by norm hash; changes open a review PR rather than auto-publishing.

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
