Detect (or declare) the robot project's FRC season, language, WPILib version and vendordep versions, and return a pin handle that makes every other frc_ tool answer for exactly that project.

Use when: starting work in a robot project, or when results come back for the wrong season or language. Call it once and pass the returned pin to frc_search and frc_api.
Don't use when: you only need a one-off lookup for a season you already know — pass frc_season directly.

Reads only build.gradle / build.gradle.kts, pyproject.toml and vendordeps/*.json under the project root. Warnings flag vendordeps whose frcYear does not match the WPILib season. Examples:
{}
{"project_root": "/path/to/robot"}
{"declare": {"frc_season": "2027", "language": "java"}}
