Look up an exact API symbol (class, method, constructor, field) for one FRC season and language: signatures, deprecation/removal, replacement, and where the same symbol lives in other seasons.

Use when: you know or suspect a class/method name (TalonFX, SparkMax#configure, frc::SwerveDriveKinematics, edu.wpi.first.math.kinematics.SwerveDriveKinematics) and need its current, correct form — especially before writing code against it.
Don't use when: you have a conceptual question — call frc_search.

status "version_mismatch" means the symbol does not exist in the pinned season (e.g. CANSparkMax in 2025+, edu.wpi.first packages in 2027); curated[] then gives the cited rename. Names match exactly: match "case_differs" (status low_confidence) is a different symbol, not the one you asked for. Examples:
{"symbol": "TalonFX#TalonFX", "language": "java"}
{"symbol": "SwerveDriveKinematics", "frc_season": "2027"}
