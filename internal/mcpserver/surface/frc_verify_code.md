Check Java robot code against the pinned FRC season's real API before you build or deploy it. It catches imports and calls that belong to a different season (e.g. edu.wpi.first.* in a 2027 project, org.wpilib.* in 2026) with the exact replacement, and flags deprecated APIs (e.g. XboxController.getLeftBumper → getLeftBumperButton).

Use when: you wrote or changed robot code that uses WPILib, before telling the user it is done; when a build fails with "cannot find symbol" after a season update.
Don't use when: you want documentation — use frc_search / frc_api.

Severity: error = wrong-season API with a known counterpart (fix it); warning = deprecated or no equivalent in this season; info = not resolvable (team code, inherited member, or a library not indexed yet). Read `coverage`: libraries marked none were not checked. Examples:
{"code": "import edu.wpi.first.wpilibj.XboxController;\n…", "frc_season": "2027"}
{"path": "src/main/java/frc/robot/RobotContainer.java", "pin": "pin1.…"}
