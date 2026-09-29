FRC motor specs for sizing mechanisms and setting up simulation: stall torque, stall current, free speed, free current, nominal voltage. Every source is returned as its own column and never merged or averaged. Today the source is WPILib's DCMotor constants ("wpilib-dcmotor"), i.e. exactly what WPILib simulation uses, plus the matching DCMotor factory in Java, C++ and Python. Exact facts from a table, not a similarity search.

Use when: choosing a motor or gearing, estimating mechanism speed/torque, or setting up a physics simulation (which DCMotor factory to call). Accepts common names: "kraken", "kraken x60 foc", "neo", "vortex", "falcon", "neo550", "775pro", "minion".
Don't use when: you need motor controller configuration or API usage — use frc_search / frc_api. Values differ between sources (vendor dynos vs WPILib constants); cite the source you use.

Examples:
{"parts": ["kraken x60", "kraken x60 foc", "neo vortex"]}
{"category": "motor"}
{"parts": ["neo"], "frc_season": "2027"}
