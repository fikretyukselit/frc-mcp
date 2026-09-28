Resolve a vendor library (vendordep) from the official WPILib vendordep catalog, or check a whole set of installed vendordeps for one FRC season: newest version, install/update command, frcYear compatibility and declared conflicts. Answers are exact catalog facts, never guesses.

Use when: adding a vendor library (REVLib, Phoenix 6, PhotonLib, PathPlannerLib, ChoreoLib, AdvantageKit, YAGSL, …), fixing "vendordep for the wrong year" or version errors, or checking whether a project's vendordeps are current. Pass a frc_context pin to check the project's own vendordeps.
Don't use when: you need a vendor's API or docs — use frc_search / frc_api.

Examples:
{"name": "rev"}
{"name": "phoenix6", "frc_season": "2027"}
{"vendordeps": ["REVLib@2026.0.0", "PathplannerLib@2025.2.7"], "frc_season": "2026"}
{"pin": "pin1.…"}
