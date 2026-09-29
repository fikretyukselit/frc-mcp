Map FRC APIs from one season to another (e.g. a 2026 robot project to 2027): renamed, moved and removed classes and methods, with the exact target-season name, a confidence and where the mapping comes from. Pass one symbol or a whole source file (Java, C++ or Python); it returns mappings only and never rewrites code.

Use when: porting robot code to a new season or vendor major version; a symbol from a tutorial or an old project does not exist in the pinned season (frc_api or frc_verify_code said version_mismatch).
Don't use when: you need docs for a symbol that exists — use frc_api.

Sources, most reliable first: curated (a cited upstream change, e.g. ChassisSpeeds → ChassisVelocities), upstream (the library's own deprecation note), generated (the same class in a new package, e.g. edu.wpi.first.* → org.wpilib.*). Unresolved symbols come with pointers to target-season docs and release notes; read them with frc_fetch. After applying the mappings, run frc_verify_code for the target season. Examples:
{"symbol": "ChassisSpeeds", "from": "2026", "to": "2027"}
{"path": "src/main/java/frc/robot/subsystems/Drive.java", "to": "2027"}
