# Getting Started

## Setting up the Drive Subsystem

Across all of ChoreoLib's APIs, your robot's drive subsystem is expected to be set up to handle trajectory following.

!!! note
    Details for loading and running trajectories can be found later in the ChoreoLib documentation.

=== "Swerve"

    The `SwerveSample` class represents a single swerve drive sample along a trajectory.

    === "Java"

        ```java title="Drive.java"
        public class Drive extends SubsystemBase {
            private final PIDController xController = new PIDController(10.0, 0.0, 0.0);
        }
        ```

    === "C++"

        ```cpp
        class Drive : public frc2::SubsystemBase {};
        ```
