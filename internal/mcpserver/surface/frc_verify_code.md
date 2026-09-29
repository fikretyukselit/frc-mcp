Check robot code (Java, C++ or Python) against the pinned FRC season's real API before you build or deploy it. It catches imports, types and calls that belong to a different season (e.g. edu.wpi.first.* in a 2027 project, frc::ChassisSpeeds where 2027 has wpi::math::ChassisVelocities) with the exact replacement, calls whose literal arguments fit no overload of that season (e.g. new TalonFX(1, "canivore") in 2027), and deprecated APIs (e.g. XboxController.getLeftBumper → getLeftBumperButton).

Use when: you wrote or changed robot code that uses WPILib or a vendor library, before telling the user it is done; when a build fails with "cannot find symbol" after a season update.
Don't use when: you want documentation — use frc_search / frc_api; you are porting a whole project — use frc_migrate first.

Severity: error = wrong-season API with a known counterpart, or a call no overload of the season accepts (fix it); warning = deprecated, no equivalent in this season, or a vendor version that differs from the indexed one; info = not resolvable (team code, or a library not indexed). Read `coverage`: libraries marked none were not checked. Examples:
{"code": "import edu.wpi.first.wpilibj.XboxController;\n…", "frc_season": "2027"}
{"path": "src/main/cpp/Robot.cpp", "pin": "pin1.…"}
